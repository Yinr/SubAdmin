package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"subadmin/internal/applog"
	"subadmin/internal/auth"
	"subadmin/internal/config"
	"subadmin/internal/db"
	"subadmin/internal/secretbox"
	"subadmin/internal/sites"
)

func main() {
	cfg := config.Load()
	logger, err := applog.Open(cfg.LogDir, cfg.LogLevel, cfg.LogMaxBytes, cfg.LogBackups)
	if err != nil {
		log.Fatalf("open app logger: %v", err)
	}
	defer logger.Close()
	slog.Info("subadmin starting", "addr", cfg.Addr, "db_path", cfg.DBPath, "log_dir", cfg.LogDir, "log_level", cfg.LogLevel, "log_max_bytes", cfg.LogMaxBytes, "log_backups", cfg.LogBackups)
	dbPath := cfg.DBPath
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		slog.Error("create db dir failed", "error", err, "path", filepath.Dir(dbPath))
		os.Exit(1)
	}

	store, err := db.Open(dbPath)
	if err != nil {
		slog.Error("open db failed", "error", err, "path", dbPath)
		os.Exit(1)
	}
	defer store.Close()
	slog.Info("database opened", "path", dbPath)
	authManager := auth.NewManager(store.DB(), cfg)
	var siteService *sites.Service
	var jobService *jobManager
	if cfg.SecretKey != "" {
		box, err := secretbox.New(cfg.SecretKey)
		if err != nil {
			slog.Error("init secret box failed", "error", err)
			os.Exit(1)
		}
		siteService = sites.NewService(store.DB(), box)
		jobService = newJobManager(store.DB(), siteService, cfg.LogDir)
		if err := jobService.markInterrupted(context.Background()); err != nil {
			slog.Warn("mark interrupted jobs failed", "error", err)
		}
		slog.Info("site and job services initialized")
	} else {
		slog.Warn("site management disabled because SUBADMIN_SECRET_KEY is not set")
	}

	mux := http.NewServeMux()
	registerFrontend(mux)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var input struct {
			Secret string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := authManager.Login(w, r, input.Secret); err != nil {
			if err == auth.ErrUnauthorized {
				slog.WarnContext(r.Context(), "login failed", "reason", "invalid secret", "remote_addr", r.RemoteAddr)
				writeError(w, http.StatusUnauthorized, "invalid login secret")
				return
			}
			slog.ErrorContext(r.Context(), "login failed", "error", err)
			writeError(w, http.StatusInternalServerError, "login failed")
			return
		}
		slog.InfoContext(r.Context(), "login succeeded", "remote_addr", r.RemoteAddr)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := authManager.Logout(w, r); err != nil {
			slog.ErrorContext(r.Context(), "logout failed", "error", err)
			writeError(w, http.StatusInternalServerError, "logout failed")
			return
		}
		slog.InfoContext(r.Context(), "logout succeeded", "remote_addr", r.RemoteAddr)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		session, err := authManager.Current(r)
		if err != nil {
			if err == auth.ErrUnauthorized {
				writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
				return
			}
			writeError(w, http.StatusInternalServerError, "session check failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"expiresAt":     session.ExpiresAt.Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/docs", protectedDocsHandler(authManager))
	mux.HandleFunc("/docs/", protectedDocsHandler(authManager))
	mux.HandleFunc("/api/audit-logs", auditLogsHandler(authManager, store.DB()))
	mux.HandleFunc("/api/import-templates", importTemplatesHandler(authManager, store.DB()))
	mux.HandleFunc("/api/import-templates/", importTemplateDetailHandler(authManager, store.DB()))
	mux.HandleFunc("/api/jobs", jobsHandler(authManager, jobService))
	mux.HandleFunc("/api/jobs/", jobDetailHandler(authManager, jobService))
	mux.HandleFunc("/api/sites", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		if siteService == nil {
			writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for site management")
			return
		}
		switch r.Method {
		case http.MethodGet:
			items, err := siteService.List(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "list sites failed")
				return
			}
			slog.DebugContext(r.Context(), "sites listed", "count", len(items))
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodPost:
			var input sites.CreateInput
			if err := sites.DecodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json body")
				return
			}
			item, err := siteService.Create(r.Context(), input)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			slog.InfoContext(r.Context(), "site created", "site_id", item.ID, "site_name", item.Name)
			writeAuditLog(store.DB(), r, &item.ID, "site.create", "site", 1, map[string]any{"name": item.Name, "baseUrl": item.BaseURL}, map[string]any{"ok": true})
			writeJSON(w, http.StatusCreated, item)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/sites/", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		if siteService == nil {
			writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for site management")
			return
		}
		id, action, ok := sites.IDFromPath(r.URL.Path, "/api/sites")
		if !ok {
			writeError(w, http.StatusNotFound, "site not found")
			return
		}
		if action == "test" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			result, err := siteService.Test(r.Context(), id)
			if err != nil {
				writeSiteError(w, err)
				return
			}
			slog.InfoContext(r.Context(), "site connection tested", "site_id", id, "ok", result["ok"], "status_code", result["statusCode"])
			writeJSON(w, http.StatusOK, result)
			return
		}
		if action == "accounts" {
			if r.Method == http.MethodPost {
				writeBatchAccountTest(w, r, siteService, id, cfg.LogDir)
				return
			}
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			query := r.URL.Query()
			data, statusCode, err := siteService.AdminGET(r.Context(), id, "/api/v1/admin/accounts", query)
			if err != nil {
				writeSiteError(w, err)
				return
			}
			slog.InfoContext(r.Context(), "accounts proxied", "site_id", id, "status_code", statusCode, "response_bytes", len(data), "query_keys", sortedQueryKeys(query))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			data = sanitizeJSONForBrowser(data)
			_, _ = w.Write(data)
			return
		}
		if action == "accounts/search-by-names" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeAccountsSearchByNames(w, r, siteService, id)
			return
		}
		if action == "accounts/refresh" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeBatchAccountRefresh(w, r, siteService, id)
			return
		}
		if action == "jobs/batch-account-test" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeCreateBatchAccountTestJob(w, r, jobService, id)
			return
		}
		if action == "jobs/batch-token-refresh" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeCreateBatchTokenRefreshJob(w, r, jobService, id)
			return
		}
		if action == "imports/preview" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeImportPreview(w, r, store.DB(), id)
			return
		}
		if action == "imports/accounts" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeCreateImportAccountsJob(w, r, jobService, id)
			return
		}
		if action == "groups" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			data, statusCode, err := siteService.AdminGET(r.Context(), id, "/api/v1/admin/groups/all", r.URL.Query())
			if err != nil {
				writeSiteError(w, err)
				return
			}
			slog.DebugContext(r.Context(), "groups proxied", "site_id", id, "status_code", statusCode, "response_bytes", len(data))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			_, _ = w.Write(sanitizeJSONForBrowser(data))
			return
		}
		if action == "proxies" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			data, statusCode, err := siteService.AdminGET(r.Context(), id, "/api/v1/admin/proxies/all", r.URL.Query())
			if err != nil {
				writeSiteError(w, err)
				return
			}
			slog.DebugContext(r.Context(), "proxies proxied", "site_id", id, "status_code", statusCode, "response_bytes", len(data))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			_, _ = w.Write(sanitizeJSONForBrowser(data))
			return
		}
		if action == "statistics" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeSiteStatistics(w, r, siteService, id)
			return
		}
		if action == "statistics/user-concurrency" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeSiteUserConcurrency(w, r, siteService, id)
			return
		}
		if action == "statistics/account-concurrency" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeSiteAccountConcurrency(w, r, siteService, id)
			return
		}
		if action == "group-quota" {
			writeGroupQuota(w, r, authManager, siteService, id)
			return
		}
		if action == "error-accounts" {
			writeErrorAccounts(w, r, authManager, siteService, id)
			return
		}
		if action != "" {
			writeError(w, http.StatusNotFound, "site action not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			item, err := siteService.Get(r.Context(), id)
			if err != nil {
				writeSiteError(w, err)
				return
			}
			slog.DebugContext(r.Context(), "site loaded", "site_id", item.ID, "site_name", item.Name)
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			var input sites.UpdateInput
			if err := sites.DecodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json body")
				return
			}
			item, err := siteService.Update(r.Context(), id, input)
			if err != nil {
				writeSiteError(w, err)
				return
			}
			slog.InfoContext(r.Context(), "site updated", "site_id", item.ID, "site_name", item.Name)
			writeAuditLog(store.DB(), r, &item.ID, "site.update", "site", 1, map[string]any{"name": item.Name, "baseUrl": item.BaseURL}, map[string]any{"ok": true})
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			if err := siteService.Delete(r.Context(), id); err != nil {
				writeSiteError(w, err)
				return
			}
			slog.WarnContext(r.Context(), "site deleted", "site_id", id)
			writeAuditLog(store.DB(), r, &id, "site.delete", "site", 1, map[string]any{"siteId": id}, map[string]any{"ok": true})
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	addr := cfg.Addr
	slog.Info("subadmin listening", "addr", addr)
	if err := http.ListenAndServe(addr, loggingMiddleware(mux)); err != nil {
		slog.Error("serve failed", "error", err)
		os.Exit(1)
	}
}

func protectedDocsHandler(authManager *auth.Manager) http.HandlerFunc {
	docsPath := os.Getenv("SUBADMIN_DOCS_DIR")
	if docsPath == "" {
		docsPath = "../docs"
	}
	fileServer := http.FileServer(http.Dir(docsPath))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs" {
			http.Redirect(w, r, "/docs/", http.StatusPermanentRedirect)
			return
		}
		if !requireAuth(w, r, authManager) {
			return
		}
		http.StripPrefix("/docs/", fileServer).ServeHTTP(w, r)
	}
}

func registerFrontend(mux *http.ServeMux) {
	distPath := os.Getenv("SUBADMIN_WEB_DIST")
	if distPath == "" {
		distPath = "../web/dist"
	}
	if info, err := os.Stat(filepath.Join(distPath, "index.html")); err == nil && !info.IsDir() {
		files := http.FileServer(http.Dir(distPath))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				if _, err := os.Stat(filepath.Join(distPath, filepath.Clean(r.URL.Path))); errors.Is(err, fs.ErrNotExist) {
					http.ServeFile(w, r, filepath.Join(distPath, "index.html"))
					return
				}
			}
			files.ServeHTTP(w, r)
		})
		return
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, shellPage())
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(applog.WithRequestID(r.Context(), requestID))
		wrapped := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		} else if isLowSignalRequest(r) {
			level = slog.LevelDebug
		}
		slog.LogAttrs(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("route_group", routeGroup(r.URL.Path)),
			slog.Int("status", status),
			slog.Int("bytes", wrapped.bytes),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent_family", userAgentFamily(r.UserAgent())),
		)
	})
}

func newRequestID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(data[:])
}

func isLowSignalRequest(r *http.Request) bool {
	path := r.URL.Path
	return path == "/healthz" || strings.HasPrefix(path, "/assets/") || path == "/favicon.ico"
}

func routeGroup(path string) string {
	switch {
	case path == "/healthz":
		return "health"
	case strings.HasPrefix(path, "/api/auth"):
		return "auth"
	case strings.HasPrefix(path, "/api/sites") && strings.Contains(path, "search-by-names"):
		return "accounts/search-by-names"
	case strings.HasPrefix(path, "/api/sites") && strings.Contains(path, "group-quota"):
		return "group-quota"
	case strings.HasPrefix(path, "/api/sites") && strings.Contains(path, "error-accounts"):
		return "error-accounts"
	case strings.HasPrefix(path, "/api/sites"):
		return "sites"
	case strings.HasPrefix(path, "/api/jobs"):
		return "jobs"
	case strings.HasPrefix(path, "/docs"):
		return "docs"
	case strings.HasPrefix(path, "/assets"):
		return "assets"
	default:
		return "frontend"
	}
}

func userAgentFamily(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "edg/"):
		return "edge"
	case strings.Contains(lower, "chrome/"):
		return "chrome"
	case strings.Contains(lower, "firefox/"):
		return "firefox"
	case strings.Contains(lower, "safari/"):
		return "safari"
	case strings.Contains(lower, "curl/"):
		return "curl"
	case value == "":
		return "unknown"
	default:
		return "other"
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	slog.LogAttrs(context.Background(), logLevelForStatus(status), "api error", slog.Int("status", status), slog.String("message", message))
	writeJSON(w, status, map[string]any{"error": message})
}

func logLevelForStatus(status int) slog.Level {
	if status >= 500 {
		return slog.LevelError
	}
	if status >= 400 {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

func sortedQueryKeys(query url.Values) []string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sanitizeJSONForBrowser(data []byte) []byte {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return data
	}
	redactSensitive(value)
	sanitized, err := json.Marshal(value)
	if err != nil {
		return data
	}
	return sanitized
}

func redactSensitive(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if isSensitiveKey(key) {
				current[key] = "[redacted]"
				continue
			}
			redactSensitive(child)
		}
	case []any:
		for _, child := range current {
			redactSensitive(child)
		}
	}
}

func isSensitiveKey(key string) bool {
	switch strings.ToLower(key) {
	case "credentials", "credential", "access_token", "refresh_token", "id_token", "token", "secret", "password", "cookie", "authorization", "api_key", "key", "username":
		return true
	default:
		return false
	}
}

func requireAuth(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	if _, err := manager.Current(r); err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return false
		}
		writeError(w, http.StatusInternalServerError, "session check failed")
		return false
	}
	return true
}

func writeSiteError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "site not found")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeError(w, http.StatusGatewayTimeout, "upstream request timed out")
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		writeError(w, http.StatusGatewayTimeout, "upstream request timed out")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

type batchAccountTestInput struct {
	IDs          []int64                `json:"ids"`
	ModelID      string                 `json:"modelId"`
	Prompt       string                 `json:"prompt"`
	Mode         string                 `json:"mode"`
	DelayMs      int                    `json:"delayMs"`
	JitterMs     int                    `json:"jitterMs"`
	LogResponses bool                   `json:"logResponses"`
	AccountMeta  map[string]accountMeta `json:"accountMeta,omitempty"`
}

type batchTokenRefreshInput struct {
	IDs         []int64                `json:"ids"`
	AccountMeta map[string]accountMeta `json:"accountMeta,omitempty"`
}

type accountMeta struct {
	Name string `json:"name,omitempty"`
	Note string `json:"note,omitempty"`
}

type jobManager struct {
	db          *sql.DB
	siteService *sites.Service
	logDir      string
	mu          sync.Mutex
	cancels     map[int64]context.CancelFunc
}

type jobRecord struct {
	ID           int64           `json:"id"`
	SiteID       *int64          `json:"siteId,omitempty"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	TotalCount   int             `json:"totalCount"`
	DoneCount    int             `json:"doneCount"`
	SuccessCount int             `json:"successCount"`
	FailedCount  int             `json:"failedCount"`
	Input        json.RawMessage `json:"input"`
	Result       json.RawMessage `json:"result"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    int64           `json:"createdAt"`
	StartedAt    *int64          `json:"startedAt,omitempty"`
	FinishedAt   *int64          `json:"finishedAt,omitempty"`
}

type auditLogRecord struct {
	ID             int64           `json:"id"`
	SiteID         *int64          `json:"siteId,omitempty"`
	Action         string          `json:"action"`
	TargetType     string          `json:"targetType"`
	TargetCount    int             `json:"targetCount"`
	RequestSummary json.RawMessage `json:"requestSummary"`
	ResultSummary  json.RawMessage `json:"resultSummary"`
	IP             string          `json:"ip"`
	UserAgent      string          `json:"userAgent"`
	CreatedAt      int64           `json:"createdAt"`
}

func auditLogsHandler(authManager *auth.Manager, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items, err := listAuditLogs(r.Context(), database, 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list audit logs failed")
			return
		}
		slog.DebugContext(r.Context(), "audit logs listed", "count", len(items))
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func listAuditLogs(ctx context.Context, database *sql.DB, limit int) ([]auditLogRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := database.QueryContext(ctx, `
SELECT id, site_id, action, target_type, target_count, request_summary_json, result_summary_json, ip, user_agent, created_at
FROM audit_logs ORDER BY id DESC LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []auditLogRecord{}
	for rows.Next() {
		var item auditLogRecord
		var siteID sql.NullInt64
		var requestJSON, resultJSON string
		if err := rows.Scan(&item.ID, &siteID, &item.Action, &item.TargetType, &item.TargetCount, &requestJSON, &resultJSON, &item.IP, &item.UserAgent, &item.CreatedAt); err != nil {
			return nil, err
		}
		if siteID.Valid {
			item.SiteID = &siteID.Int64
		}
		item.RequestSummary = json.RawMessage(requestJSON)
		item.ResultSummary = json.RawMessage(resultJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func writeAuditLog(database *sql.DB, r *http.Request, siteID *int64, action, targetType string, targetCount int, requestSummary, resultSummary map[string]any) {
	requestJSON, err := json.Marshal(sanitizeAuditSummary(requestSummary))
	if err != nil {
		requestJSON = []byte(`{}`)
	}
	resultJSON, err := json.Marshal(sanitizeAuditSummary(resultSummary))
	if err != nil {
		resultJSON = []byte(`{}`)
	}
	var nullableSiteID any
	if siteID != nil {
		nullableSiteID = *siteID
	}
	_, err = database.ExecContext(r.Context(), `
INSERT INTO audit_logs (site_id, action, target_type, target_count, request_summary_json, result_summary_json, ip, user_agent, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, nullableSiteID, action, targetType, targetCount, string(requestJSON), string(resultJSON), r.RemoteAddr, userAgentFamily(r.UserAgent()), time.Now().Unix())
	if err != nil {
		slog.WarnContext(r.Context(), "write audit log failed", "action", action, "error", err)
	}
}

func sanitizeAuditSummary(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, child := range value {
		if isSensitiveKey(key) {
			result[key] = "[redacted]"
			continue
		}
		result[key] = child
	}
	return result
}

func newJobManager(db *sql.DB, siteService *sites.Service, logDir string) *jobManager {
	return &jobManager{db: db, siteService: siteService, logDir: logDir, cancels: map[int64]context.CancelFunc{}}
}

func jobsHandler(authManager *auth.Manager, manager *jobManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		if manager == nil {
			writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for jobs")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items, err := manager.list(r.Context(), 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list jobs failed")
			return
		}
		slog.DebugContext(r.Context(), "jobs listed", "count", len(items))
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func jobDetailHandler(authManager *auth.Manager, manager *jobManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		if manager == nil {
			writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for jobs")
			return
		}
		id, action, ok := jobIDFromPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		switch action {
		case "":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			job, err := manager.get(r.Context(), id)
			if err != nil {
				writeJobError(w, err)
				return
			}
			slog.DebugContext(r.Context(), "job loaded", "job_id", job.ID, "type", job.Type, "status", job.Status, "done_count", job.DoneCount, "total_count", job.TotalCount)
			writeJSON(w, http.StatusOK, job)
		case "cancel":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			job, err := manager.cancel(r.Context(), id)
			if err != nil {
				writeJobError(w, err)
				return
			}
			slog.WarnContext(r.Context(), "job cancel requested", "job_id", id, "status", job.Status, "type", job.Type)
			writeAuditLog(manager.db, r, job.SiteID, "job.cancel", job.Type, job.TotalCount, map[string]any{"jobId": id}, map[string]any{"status": job.Status})
			writeJSON(w, http.StatusOK, job)
		case "retry-failed":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			job, err := manager.retryFailed(r.Context(), id)
			if err != nil {
				writeJobError(w, err)
				return
			}
			slog.InfoContext(r.Context(), "job retry created", "source_job_id", id, "new_job_id", job.ID, "type", job.Type, "total_count", job.TotalCount)
			writeAuditLog(manager.db, r, job.SiteID, "job.retry_failed", job.Type, job.TotalCount, map[string]any{"sourceJobId": id, "newJobId": job.ID}, map[string]any{"status": job.Status})
			writeJSON(w, http.StatusCreated, job)
		default:
			writeError(w, http.StatusNotFound, "job action not found")
		}
	}
}

func jobIDFromPath(path string) (int64, string, bool) {
	rest := strings.TrimPrefix(path, "/api/jobs/")
	parts := strings.SplitN(strings.Trim(rest, "/"), "/", 2)
	if parts[0] == "" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	action := ""
	if len(parts) == 2 {
		action = strings.Trim(parts[1], "/")
	}
	return id, action, true
}

func writeJobError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeCreateBatchAccountTestJob(w http.ResponseWriter, r *http.Request, manager *jobManager, siteID int64) {
	if manager == nil {
		writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for jobs")
		return
	}
	var input batchAccountTestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	job, err := manager.createBatchAccountTest(r.Context(), siteID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.InfoContext(r.Context(), "job created", "job_id", job.ID, "type", job.Type, "site_id", siteID, "total_count", job.TotalCount)
	writeAuditLog(manager.db, r, &siteID, "job.create", job.Type, job.TotalCount, map[string]any{"jobId": job.ID, "type": job.Type}, map[string]any{"status": job.Status})
	writeJSON(w, http.StatusCreated, job)
}

func writeCreateBatchTokenRefreshJob(w http.ResponseWriter, r *http.Request, manager *jobManager, siteID int64) {
	if manager == nil {
		writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for jobs")
		return
	}
	var input batchTokenRefreshInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	job, err := manager.createBatchTokenRefresh(r.Context(), siteID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.InfoContext(r.Context(), "job created", "job_id", job.ID, "type", job.Type, "site_id", siteID, "total_count", job.TotalCount)
	writeAuditLog(manager.db, r, &siteID, "job.create", job.Type, job.TotalCount, map[string]any{"jobId": job.ID, "type": job.Type}, map[string]any{"status": job.Status})
	writeJSON(w, http.StatusCreated, job)
}

func writeCreateImportAccountsJob(w http.ResponseWriter, r *http.Request, manager *jobManager, siteID int64) {
	if manager == nil {
		writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required for jobs")
		return
	}
	var input importAccountsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !input.Confirmation.Confirmed {
		writeError(w, http.StatusBadRequest, "import confirmation is required")
		return
	}
	input.Settings = cleanImportAccountSettings(input.Settings)
	job, err := manager.createImportAccounts(r.Context(), siteID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.WarnContext(r.Context(), "import accounts job created", "job_id", job.ID, "type", job.Type, "site_id", siteID, "total_count", job.TotalCount)
	writeAuditLog(manager.db, r, &siteID, "job.create", job.Type, job.TotalCount, map[string]any{"jobId": job.ID, "type": job.Type, "filename": input.Filename, "settings": safeImportExecutionSettings(input.Settings)}, map[string]any{"status": job.Status})
	writeJSON(w, http.StatusCreated, job)
}

func (m *jobManager) createBatchAccountTest(ctx context.Context, siteID int64, input batchAccountTestInput) (*jobRecord, error) {
	ids := cleanAccountIDs(input.IDs)
	if len(ids) == 0 {
		return nil, errors.New("ids is required")
	}
	input.IDs = ids
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Mode = strings.TrimSpace(input.Mode)
	input.AccountMeta = cleanAccountMeta(input.AccountMeta)
	if input.DelayMs < 0 {
		input.DelayMs = 0
	}
	if input.JitterMs < 0 {
		input.JitterMs = 0
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := m.db.ExecContext(ctx, `
INSERT INTO jobs (site_id, type, status, total_count, done_count, success_count, failed_count, input_json, result_json, created_at)
VALUES (?, 'batch_account_test', 'queued', ?, 0, 0, 0, ?, '{"items":[]}', ?)
`, siteID, len(ids), string(inputJSON), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	m.startBatchAccountTest(id, siteID, input)
	return m.get(ctx, id)
}

func (m *jobManager) createBatchTokenRefresh(ctx context.Context, siteID int64, input batchTokenRefreshInput) (*jobRecord, error) {
	ids := cleanAccountIDs(input.IDs)
	if len(ids) == 0 {
		return nil, errors.New("ids is required")
	}
	input.IDs = ids
	input.AccountMeta = cleanAccountMeta(input.AccountMeta)
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := m.db.ExecContext(ctx, `
INSERT INTO jobs (site_id, type, status, total_count, done_count, success_count, failed_count, input_json, result_json, created_at)
VALUES (?, 'batch_token_refresh', 'queued', ?, 0, 0, 0, ?, '{}', ?)
`, siteID, len(ids), string(inputJSON), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	m.startBatchTokenRefresh(id, siteID, input)
	return m.get(ctx, id)
}

func (m *jobManager) createImportAccounts(ctx context.Context, siteID int64, input importAccountsInput) (*jobRecord, error) {
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" {
		return nil, errors.New("text is required")
	}
	if len(input.Text) > 2<<20 {
		return nil, errors.New("import input is too large")
	}
	if input.Confirmation.SiteID != 0 && input.Confirmation.SiteID != siteID {
		return nil, errors.New("confirmed site does not match active site")
	}
	input.Settings = cleanImportAccountSettings(input.Settings)
	accounts, err := buildImportAccounts(input.Text, input.Settings)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, errors.New("no importable accounts found")
	}
	if err := m.validateImportReferences(ctx, siteID, input.Settings); err != nil {
		return nil, err
	}
	site, _ := m.siteService.Get(ctx, siteID)
	siteSummary := map[string]any{"id": siteID}
	if site != nil {
		siteSummary["name"] = site.Name
		siteSummary["baseUrl"] = site.BaseURL
	}
	safeInput := map[string]any{
		"filename": input.Filename,
		"settings": safeImportExecutionSettings(input.Settings),
		"summary":  map[string]any{"total": len(accounts)},
		"site":     siteSummary,
	}
	inputJSON, err := json.Marshal(safeInput)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := m.db.ExecContext(ctx, `
INSERT INTO jobs (site_id, type, status, total_count, done_count, success_count, failed_count, input_json, result_json, created_at)
VALUES (?, 'import_accounts', 'queued', ?, 0, 0, 0, ?, '{"items":[]}', ?)
`, siteID, len(accounts), string(inputJSON), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	m.startImportAccounts(id, siteID, accounts, input.Settings)
	return m.get(ctx, id)
}

func (m *jobManager) startBatchAccountTest(jobID, siteID int64, input batchAccountTestInput) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[jobID] = cancel
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.cancels, jobID)
			m.mu.Unlock()
		}()
		m.runBatchAccountTest(ctx, jobID, siteID, input)
	}()
}

func (m *jobManager) startBatchTokenRefresh(jobID, siteID int64, input batchTokenRefreshInput) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[jobID] = cancel
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.cancels, jobID)
			m.mu.Unlock()
		}()
		m.runBatchTokenRefresh(ctx, jobID, siteID, input)
	}()
}

func (m *jobManager) startImportAccounts(jobID, siteID int64, accounts []importAccountExecution, settings importAccountSettings) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[jobID] = cancel
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.cancels, jobID)
			m.mu.Unlock()
		}()
		m.runImportAccounts(ctx, jobID, siteID, accounts, settings)
	}()
}

func (m *jobManager) runBatchAccountTest(ctx context.Context, jobID, siteID int64, input batchAccountTestInput) {
	now := time.Now().Unix()
	_, _ = m.db.ExecContext(context.Background(), `UPDATE jobs SET status = 'running', started_at = ? WHERE id = ?`, now, jobID)
	slog.Info("job started", "job_id", jobID, "type", "batch_account_test", "site_id", siteID, "total_count", len(input.IDs))
	items := make([]map[string]any, 0, len(input.IDs))
	successCount := 0
	failedCount := 0
	for index, accountID := range input.IDs {
		if ctx.Err() != nil {
			_ = m.finishJob(jobID, "cancelled", items, successCount, failedCount, ctx.Err().Error())
			slog.Warn("job cancelled", "job_id", jobID, "type", "batch_account_test", "done_count", len(items), "error", ctx.Err())
			return
		}
		if index > 0 && !waitForJobDelay(ctx, len(input.IDs), input.DelayMs, input.JitterMs) {
			_ = m.finishJob(jobID, "cancelled", items, successCount, failedCount, context.Canceled.Error())
			slog.Warn("job cancelled during delay", "job_id", jobID, "type", "batch_account_test", "done_count", len(items))
			return
		}
		slog.Debug("job item started", "job_id", jobID, "type", "batch_account_test", "site_id", siteID, "account_id", accountID, "index", index+1, "total_count", len(input.IDs))
		result := runAccountTest(ctx, m.siteService, m.logDir, siteID, accountID, input)
		applyAccountMeta(result, input.AccountMeta)
		if ctx.Err() != nil {
			items = append(items, result)
			failedCount++
			_ = m.updateJobProgress(jobID, items, successCount, failedCount)
			_ = m.finishJob(jobID, "cancelled", items, successCount, failedCount, ctx.Err().Error())
			slog.Warn("job cancelled", "job_id", jobID, "type", "batch_account_test", "done_count", len(items), "error", ctx.Err())
			return
		}
		if result["ok"] == true {
			successCount++
		} else {
			failedCount++
		}
		items = append(items, result)
		_ = m.updateJobProgress(jobID, items, successCount, failedCount)
		slog.Debug("job item finished", "job_id", jobID, "type", "batch_account_test", "site_id", siteID, "account_id", accountID, "ok", result["ok"], "status_code", result["statusCode"], "done_count", len(items))
	}
	status := "succeeded"
	if failedCount > 0 {
		status = "failed"
	}
	_ = m.finishJob(jobID, status, items, successCount, failedCount, "")
	slog.Info("job finished", "job_id", jobID, "type", "batch_account_test", "status", status, "success_count", successCount, "failed_count", failedCount)
}

func (m *jobManager) runBatchTokenRefresh(ctx context.Context, jobID, siteID int64, input batchTokenRefreshInput) {
	now := time.Now().Unix()
	_, _ = m.db.ExecContext(context.Background(), `UPDATE jobs SET status = 'running', started_at = ? WHERE id = ?`, now, jobID)
	slog.Info("job started", "job_id", jobID, "type", "batch_token_refresh", "site_id", siteID, "total_count", len(input.IDs))
	if ctx.Err() != nil {
		_ = m.finishJob(jobID, "cancelled", nil, 0, 0, ctx.Err().Error())
		slog.Warn("job cancelled", "job_id", jobID, "type", "batch_token_refresh", "error", ctx.Err())
		return
	}
	started := time.Now()
	slog.Debug("token refresh upstream request started", "job_id", jobID, "site_id", siteID, "account_count", len(input.IDs))
	data, statusCode, err := m.siteService.AdminPOSTJSON(ctx, siteID, "/api/v1/admin/accounts/batch-refresh", map[string]any{
		"account_ids": input.IDs,
	})
	durationMS := time.Since(started).Milliseconds()
	if err != nil {
		slog.Warn("token refresh upstream request failed", "job_id", jobID, "site_id", siteID, "status_code", statusCode, "duration_ms", durationMS, "error", err)
	} else {
		slog.Debug("token refresh upstream request finished", "job_id", jobID, "site_id", siteID, "status_code", statusCode, "duration_ms", durationMS)
	}
	if ctx.Err() != nil {
		items := buildTokenRefreshItems(input.IDs, input.AccountMeta, statusCode, durationMS, data, ctx.Err())
		_ = m.updateJobProgress(jobID, items, 0, len(items))
		_ = m.finishJob(jobID, "cancelled", items, 0, len(items), ctx.Err().Error())
		slog.Warn("job cancelled", "job_id", jobID, "type", "batch_token_refresh", "done_count", len(items), "error", ctx.Err())
		return
	}
	items := buildTokenRefreshItems(input.IDs, input.AccountMeta, statusCode, durationMS, data, err)
	successCount, failedCount := countJobItems(items)
	_ = m.updateJobProgress(jobID, items, successCount, failedCount)
	status := "succeeded"
	if failedCount > 0 {
		status = "failed"
	}
	_ = m.finishJob(jobID, status, items, successCount, failedCount, "")
	slog.Info("job finished", "job_id", jobID, "type", "batch_token_refresh", "status", status, "success_count", successCount, "failed_count", failedCount, "duration_ms", durationMS)
}

func (m *jobManager) runImportAccounts(ctx context.Context, jobID, siteID int64, accounts []importAccountExecution, settings importAccountSettings) {
	now := time.Now().Unix()
	_, _ = m.db.ExecContext(context.Background(), `UPDATE jobs SET status = 'running', started_at = ? WHERE id = ?`, now, jobID)
	slog.Warn("job started", "job_id", jobID, "type", "import_accounts", "site_id", siteID, "total_count", len(accounts))
	if ctx.Err() != nil {
		_ = m.finishJob(jobID, "cancelled", nil, 0, 0, ctx.Err().Error())
		return
	}
	requestAccounts := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		payload := map[string]any{
			"name":        account.Name,
			"platform":    account.Platform,
			"type":        account.Type,
			"credentials": account.Credentials,
			"group_ids":   settings.GroupIDs,
		}
		if len(account.Extra) > 0 {
			payload["extra"] = account.Extra
		}
		if settings.ProxyID != nil {
			payload["proxy_id"] = *settings.ProxyID
		}
		if settings.Priority != nil {
			payload["priority"] = *settings.Priority
		}
		if settings.Concurrency != nil {
			payload["concurrency"] = *settings.Concurrency
		}
		requestAccounts = append(requestAccounts, payload)
	}
	started := time.Now()
	data, statusCode, err := m.siteService.AdminPOSTJSONWithHeaders(ctx, siteID, "/api/v1/admin/accounts/batch", map[string]any{"accounts": requestAccounts}, map[string]string{
		"Idempotency-Key": fmt.Sprintf("subadmin-import-%d", jobID),
	})
	durationMS := time.Since(started).Milliseconds()
	if err != nil {
		slog.Warn("import accounts upstream request failed", "job_id", jobID, "site_id", siteID, "status_code", statusCode, "duration_ms", durationMS, "error", err)
	} else {
		slog.Info("import accounts upstream request finished", "job_id", jobID, "site_id", siteID, "status_code", statusCode, "duration_ms", durationMS)
	}
	items := buildImportAccountItems(accounts, statusCode, durationMS, data, err)
	successCount, failedCount := countJobItems(items)
	_ = m.updateJobProgress(jobID, items, successCount, failedCount)
	status := "succeeded"
	if failedCount > 0 {
		status = "failed"
	}
	_ = m.finishJob(jobID, status, items, successCount, failedCount, "")
	slog.Info("job finished", "job_id", jobID, "type", "import_accounts", "status", status, "success_count", successCount, "failed_count", failedCount, "duration_ms", durationMS)
}

func buildImportAccountItems(accounts []importAccountExecution, statusCode int, durationMS int64, data []byte, err error) []map[string]any {
	items := make([]map[string]any, 0, len(accounts))
	if err != nil {
		for _, account := range accounts {
			items = append(items, map[string]any{"id": account.Index, "index": account.Index, "name": account.Name, "platform": account.Platform, "type": account.Type, "ok": false, "statusCode": statusCode, "durationMs": durationMS, "error": err.Error()})
		}
		return items
	}
	response := decodeJSONValue(sanitizeJSONForBrowser(data))
	dataMap := unwrapResponseMap(response)
	results, _ := dataMap["results"].([]any)
	for index, account := range accounts {
		item := map[string]any{"id": account.Index, "index": account.Index, "name": account.Name, "platform": account.Platform, "type": account.Type, "statusCode": statusCode, "durationMs": durationMS}
		var result map[string]any
		if index < len(results) {
			result, _ = results[index].(map[string]any)
		}
		if result == nil {
			ok := statusCode >= 200 && statusCode < 300
			item["ok"] = ok
			if !ok {
				item["error"] = fmt.Sprintf("HTTP %d", statusCode)
			}
			items = append(items, item)
			continue
		}
		item["ok"] = result["success"] == true
		if createdID := int64FromAny(result["id"]); createdID > 0 {
			item["accountId"] = createdID
		}
		if message := strings.TrimSpace(fmt.Sprint(result["error"])); message != "" && message != "<nil>" {
			item["error"] = message
		}
		items = append(items, item)
	}
	return items
}

func buildTokenRefreshItems(ids []int64, meta map[string]accountMeta, statusCode int, durationMS int64, data []byte, err error) []map[string]any {
	items := make([]map[string]any, 0, len(ids))
	if err != nil {
		for _, id := range ids {
			item := map[string]any{"id": id, "ok": false, "statusCode": statusCode, "durationMs": durationMS, "hint": "刷新失败", "error": err.Error()}
			applyAccountMeta(item, meta)
			items = append(items, item)
		}
		return items
	}
	response := decodeJSONValue(sanitizeJSONForBrowser(data))
	dataMap := unwrapResponseMap(response)
	errorsByID := issueTextByAccountID(dataMap["errors"], "error")
	warningsByID := issueTextByAccountID(dataMap["warnings"], "warning")
	for _, id := range ids {
		item := map[string]any{"id": id, "statusCode": statusCode, "durationMs": durationMS}
		applyAccountMeta(item, meta)
		key := strconv.FormatInt(id, 10)
		if message := errorsByID[key]; message != "" || statusCode < 200 || statusCode >= 300 {
			item["ok"] = false
			item["hint"] = "刷新失败"
			if message != "" {
				item["error"] = message
			} else {
				item["error"] = fmt.Sprintf("HTTP %d", statusCode)
			}
		} else {
			item["ok"] = true
			item["hint"] = "刷新成功"
			if warning := warningsByID[key]; warning != "" {
				item["warning"] = warning
				item["message"] = warning
			}
		}
		items = append(items, item)
	}
	return items
}

func unwrapResponseMap(value any) map[string]any {
	root, _ := value.(map[string]any)
	if data, ok := root["data"].(map[string]any); ok {
		return data
	}
	return root
}

func issueTextByAccountID(value any, field string) map[string]string {
	result := map[string]string{}
	items, ok := value.([]any)
	if !ok {
		return result
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := int64FromAny(item["account_id"])
		if id <= 0 {
			id = int64FromAny(item["id"])
		}
		if id <= 0 {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(item[field]))
		if text == "<nil>" || text == "" {
			text = strings.TrimSpace(fmt.Sprint(item["message"]))
		}
		if text != "" && text != "<nil>" {
			result[strconv.FormatInt(id, 10)] = text
		}
	}
	return result
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}

func countJobItems(items []map[string]any) (int, int) {
	successCount := 0
	failedCount := 0
	for _, item := range items {
		if item["ok"] == true {
			successCount++
		} else {
			failedCount++
		}
	}
	return successCount, failedCount
}

func waitForJobDelay(ctx context.Context, totalCount, delayMs, jitterMs int) bool {
	delay := delayMs
	if delay < 0 {
		delay = 0
	}
	floor := batchDelayFloor(totalCount)
	if delay < floor {
		delay = floor
	}
	if jitterMs > 0 {
		delay += int(time.Now().UnixNano()%int64((jitterMs*2)+1)) - jitterMs
		if delay < floor {
			delay = floor
		}
	}
	timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func batchDelayFloor(totalCount int) int {
	if totalCount > 100 {
		return 1500
	}
	if totalCount > 50 {
		return 1000
	}
	if totalCount > 20 {
		return 600
	}
	return 0
}

func (m *jobManager) updateJobProgress(jobID int64, items []map[string]any, successCount, failedCount int) error {
	resultJSON, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(context.Background(), `
UPDATE jobs SET done_count = ?, success_count = ?, failed_count = ?, result_json = ? WHERE id = ?
`, len(items), successCount, failedCount, string(resultJSON), jobID)
	return err
}

func (m *jobManager) finishJob(jobID int64, status string, items []map[string]any, successCount, failedCount int, errorMessage string) error {
	resultJSON, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = m.db.ExecContext(context.Background(), `
UPDATE jobs SET status = ?, done_count = ?, success_count = ?, failed_count = ?, result_json = ?, error_message = ?, finished_at = ? WHERE id = ?
`, status, len(items), successCount, failedCount, string(resultJSON), errorMessage, now, jobID)
	return err
}

func (m *jobManager) list(ctx context.Context, limit int) ([]jobRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := m.db.QueryContext(ctx, `
SELECT id, site_id, type, status, total_count, done_count, success_count, failed_count, input_json, result_json, error_message, created_at, started_at, finished_at
FROM jobs ORDER BY id DESC LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []jobRecord{}
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (m *jobManager) get(ctx context.Context, id int64) (*jobRecord, error) {
	row := m.db.QueryRowContext(ctx, `
SELECT id, site_id, type, status, total_count, done_count, success_count, failed_count, input_json, result_json, error_message, created_at, started_at, finished_at
FROM jobs WHERE id = ?
`, id)
	item, err := scanJob(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *jobManager) cancel(ctx context.Context, id int64) (*jobRecord, error) {
	m.mu.Lock()
	cancel := m.cancels[id]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	now := time.Now().Unix()
	res, err := m.db.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', error_message = 'cancelled', finished_at = ? WHERE id = ? AND status IN ('queued', 'running')`, now, id)
	if err != nil {
		return nil, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		if _, err := m.get(ctx, id); err != nil {
			return nil, err
		}
	}
	return m.get(ctx, id)
}

func (m *jobManager) retryFailed(ctx context.Context, id int64) (*jobRecord, error) {
	job, err := m.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Type != "batch_account_test" && job.Type != "batch_token_refresh" {
		return nil, errors.New("job type cannot be retried")
	}
	failedIDs := failedIDsFromJobResult(job.Result)
	if len(failedIDs) == 0 {
		return nil, errors.New("job has no failed account ids")
	}
	if job.SiteID == nil {
		return nil, errors.New("job has no site")
	}
	if job.Type == "batch_token_refresh" {
		var input batchTokenRefreshInput
		if err := json.Unmarshal(job.Input, &input); err != nil {
			return nil, err
		}
		input.IDs = failedIDs
		return m.createBatchTokenRefresh(ctx, *job.SiteID, input)
	}
	var input batchAccountTestInput
	if err := json.Unmarshal(job.Input, &input); err != nil {
		return nil, err
	}
	input.IDs = failedIDs
	return m.createBatchAccountTest(ctx, *job.SiteID, input)
}

func (m *jobManager) markInterrupted(ctx context.Context) error {
	now := time.Now().Unix()
	_, err := m.db.ExecContext(ctx, `UPDATE jobs SET status = 'failed', error_message = 'interrupted by restart', finished_at = ? WHERE status IN ('queued', 'running')`, now)
	return err
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(row jobScanner) (jobRecord, error) {
	var item jobRecord
	var siteID sql.NullInt64
	var startedAt, finishedAt sql.NullInt64
	var inputJSON, resultJSON string
	err := row.Scan(&item.ID, &siteID, &item.Type, &item.Status, &item.TotalCount, &item.DoneCount, &item.SuccessCount, &item.FailedCount, &inputJSON, &resultJSON, &item.Error, &item.CreatedAt, &startedAt, &finishedAt)
	if err != nil {
		return item, err
	}
	if siteID.Valid {
		item.SiteID = &siteID.Int64
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.Int64
	}
	if finishedAt.Valid {
		item.FinishedAt = &finishedAt.Int64
	}
	item.Input = json.RawMessage(inputJSON)
	item.Result = json.RawMessage(resultJSON)
	return item, nil
}

func failedIDsFromJobResult(raw json.RawMessage) []int64 {
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	ids := []int64{}
	for _, item := range payload.Items {
		if item["ok"] == true {
			continue
		}
		switch value := item["id"].(type) {
		case float64:
			ids = append(ids, int64(value))
		case string:
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				ids = append(ids, parsed)
			}
		}
	}
	return cleanAccountIDs(ids)
}

func cleanAccountIDs(ids []int64) []int64 {
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func cleanAccountMeta(meta map[string]accountMeta) map[string]accountMeta {
	if len(meta) == 0 {
		return nil
	}
	cleaned := map[string]accountMeta{}
	for key, value := range meta {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(value.Name)
		note := strings.TrimSpace(value.Note)
		if name == "" && note == "" {
			continue
		}
		cleaned[id] = accountMeta{Name: name, Note: note}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func applyAccountMeta(result map[string]any, meta map[string]accountMeta) {
	if len(meta) == 0 {
		return
	}
	id := strings.TrimSpace(fmt.Sprint(result["id"]))
	if id == "" {
		return
	}
	value, ok := meta[id]
	if !ok {
		return
	}
	if value.Name != "" {
		result["name"] = value.Name
	}
	if value.Note != "" {
		result["note"] = value.Note
	}
}

type importPreviewInput struct {
	Text     string         `json:"text"`
	Filename string         `json:"filename"`
	Settings map[string]any `json:"settings"`
	Limit    int            `json:"limit"`
	Offset   int            `json:"offset"`
}

type importAccountsInput struct {
	Text         string                `json:"text"`
	Filename     string                `json:"filename"`
	Settings     importAccountSettings `json:"settings"`
	Confirmation importAccountConfirm  `json:"confirmation"`
}

type importAccountSettings struct {
	NamePrefix  string   `json:"namePrefix"`
	GroupIDs    []int64  `json:"groupIds"`
	ProxyID     *int64   `json:"proxyId"`
	Models      []string `json:"models"`
	Priority    *int     `json:"priority"`
	Concurrency *int     `json:"concurrency"`
}

type importAccountConfirm struct {
	Confirmed   bool   `json:"confirmed"`
	SiteID      int64  `json:"siteId"`
	SiteName    string `json:"siteName"`
	SiteBaseURL string `json:"siteBaseUrl"`
}

type importAccountExecution struct {
	Index       int            `json:"index"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"-"`
	Extra       map[string]any `json:"-"`
}

type importTemplateRecord struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	SiteID    *int64          `json:"siteId,omitempty"`
	Template  json.RawMessage `json:"template"`
	Enabled   bool            `json:"enabled"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

func importTemplatesHandler(authManager *auth.Manager, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			items, err := listImportTemplates(r.Context(), database)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "list import templates failed")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodPost:
			var input struct {
				Name     string         `json:"name"`
				SiteID   *int64         `json:"siteId"`
				Template map[string]any `json:"template"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json body")
				return
			}
			item, err := createImportTemplate(r.Context(), database, input.Name, input.SiteID, input.Template)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeAuditLog(database, r, input.SiteID, "import_template.create", "import_template", 1, map[string]any{"name": item.Name}, map[string]any{"ok": true, "templateId": item.ID})
			writeJSON(w, http.StatusCreated, item)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func importTemplateDetailHandler(authManager *auth.Manager, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r, authManager) {
			return
		}
		id, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/import-templates/"), "/"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := deleteImportTemplate(r.Context(), database, id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeAuditLog(database, r, nil, "import_template.delete", "import_template", 1, map[string]any{"templateId": id}, map[string]any{"ok": true})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func listImportTemplates(ctx context.Context, database *sql.DB) ([]importTemplateRecord, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, name, site_id, template_json, enabled, created_at, updated_at FROM import_templates WHERE enabled = 1 ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []importTemplateRecord{}
	for rows.Next() {
		var item importTemplateRecord
		var siteID sql.NullInt64
		var templateJSON string
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &siteID, &templateJSON, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if siteID.Valid {
			item.SiteID = &siteID.Int64
		}
		item.Template = json.RawMessage(templateJSON)
		item.Enabled = enabled == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func createImportTemplate(ctx context.Context, database *sql.DB, name string, siteID *int64, template map[string]any) (*importTemplateRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	templateJSON, err := json.Marshal(sanitizeImportSettings(template))
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	var nullableSiteID any
	if siteID != nil && *siteID > 0 {
		nullableSiteID = *siteID
	}
	res, err := database.ExecContext(ctx, `INSERT INTO import_templates (name, site_id, template_json, enabled, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)`, name, nullableSiteID, string(templateJSON), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &importTemplateRecord{ID: id, Name: name, SiteID: siteID, Template: json.RawMessage(templateJSON), Enabled: true, CreatedAt: now, UpdatedAt: now}, nil
}

func deleteImportTemplate(ctx context.Context, database *sql.DB, id int64) error {
	res, err := database.ExecContext(ctx, `UPDATE import_templates SET enabled = 0, updated_at = ? WHERE id = ?`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("template not found")
	}
	return nil
}

type importPreviewItem struct {
	Index              int      `json:"index"`
	Recognized         bool     `json:"recognized"`
	Platform           string   `json:"platform"`
	Type               string   `json:"type"`
	Name               string   `json:"name"`
	Group              string   `json:"group,omitempty"`
	AppliedGroups      []string `json:"appliedGroups,omitempty"`
	AppliedProxy       string   `json:"appliedProxy,omitempty"`
	AppliedModels      []string `json:"appliedModels,omitempty"`
	AppliedPriority    string   `json:"appliedPriority,omitempty"`
	AppliedConcurrency string   `json:"appliedConcurrency,omitempty"`
	CredentialFields   []string `json:"credentialFields"`
	MissingFields      []string `json:"missingFields"`
	Warnings           []string `json:"warnings"`
	DuplicateKey       string   `json:"duplicateKey,omitempty"`
	RawPreview         string   `json:"rawPreview,omitempty"`
}

func writeImportPreview(w http.ResponseWriter, r *http.Request, database *sql.DB, siteID int64) {
	input, err := decodeImportPreviewInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings := sanitizeImportSettings(input.Settings)
	items, warnings := parseImportPreview(input.Text, settings)
	markImportDuplicates(items)
	pageItems := paginateImportItems(items, input.Offset, input.Limit)
	summary := map[string]any{
		"total":      len(items),
		"recognized": countImportItems(items, func(item importPreviewItem) bool { return item.Recognized }),
		"invalid":    countImportItems(items, func(item importPreviewItem) bool { return !item.Recognized }),
		"duplicates": countImportItems(items, func(item importPreviewItem) bool { return item.DuplicateKey != "" }),
	}
	slog.InfoContext(r.Context(), "import preview generated", "site_id", siteID, "filename", input.Filename, "total", summary["total"], "returned", len(pageItems), "offset", input.Offset, "limit", input.Limit, "recognized", summary["recognized"], "invalid", summary["invalid"], "duplicates", summary["duplicates"])
	writeAuditLog(database, r, &siteID, "import.preview", "account", len(items), map[string]any{"filename": input.Filename, "settings": settings}, map[string]any{"summary": summary})
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    pageItems,
		"warnings": warnings,
		"errors":   []string{},
		"summary":  summary,
		"settings": settings,
		"page": map[string]any{
			"offset": input.Offset,
			"limit":  input.Limit,
			"count":  len(pageItems),
		},
	})
}

func decodeImportPreviewInput(r *http.Request) (importPreviewInput, error) {
	const maxBodyBytes = 2 << 20
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(maxBodyBytes); err != nil {
			return importPreviewInput{}, errors.New("invalid multipart form")
		}
		input := importPreviewInput{Text: strings.TrimSpace(r.FormValue("text")), Settings: map[string]any{}}
		for _, key := range []string{"defaultGroup", "proxy", "priority", "concurrency", "namePrefix"} {
			if value := strings.TrimSpace(r.FormValue(key)); value != "" {
				input.Settings[key] = value
			}
		}
		file, header, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, maxBodyBytes+1))
			if err != nil {
				return importPreviewInput{}, errors.New("read upload failed")
			}
			if len(data) > maxBodyBytes {
				return importPreviewInput{}, errors.New("import preview input is too large")
			}
			input.Filename = header.Filename
			if input.Text != "" {
				input.Text += "\n"
			}
			input.Text += string(data)
		}
		if strings.TrimSpace(input.Text) == "" {
			return importPreviewInput{}, errors.New("text or file is required")
		}
		input.Limit = 10
		return input, nil
	}
	var input importPreviewInput
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes+1))
	if err := decoder.Decode(&input); err != nil {
		return importPreviewInput{}, errors.New("invalid json body")
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" {
		return importPreviewInput{}, errors.New("text is required")
	}
	if len(input.Text) > maxBodyBytes {
		return importPreviewInput{}, errors.New("import preview input is too large")
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 10
	}
	if input.Offset < 0 {
		input.Offset = 0
	}
	return input, nil
}

func paginateImportItems(items []importPreviewItem, offset, limit int) []importPreviewItem {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []importPreviewItem{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func parseImportPreview(text string, settings map[string]any) ([]importPreviewItem, []string) {
	chunks := splitImportChunks(text)
	items := make([]importPreviewItem, 0, len(chunks))
	warnings := []string{}
	for index, chunk := range chunks {
		item := buildImportPreviewItem(index+1, chunk)
		applyImportPreviewSettings(&item, settings)
		if !item.Recognized {
			warnings = append(warnings, fmt.Sprintf("第 %d 条未识别为账号格式", index+1))
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		warnings = append(warnings, "未解析到账号条目")
	}
	return items, warnings
}

func applyImportPreviewSettings(item *importPreviewItem, settings map[string]any) {
	if item == nil || len(settings) == 0 {
		return
	}
	namePrefix := renderImportNamePrefix(strings.TrimSpace(fmt.Sprint(settings["namePrefix"])), time.Now())
	if namePrefix != "" && item.Name != "" && !strings.HasPrefix(item.Name, namePrefix) {
		item.Name = namePrefix + item.Name
	}
	item.AppliedGroups = stringSliceSetting(settings["groups"])
	if len(item.AppliedGroups) == 0 {
		if group := strings.TrimSpace(fmt.Sprint(settings["defaultGroup"])); group != "" && group != "<nil>" {
			item.AppliedGroups = []string{group}
		}
	}
	if proxy := strings.TrimSpace(fmt.Sprint(settings["proxy"])); proxy != "" && proxy != "<nil>" {
		item.AppliedProxy = proxy
	}
	item.AppliedModels = stringSliceSetting(settings["models"])
	if priority := strings.TrimSpace(fmt.Sprint(settings["priority"])); priority != "" && priority != "<nil>" {
		item.AppliedPriority = priority
	}
	if concurrency := strings.TrimSpace(fmt.Sprint(settings["concurrency"])); concurrency != "" && concurrency != "<nil>" {
		item.AppliedConcurrency = concurrency
	}
}

func stringSliceSetting(value any) []string {
	result := []string{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
	case []string:
		for _, item := range typed {
			if text := strings.TrimSpace(item); text != "" {
				result = append(result, text)
			}
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			if text := strings.TrimSpace(item); text != "" {
				result = append(result, text)
			}
		}
	}
	return result
}

func splitImportChunks(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err == nil {
		switch typed := value.(type) {
		case []any:
			chunks := make([]string, 0, len(typed))
			for _, item := range typed {
				if data, err := json.Marshal(item); err == nil {
					chunks = append(chunks, string(data))
				}
			}
			return chunks
		case map[string]any:
			if accounts, ok := typed["accounts"].([]any); ok {
				chunks := make([]string, 0, len(accounts))
				for _, item := range accounts {
					if data, err := json.Marshal(item); err == nil {
						chunks = append(chunks, string(data))
					}
				}
				return chunks
			}
			return []string{trimmed}
		}
	}
	parts := regexp.MustCompile(`\n\s*\n+`).Split(trimmed, -1)
	if len(parts) > 1 {
		return compactStrings(parts)
	}
	return compactStrings(strings.Split(trimmed, "\n"))
}

func buildImportPreviewItem(index int, chunk string) importPreviewItem {
	fields := parseImportFields(chunk)
	item := importPreviewItem{Index: index, CredentialFields: []string{}, MissingFields: []string{}, Warnings: []string{}, RawPreview: safeRawPreview(chunk)}
	item.Platform = firstNonEmpty(fields, "platform", "provider", "vendor")
	item.Type = firstNonEmpty(fields, "type", "account_type", "kind")
	item.Name = firstNonEmpty(fields, "name", "email", "username", "label")
	item.Group = firstNonEmpty(fields, "group", "group_name", "group_id")
	if item.Platform == "" {
		item.Platform = inferImportPlatform(fields, chunk)
	}
	if item.Type == "" {
		item.Type = inferImportType(fields, chunk)
	}
	if item.Name == "" && fields["id"] != "" {
		item.Name = "账号 #" + fields["id"]
	}
	item.CredentialFields = credentialFieldNames(fields)
	if item.Platform == "" {
		item.MissingFields = append(item.MissingFields, "platform")
	}
	if item.Type == "" {
		item.MissingFields = append(item.MissingFields, "type")
	}
	if len(item.CredentialFields) == 0 {
		item.MissingFields = append(item.MissingFields, "credentials")
	}
	if item.Name == "" {
		item.Warnings = append(item.Warnings, "缺少显示名，将需要导入设置生成名称")
	}
	item.Recognized = len(item.MissingFields) == 0
	if item.Name != "" {
		item.DuplicateKey = strings.ToLower(item.Platform + ":" + item.Type + ":" + item.Name)
	}
	return item
}

func parseImportFields(chunk string) map[string]string {
	fields := map[string]string{}
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(chunk)), &value); err == nil {
		flattenImportFields(fields, "", value)
		return fields
	}
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(strings.Trim(line, ","))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			key, value, ok = strings.Cut(line, ":")
		}
		if !ok {
			continue
		}
		key = normalizeImportKey(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && value != "" {
			fields[key] = value
		}
	}
	return fields
}

func flattenImportFields(fields map[string]string, prefix string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			nextKey := normalizeImportKey(key)
			if prefix != "" {
				nextKey = prefix + "." + nextKey
			}
			flattenImportFields(fields, nextKey, child)
		}
	case string:
		if prefix != "" && strings.TrimSpace(typed) != "" {
			fields[prefix] = strings.TrimSpace(typed)
		}
	case float64, bool:
		if prefix != "" {
			fields[prefix] = fmt.Sprint(typed)
		}
	}
}

func normalizeImportKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.Trim(key, `"'`)
	key = strings.ReplaceAll(key, "-", "_")
	return key
}

func firstNonEmpty(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func inferImportPlatform(fields map[string]string, chunk string) string {
	lower := strings.ToLower(chunk)
	for _, platform := range []string{"anthropic", "openai", "gemini", "antigravity"} {
		if strings.Contains(lower, platform) {
			return platform
		}
	}
	if fields["credentials.refresh_token"] != "" || fields["refresh_token"] != "" {
		return "anthropic"
	}
	return ""
}

func inferImportType(fields map[string]string, chunk string) string {
	lower := strings.ToLower(chunk)
	if fields["api_key"] != "" || fields["credentials.api_key"] != "" || strings.Contains(lower, "api_key") {
		return "apikey"
	}
	if fields["refresh_token"] != "" || fields["credentials.refresh_token"] != "" || strings.Contains(lower, "refresh_token") {
		return "oauth"
	}
	if strings.Contains(lower, "setup") {
		return "setup-token"
	}
	return ""
}

func credentialFieldNames(fields map[string]string) []string {
	result := []string{}
	for key, value := range fields {
		if value == "" {
			continue
		}
		base := key
		if parts := strings.Split(key, "."); len(parts) > 1 {
			base = parts[len(parts)-1]
		}
		if isSensitiveKey(base) || strings.Contains(base, "token") || strings.Contains(base, "credential") {
			result = append(result, base)
		}
	}
	slices.Sort(result)
	return result
}

func markImportDuplicates(items []importPreviewItem) {
	seen := map[string]int{}
	for index := range items {
		key := items[index].DuplicateKey
		if key == "" {
			continue
		}
		seen[key]++
		if seen[key] > 1 {
			items[index].Warnings = append(items[index].Warnings, "疑似重复账号")
		}
	}
	for index := range items {
		if key := items[index].DuplicateKey; key != "" && seen[key] <= 1 {
			items[index].DuplicateKey = ""
		}
	}
}

func safeRawPreview(chunk string) string {
	cleaned := sanitizeSecretText(strings.TrimSpace(chunk))
	if len(cleaned) > 180 {
		return cleaned[:180] + "..."
	}
	return cleaned
}

func sanitizeSecretText(value string) string {
	return redactSensitiveAssignments(value)
}

func sanitizeImportSettings(settings map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range settings {
		cleanKey := normalizeImportKey(key)
		if isSensitiveKey(cleanKey) {
			continue
		}
		result[key] = value
	}
	return result
}

func buildImportAccounts(text string, settings importAccountSettings) ([]importAccountExecution, error) {
	chunks := splitImportChunks(text)
	accounts := make([]importAccountExecution, 0, len(chunks))
	for index, chunk := range chunks {
		preview := buildImportPreviewItem(index+1, chunk)
		if !preview.Recognized {
			return nil, fmt.Errorf("第 %d 条缺少必要字段: %s", index+1, strings.Join(preview.MissingFields, ", "))
		}
		raw, err := parseImportAccountObject(chunk)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条解析失败: %w", index+1, err)
		}
		credentials := mapFromAny(raw["credentials"])
		if credentials == nil {
			credentials = credentialsFromFlatFields(parseImportFields(chunk))
		}
		if len(credentials) == 0 {
			return nil, fmt.Errorf("第 %d 条缺少 credentials", index+1)
		}
		models := cleanStringList(settings.Models)
		if len(models) > 0 {
			credentials["model_mapping"] = identityModelMapping(models)
		}
		extra := mapFromAny(raw["extra"])
		name := strings.TrimSpace(preview.Name)
		if name == "" {
			name = fmt.Sprintf("导入账号 #%d", index+1)
		}
		namePrefix := renderImportNamePrefix(settings.NamePrefix, time.Now())
		if namePrefix != "" && !strings.HasPrefix(name, namePrefix) {
			name = namePrefix + name
		}
		accounts = append(accounts, importAccountExecution{
			Index:       index + 1,
			Name:        name,
			Platform:    strings.TrimSpace(preview.Platform),
			Type:        strings.TrimSpace(preview.Type),
			Credentials: credentials,
			Extra:       extra,
		})
	}
	return accounts, nil
}

func parseImportAccountObject(chunk string) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(chunk)), &raw); err == nil {
		return raw, nil
	}
	fields := parseImportFields(chunk)
	result := map[string]any{}
	for key, value := range fields {
		result[key] = value
	}
	return result, nil
}

func credentialsFromFlatFields(fields map[string]string) map[string]any {
	credentials := map[string]any{}
	for key, value := range fields {
		if value == "" {
			continue
		}
		credKey := ""
		if strings.HasPrefix(key, "credentials.") {
			credKey = strings.TrimPrefix(key, "credentials.")
		} else if isCredentialImportKey(key) {
			credKey = key
		}
		if credKey != "" {
			credentials[credKey] = value
		}
	}
	return credentials
}

func isCredentialImportKey(key string) bool {
	switch key {
	case "access_token", "refresh_token", "id_token", "api_key", "base_url", "client_id", "account_id", "chatgpt_account_id", "account_uuid", "org_uuid", "project_id", "tier_id", "service_account_json", "aws_access_key_id", "aws_secret_access_key", "aws_session_token", "aws_region", "auth_mode":
		return true
	default:
		return strings.Contains(key, "token") || strings.Contains(key, "cookie") || strings.Contains(key, "credential")
	}
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		result := map[string]any{}
		for key, child := range typed {
			result[key] = child
		}
		return result
	}
	return nil
}

func identityModelMapping(models []string) map[string]string {
	mapping := map[string]string{}
	for _, model := range cleanStringList(models) {
		mapping[model] = model
	}
	return mapping
}

func cleanStringList(items []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		text := strings.TrimSpace(item)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		result = append(result, text)
	}
	return result
}

func cleanImportAccountSettings(settings importAccountSettings) importAccountSettings {
	settings.NamePrefix = strings.TrimSpace(settings.NamePrefix)
	settings.GroupIDs = cleanAccountIDs(settings.GroupIDs)
	settings.Models = cleanStringList(settings.Models)
	if settings.ProxyID != nil && *settings.ProxyID <= 0 {
		settings.ProxyID = nil
	}
	if settings.Priority != nil {
		value := *settings.Priority
		settings.Priority = &value
	}
	if settings.Concurrency != nil {
		value := *settings.Concurrency
		if value < 0 {
			value = 0
		}
		settings.Concurrency = &value
	}
	return settings
}

func renderImportNamePrefix(prefix string, now time.Time) string {
	if prefix == "" {
		return ""
	}
	return strings.ReplaceAll(prefix, "{date}", now.Format("20060102"))
}

func safeImportExecutionSettings(settings importAccountSettings) map[string]any {
	result := map[string]any{}
	if settings.NamePrefix != "" {
		result["namePrefix"] = settings.NamePrefix
	}
	if len(settings.GroupIDs) > 0 {
		result["groupIds"] = settings.GroupIDs
	}
	if settings.ProxyID != nil {
		result["proxyId"] = *settings.ProxyID
	}
	if len(settings.Models) > 0 {
		result["models"] = settings.Models
	}
	if settings.Priority != nil {
		result["priority"] = *settings.Priority
	}
	if settings.Concurrency != nil {
		result["concurrency"] = *settings.Concurrency
	}
	return result
}

func (m *jobManager) validateImportReferences(ctx context.Context, siteID int64, settings importAccountSettings) error {
	if len(settings.GroupIDs) > 0 {
		groups, err := m.fetchUpstreamList(ctx, siteID, "/api/v1/admin/groups/all")
		if err != nil {
			return err
		}
		ids := map[int64]bool{}
		for _, group := range groups {
			if id := int64FromAny(group["id"]); id > 0 {
				ids[id] = true
			}
		}
		for _, id := range settings.GroupIDs {
			if !ids[id] {
				return fmt.Errorf("group %d not found", id)
			}
		}
	}
	if settings.ProxyID != nil {
		proxies, err := m.fetchUpstreamList(ctx, siteID, "/api/v1/admin/proxies/all")
		if err != nil {
			return err
		}
		found := false
		for _, proxy := range proxies {
			if int64FromAny(proxy["id"]) == *settings.ProxyID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("proxy %d not found", *settings.ProxyID)
		}
	}
	return nil
}

func (m *jobManager) fetchUpstreamList(ctx context.Context, siteID int64, path string) ([]map[string]any, error) {
	data, statusCode, err := m.siteService.AdminGET(ctx, siteID, path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("upstream %s returned HTTP %d", path, statusCode)
	}
	value := decodeJSONValue(sanitizeJSONForBrowser(data))
	return listMapsFromAny(value), nil
}

func listMapsFromAny(value any) []map[string]any {
	if items, ok := value.([]any); ok {
		return mapsFromItems(items)
	}
	root, _ := value.(map[string]any)
	if root == nil {
		return nil
	}
	for _, key := range []string{"data", "items", "groups", "proxies"} {
		child := root[key]
		if items, ok := child.([]any); ok {
			return mapsFromItems(items)
		}
		if childMap, ok := child.(map[string]any); ok {
			if items, ok := childMap["items"].([]any); ok {
				return mapsFromItems(items)
			}
			if nested := listMapsFromAny(childMap); len(nested) > 0 {
				return nested
			}
		}
	}
	return nil
}

func mapsFromItems(items []any) []map[string]any {
	result := []map[string]any{}
	for _, item := range items {
		if typed, ok := item.(map[string]any); ok {
			result = append(result, typed)
		}
	}
	return result
}

func countImportItems(items []importPreviewItem, match func(importPreviewItem) bool) int {
	count := 0
	for _, item := range items {
		if match(item) {
			count++
		}
	}
	return count
}

func compactStrings(items []string) []string {
	result := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func writeBatchAccountTest(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64, logDir string) {
	var input batchAccountTestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input.IDs = cleanAccountIDs(input.IDs)
	if len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	results := make([]map[string]any, 0, len(input.IDs))
	for _, accountID := range input.IDs {
		result := runAccountTest(r.Context(), siteService, logDir, siteID, accountID, input)
		applyAccountMeta(result, input.AccountMeta)
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func runAccountTest(ctx context.Context, siteService *sites.Service, logDir string, siteID, accountID int64, input batchAccountTestInput) map[string]any {
	started := time.Now()
	data, statusCode, err := siteService.AdminPOSTJSON(ctx, siteID, fmt.Sprintf("/api/v1/admin/accounts/%d/test", accountID), map[string]any{
		"model_id": strings.TrimSpace(input.ModelID),
		"prompt":   strings.TrimSpace(input.Prompt),
		"mode":     strings.TrimSpace(input.Mode),
	})
	result := map[string]any{
		"id":         accountID,
		"statusCode": statusCode,
		"durationMs": time.Since(started).Milliseconds(),
	}
	if err != nil {
		result["ok"] = false
		result["error"] = err.Error()
		if hint, resetAt := parseKnownTestHint(err.Error()); hint != "" {
			result["hint"] = hint
			if resetAt != "" {
				result["resetAt"] = resetAt
			}
		}
		return result
	}
	sanitizedBody := string(sanitizeJSONForBrowser(data))
	ok, message, model, hint, resetAt := summarizeAccountTestSSE(sanitizedBody)
	result["ok"] = ok
	result["message"] = message
	if hint != "" {
		result["hint"] = hint
	} else if ok {
		result["hint"] = "正常"
	}
	if resetAt != "" {
		result["resetAt"] = resetAt
	}
	if model != "" {
		result["model"] = model
	}
	result["body"] = sanitizedBody
	if input.LogResponses {
		if path, err := writeBatchTestLog(logDir, siteID, accountID, sanitizedBody); err == nil {
			result["logPath"] = path
		} else {
			result["logError"] = err.Error()
		}
	}
	return result
}

func writeBatchAccountRefresh(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	ids := make([]int64, 0, len(input.IDs))
	seen := make(map[int64]bool, len(input.IDs))
	for _, id := range input.IDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	data, statusCode, err := siteService.AdminPOSTJSON(r.Context(), siteID, "/api/v1/admin/accounts/batch-refresh", map[string]any{
		"account_ids": ids,
	})
	if err != nil {
		writeSiteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(sanitizeJSONForBrowser(data))
}

func writeAccountsSearchByNames(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	var input struct {
		Names        []string `json:"names"`
		SkipComments bool     `json:"skipComments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keywords := make([]string, 0, len(input.Names))
	seen := make(map[string]bool, len(input.Names))
	for _, raw := range input.Names {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if input.SkipComments && strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		keywords = append(keywords, line)
	}
	if len(keywords) == 0 {
		writeError(w, http.StatusBadRequest, "names is required")
		return
	}
	if len(keywords) > 50 {
		writeError(w, http.StatusBadRequest, "too many keywords (max 50)")
		return
	}
	type keywordResult struct {
		Keyword    string           `json:"keyword"`
		Accounts   []map[string]any `json:"accounts"`
		Total      int64            `json:"total"`
		Returned   int              `json:"returned"`
		Truncated  bool             `json:"truncated"`
		Error      string           `json:"error,omitempty"`
		StatusCode int              `json:"statusCode,omitempty"`
	}
	items := make([]keywordResult, 0, len(keywords))
	for _, keyword := range keywords {
		query := url.Values{}
		query.Set("search", keyword)
		query.Set("page_size", "100")
		data, statusCode, err := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/accounts", query)
		if err != nil {
			slog.WarnContext(r.Context(), "search-by-names keyword failed", "site_id", siteID, "keyword", keyword, "error", err)
			items = append(items, keywordResult{Keyword: keyword, Accounts: []map[string]any{}, Error: err.Error()})
			continue
		}
		if statusCode < 200 || statusCode >= 300 {
			slog.WarnContext(r.Context(), "search-by-names keyword upstream error", "site_id", siteID, "keyword", keyword, "status_code", statusCode)
			items = append(items, keywordResult{Keyword: keyword, Accounts: []map[string]any{}, Error: upstreamErrorMessage(data), StatusCode: statusCode})
			continue
		}
		sanitized := sanitizeJSONForBrowser(data)
		parsed := decodeJSONValue(sanitized)
		accounts := extractAccountList(parsed)
		total := int64(len(accounts))
		if parsedTotal, ok := paginatedTotal(parsed); ok {
			total = parsedTotal
		}
		returned := len(accounts)
		items = append(items, keywordResult{Keyword: keyword, Accounts: accounts, Total: total, Returned: returned, Truncated: total > int64(returned)})
	}
	slog.InfoContext(r.Context(), "search-by-names completed", "site_id", siteID, "keyword_count", len(keywords))
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func upstreamErrorMessage(data []byte) string {
	value := decodeJSONValue(sanitizeJSONForBrowser(data))
	root, ok := value.(map[string]any)
	if !ok {
		return "upstream request failed"
	}
	if message, ok := root["error"].(string); ok && strings.TrimSpace(message) != "" {
		return message
	}
	if message, ok := root["message"].(string); ok && strings.TrimSpace(message) != "" {
		return message
	}
	if data, ok := root["data"].(map[string]any); ok {
		if message, ok := data["error"].(string); ok && strings.TrimSpace(message) != "" {
			return message
		}
		if message, ok := data["message"].(string); ok && strings.TrimSpace(message) != "" {
			return message
		}
	}
	return "upstream request failed"
}

func extractAccountList(value any) []map[string]any {
	if value == nil {
		return nil
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		return nil
	}
	items, ok := data["items"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

type errorAccountItem struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	Platform     string   `json:"platform"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	ErrorMessage string   `json:"error_message"`
	UpdatedAt    string   `json:"updated_at"`
	Usage5h      *float64 `json:"usage_5h,omitempty"`
	Usage7d      *float64 `json:"usage_7d,omitempty"`
}

type groupQuotaAccount struct {
	ID                   int64   `json:"id"`
	Name                 string  `json:"name"`
	UsedPercent          float64 `json:"usedPercent"`
	WindowMinutes        int     `json:"windowMinutes"`
	ResetAfterSeconds    int64   `json:"resetAfterSeconds"`
	CycleCost            float64 `json:"cycleCost"`
	EstimatedTotalQuota  float64 `json:"estimatedTotalQuota"`
	RemainingUSD         float64 `json:"remainingUsd"`
}

type groupQuotaBucket struct {
	Range        string  `json:"range"`
	MinPercent   int     `json:"minPercent"`
	MaxPercent   int     `json:"maxPercent"`
	Count        int     `json:"count"`
	RemainingUSD float64 `json:"remainingUsd"`
}

type groupQuotaResponse struct {
	Group               string             `json:"group"`
	GroupID             int64              `json:"groupId"`
	TotalAccounts       int                `json:"totalAccounts"`
	TotalQuotaUSD       float64            `json:"totalQuotaUsd"`
	UsedQuotaUSD        float64            `json:"usedQuotaUsd"`
	RemainingUSD        float64            `json:"remainingUsd"`
	DefaultQuotaPerAcct float64            `json:"defaultQuotaPerAccount"`
	AccurateEstimate    bool               `json:"accurateEstimate"`
	Cached              bool               `json:"cached"`
	CachedAt            int64              `json:"cachedAt,omitempty"`
	Buckets             []groupQuotaBucket `json:"buckets"`
	Accounts            []groupQuotaAccount `json:"accounts"`
}

type groupQuotaCacheEntry struct {
	response  groupQuotaResponse
	cachedAt  time.Time
}

var (
	groupQuotaCache    = make(map[string]groupQuotaCacheEntry)
	groupQuotaCacheMu  sync.Mutex
	groupQuotaCacheTTL = 24 * time.Hour
)

func groupQuotaCacheKey(siteID int64, groupName string) string {
	return strconv.FormatInt(siteID, 10) + ":" + groupName
}

func writeGroupQuota(w http.ResponseWriter, r *http.Request, authMgr *auth.Manager, svc *sites.Service, siteID int64) {
	if !requireAuth(w, r, authMgr) {
		return
	}
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	groupName := r.URL.Query().Get("group")
	if groupName == "" {
		groupName = "free"
	}

	refresh := r.URL.Query().Get("refresh") == "true"
	cacheKey := groupQuotaCacheKey(siteID, groupName)
	if !refresh {
		groupQuotaCacheMu.Lock()
		entry, hit := groupQuotaCache[cacheKey]
		groupQuotaCacheMu.Unlock()
		if hit && time.Since(entry.cachedAt) < groupQuotaCacheTTL {
			entry.response.Cached = true
			entry.response.CachedAt = entry.cachedAt.Unix()
			writeJSON(w, http.StatusOK, entry.response)
			return
		}
	}
	defaultQuota := 2.0
	if v := r.URL.Query().Get("defaultQuota"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			defaultQuota = n
		}
	}

	groupsData, _, err := svc.AdminGET(r.Context(), siteID, "/api/v1/admin/groups/all", nil)
	if err != nil {
		writeSiteError(w, err)
		return
	}
	var groupsResp struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(groupsData, &groupsResp); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse groups")
		return
	}
	var groupID int64
	for _, g := range groupsResp.Data {
		if g.Name == groupName {
			groupID = g.ID
			break
		}
	}
	if groupID == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("group %q not found", groupName))
		return
	}

	var allItems []map[string]any
	page := 1
	pageSize := 200
	for {
		query := url.Values{"group": {strconv.FormatInt(groupID, 10)}, "page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(pageSize)}}
		accountsData, _, err := svc.AdminGET(r.Context(), siteID, "/api/v1/admin/accounts", query)
		if err != nil {
			writeSiteError(w, err)
			return
		}
		var accountsResp struct {
			Data struct {
				Items []map[string]any `json:"items"`
				Total int64            `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(accountsData, &accountsResp); err != nil {
			writeError(w, http.StatusBadRequest, "failed to parse accounts")
			return
		}
		allItems = append(allItems, accountsResp.Data.Items...)
		if len(accountsResp.Data.Items) < pageSize {
			break
		}
		if accountsResp.Data.Total > 0 && int64(len(allItems)) >= accountsResp.Data.Total {
			break
		}
		page++
		if page > 20 {
			break
		}
	}

	accounts := make([]groupQuotaAccount, 0, len(allItems))
	for _, acc := range allItems {
		id := int64FromAny(acc["id"])
		usedPct := 0.0
		windowMin := 0
		resetAfterSec := int64(0)
		if extra, ok := acc["extra"].(map[string]any); ok {
			usedPct = extractUsagePercentFloat(extra, "codex_primary_used_percent", "codex_7d_used_percent")
			if v := extractIntValue(extra, "codex_primary_window_minutes"); v > 0 {
				windowMin = int(v)
			}
			resetAfterSec = extractIntValue(extra, "codex_primary_reset_after_seconds")
		}
		name := fmt.Sprint(acc["name"])
		accounts = append(accounts, groupQuotaAccount{
			ID:                id,
			Name:              name,
			UsedPercent:       usedPct,
			WindowMinutes:     windowMin,
			ResetAfterSeconds: resetAfterSec,
		})
	}

	var earliestCycleStart time.Time
	for _, acc := range accounts {
		if acc.WindowMinutes <= 0 || acc.ResetAfterSeconds <= 0 {
			continue
		}
		cycleStart := time.Now().Add(time.Duration(acc.ResetAfterSeconds)*time.Second - time.Duration(acc.WindowMinutes)*time.Minute)
		if earliestCycleStart.IsZero() || cycleStart.Before(earliestCycleStart) {
			earliestCycleStart = cycleStart
		}
	}

	cycleCosts := make(map[int64]float64)
	accurateEstimate := false
	if !earliestCycleStart.IsZero() {
		startDate := earliestCycleStart.Format("2006-01-02")
		endDate := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		accountIDs := make([]int64, len(accounts))
		for i, acc := range accounts {
			accountIDs[i] = acc.ID
		}
		costs, err := queryUsageCostsViaAPI(r.Context(), svc, siteID, accountIDs, startDate, endDate)
		if err != nil {
			slog.WarnContext(r.Context(), "query usage costs via API failed, using default quota", "error", err)
		} else {
			cycleCosts = costs
			accurateEstimate = true
		}
	}

	for i := range accounts {
		cost, hasCost := cycleCosts[accounts[i].ID]
		if hasCost && accounts[i].UsedPercent > 5 && cost > 0 {
			estimatedTotal := cost / (accounts[i].UsedPercent / 100.0)
			accounts[i].CycleCost = math.Round(cost*100) / 100
			accounts[i].EstimatedTotalQuota = math.Round(estimatedTotal*100) / 100
			accounts[i].RemainingUSD = math.Round(estimatedTotal*(1-accounts[i].UsedPercent/100.0)*100) / 100
		} else {
			accounts[i].EstimatedTotalQuota = defaultQuota
			accounts[i].RemainingUSD = math.Round(defaultQuota*(1-accounts[i].UsedPercent/100.0)*100) / 100
			if hasCost {
				accounts[i].CycleCost = math.Round(cost*100) / 100
			}
		}
	}

	bucketDefs := []struct {
		label      string
		minPercent int
		maxPercent int
	}{{"0-10%", 0, 10}, {"11-30%", 11, 30}, {"31-50%", 31, 50}, {"51-70%", 51, 70}, {"71-90%", 71, 90}, {"91-100%", 91, 100}}

	buckets := make([]groupQuotaBucket, len(bucketDefs))
	for i, def := range bucketDefs {
		buckets[i] = groupQuotaBucket{Range: def.label, MinPercent: def.minPercent, MaxPercent: def.maxPercent}
	}

	totalUsed := 0.0
	totalQuota := 0.0
	for _, acc := range accounts {
		totalUsed += acc.EstimatedTotalQuota * acc.UsedPercent / 100.0
		totalQuota += acc.EstimatedTotalQuota
		for i, def := range bucketDefs {
			if int(math.Round(acc.UsedPercent)) >= def.minPercent && int(math.Round(acc.UsedPercent)) <= def.maxPercent {
				buckets[i].Count++
				buckets[i].RemainingUSD += acc.RemainingUSD
				break
			}
		}
	}

	totalAccounts := len(accounts)
	remaining := totalQuota - totalUsed

	for i := range buckets {
		buckets[i].RemainingUSD = math.Round(buckets[i].RemainingUSD*100) / 100
	}

	slog.InfoContext(r.Context(), "group quota computed", "site_id", siteID, "group", groupName, "total_accounts", totalAccounts, "remaining_usd", remaining, "accurate", accurateEstimate, "cached", false)
	resp := groupQuotaResponse{
		Group:               groupName,
		GroupID:             groupID,
		TotalAccounts:       totalAccounts,
		TotalQuotaUSD:       math.Round(totalQuota*100) / 100,
		UsedQuotaUSD:        math.Round(totalUsed*100) / 100,
		RemainingUSD:        math.Round(remaining*100) / 100,
		DefaultQuotaPerAcct: defaultQuota,
		AccurateEstimate:    accurateEstimate,
		Buckets:             buckets,
		Accounts:            accounts,
	}

	groupQuotaCacheMu.Lock()
	groupQuotaCache[cacheKey] = groupQuotaCacheEntry{response: resp, cachedAt: time.Now()}
	groupQuotaCacheMu.Unlock()

	writeJSON(w, http.StatusOK, resp)
}

func queryUsageCostsViaAPI(ctx context.Context, svc *sites.Service, siteID int64, accountIDs []int64, startDate, endDate string) (map[int64]float64, error) {
	result := make(map[int64]float64)
	if len(accountIDs) == 0 {
		return result, nil
	}
	idSet := make(map[int64]bool, len(accountIDs))
	for _, id := range accountIDs {
		idSet[id] = true
	}
	page := 1
	pageSize := 200
	for {
		query := url.Values{
			"start_date": {startDate},
			"end_date":   {endDate},
			"page":       {strconv.Itoa(page)},
			"page_size":  {strconv.Itoa(pageSize)},
			"sort_by":    {"created_at"},
			"sort_order": {"asc"},
		}
		data, statusCode, err := svc.AdminGET(ctx, siteID, "/api/v1/admin/usage", query)
		if err != nil {
			return nil, err
		}
		if statusCode != http.StatusOK {
			return nil, fmt.Errorf("usage API returned status %d", statusCode)
		}
		var usageResp struct {
			Data struct {
				Items []struct {
					AccountID int64   `json:"account_id"`
					TotalCost float64 `json:"total_cost"`
				} `json:"items"`
				Total int64 `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &usageResp); err != nil {
			return nil, fmt.Errorf("failed to parse usage response: %w", err)
		}
		for _, item := range usageResp.Data.Items {
			if idSet[item.AccountID] {
				result[item.AccountID] += item.TotalCost
			}
		}
		if len(usageResp.Data.Items) < pageSize {
			break
		}
		if usageResp.Data.Total > 0 && int64(page*pageSize) >= usageResp.Data.Total {
			break
		}
		page++
		if page > 100 {
			break
		}
	}
	return result, nil
}

func extractIntValue(extra map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v := extra[key]; v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return int64(f)
			}
		}
	}
	return 0
}

func extractUsagePercentFloat(extra map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if v := extra[key]; v != nil {
			s := strings.TrimSpace(strings.TrimSuffix(fmt.Sprint(v), "%"))
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return math.Max(0, math.Min(100, n))
			}
		}
	}
	return 0
}

func writeErrorAccounts(w http.ResponseWriter, r *http.Request, authMgr *auth.Manager, svc *sites.Service, siteID int64) {
	if !requireAuth(w, r, authMgr) {
		return
	}
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "SUBADMIN_SECRET_KEY is required")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	groupName := r.URL.Query().Get("group")
	if groupName == "" {
		groupName = "free"
	}

	// Resolve group name to ID
	groupsData, _, err := svc.AdminGET(r.Context(), siteID, "/api/v1/admin/groups/all", nil)
	if err != nil {
		writeSiteError(w, err)
		return
	}
	var groupsResp struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(groupsData, &groupsResp); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse groups")
		return
	}
	var groupID int64
	for _, g := range groupsResp.Data {
		if g.Name == groupName {
			groupID = g.ID
			break
		}
	}
	if groupID == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("group %q not found", groupName))
		return
	}

	// Fetch error accounts
	query := url.Values{"status": {"error"}, "group": {strconv.FormatInt(groupID, 10)}, "page_size": {"100"}}
	accountsData, _, err := svc.AdminGET(r.Context(), siteID, "/api/v1/admin/accounts", query)
	if err != nil {
		writeSiteError(w, err)
		return
	}
	var accountsResp struct {
		Data struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(accountsData, &accountsResp); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse accounts")
		return
	}

	items := make([]errorAccountItem, 0, len(accountsResp.Data.Items))
	for _, acc := range accountsResp.Data.Items {
		item := errorAccountItem{
			ID:           int64FromAny(acc["id"]),
			Name:         fmt.Sprint(acc["name"]),
			Platform:     fmt.Sprint(acc["platform"]),
			Type:         fmt.Sprint(acc["type"]),
			Status:       fmt.Sprint(acc["status"]),
			ErrorMessage: fmt.Sprint(acc["error_message"]),
			UpdatedAt:    fmt.Sprint(acc["updated_at"]),
		}
		if creds, ok := acc["credentials"].(map[string]any); ok {
			if email, ok := creds["email"].(string); ok {
				item.Email = email
			}
		}
		if extra, ok := acc["extra"].(map[string]any); ok {
			item.Usage5h = extractUsagePercent(extra, "codex_primary_used_percent", "codex_5h_used_percent")
			item.Usage7d = extractUsagePercent(extra, "codex_secondary_used_percent", "codex_7d_used_percent")
		}
		items = append(items, item)
	}
	slog.InfoContext(r.Context(), "error accounts listed", "site_id", siteID, "group", groupName, "total", accountsResp.Data.Total, "returned", len(items))
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": accountsResp.Data.Total, "group": groupName})
}

func extractUsagePercent(extra map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		if v := extra[key]; v != nil {
			s := strings.TrimSpace(strings.TrimSuffix(fmt.Sprint(v), "%"))
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return ptrFloat64(math.Max(0, math.Min(100, n)))
			}
		}
	}
	return nil
}

func ptrFloat64(v float64) *float64 { return &v }

func writeSiteStatistics(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	now := time.Now()
	requestQuery := r.URL.Query()
	query := url.Values{}
	startDate := strings.TrimSpace(requestQuery.Get("start_date"))
	endDate := strings.TrimSpace(requestQuery.Get("end_date"))
	granularity := strings.TrimSpace(requestQuery.Get("granularity"))
	if startDate == "" {
		startDate = now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	if endDate == "" {
		endDate = now.Format("2006-01-02")
	}
	if granularity == "" {
		granularity = "hour"
	} else if granularity != "hour" {
		granularity = "day"
	}
	query.Set("start_date", startDate)
	query.Set("end_date", endDate)
	query.Set("granularity", granularity)
	query.Set("include_stats", "true")
	query.Set("include_trend", "true")
	query.Set("include_model_stats", "true")
	query.Set("include_group_stats", "false")
	query.Set("include_users_trend", "true")
	query.Set("users_trend_limit", "12")

	snapshot, snapshotStatus, snapshotErr := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/dashboard/snapshot-v2", query)
	stats, statsStatus, statsErr := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/dashboard/stats", nil)
	rankingQuery := url.Values{}
	rankingQuery.Set("start_date", startDate)
	rankingQuery.Set("end_date", endDate)
	rankingQuery.Set("limit", "12")
	ranking, rankingStatus, rankingErr := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/dashboard/users-ranking", rankingQuery)
	userConcurrency, userConcurrencyStatus, userConcurrencyErr := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/ops/user-concurrency", nil)
	opsConcurrency, opsConcurrencyStatus, opsConcurrencyErr := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/ops/concurrency", nil)
	activeAccountQuery := url.Values{}
	activeAccountQuery.Set("status", "active")
	activeAccountQuery.Set("page", "1")
	activeAccountQuery.Set("page_size", "1")
	activeAccountQuery.Set("lite", "true")
	activeAccounts, activeAccountsStatus, activeAccountsErr := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/accounts", activeAccountQuery)
	if snapshotErr != nil && statsErr != nil && rankingErr != nil && userConcurrencyErr != nil && opsConcurrencyErr != nil && activeAccountsErr != nil {
		writeSiteError(w, snapshotErr)
		return
	}
	result := map[string]any{
		"snapshot":              decodeJSONValue(snapshot),
		"snapshotStatus":        snapshotStatus,
		"stats":                 decodeJSONValue(stats),
		"statsStatus":           statsStatus,
		"ranking":               decodeJSONValue(ranking),
		"rankingStatus":         rankingStatus,
		"userConcurrency":       decodeJSONValue(userConcurrency),
		"userConcurrencyStatus": userConcurrencyStatus,
		"opsConcurrency":        decodeJSONValue(opsConcurrency),
		"opsConcurrencyStatus":  opsConcurrencyStatus,
		"range": map[string]string{
			"start_date":  startDate,
			"end_date":    endDate,
			"granularity": granularity,
		},
		"notes": []string{
			"官方管理仪表盘主页面使用 dashboard/snapshot-v2 聚合 stats、trend、models 和 users_trend。",
			"snapshot-v2.stats 包含 active_users、hourly_active_users、rpm、tpm、today_account_cost、total_account_cost 等字段。",
			"用户排行来自 dashboard/users-ranking；趋势、模型分布和用户趋势来自 snapshot-v2。",
			"当前用户并发来自 Ops user-concurrency，依赖 sub2api Ops 实时监控开关。",
			"账号并发来自 ops/concurrency，依赖 sub2api Ops 实时监控开关。",
		},
	}
	if total, ok := paginatedTotal(decodeJSONValue(activeAccounts)); ok {
		result["activeStatusAccounts"] = total
	}
	if snapshotErr != nil {
		result["snapshotError"] = snapshotErr.Error()
	}
	if statsErr != nil {
		result["statsError"] = statsErr.Error()
	}
	if rankingErr != nil {
		result["rankingError"] = rankingErr.Error()
	}
	if userConcurrencyErr != nil {
		result["userConcurrencyError"] = userConcurrencyErr.Error()
	}
	if opsConcurrencyErr != nil {
		result["opsConcurrencyError"] = opsConcurrencyErr.Error()
	}
	if activeAccountsErr != nil {
		result["activeStatusAccountsError"] = activeAccountsErr.Error()
	}
	slog.InfoContext(r.Context(), "statistics loaded", "site_id", siteID, "start_date", startDate, "end_date", endDate, "granularity", granularity, "snapshot_status", snapshotStatus, "stats_status", statsStatus, "ranking_status", rankingStatus, "user_concurrency_status", userConcurrencyStatus, "account_concurrency_status", opsConcurrencyStatus, "active_accounts_status", activeAccountsStatus)
	writeJSON(w, http.StatusOK, result)
}

func paginatedTotal(value any) (int64, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return 0, false
	}
	if total, ok := numberAsInt64(object["total"]); ok {
		return total, true
	}
	if data, ok := object["data"]; ok {
		return paginatedTotal(data)
	}
	return 0, false
}

func numberAsInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case json.Number:
		parsed, err := v.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func writeSiteUserConcurrency(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	data, statusCode, err := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/ops/user-concurrency", nil)
	if err != nil {
		writeSiteError(w, err)
		return
	}
	slog.InfoContext(r.Context(), "user concurrency refreshed", "site_id", siteID, "status_code", statusCode, "response_bytes", len(data))
	writeJSON(w, http.StatusOK, map[string]any{
		"userConcurrency":       decodeJSONValue(data),
		"userConcurrencyStatus": statusCode,
	})
}

func writeSiteAccountConcurrency(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	data, statusCode, err := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/ops/concurrency", nil)
	if err != nil {
		writeSiteError(w, err)
		return
	}
	slog.InfoContext(r.Context(), "account concurrency refreshed", "site_id", siteID, "status_code", statusCode, "response_bytes", len(data))
	writeJSON(w, http.StatusOK, map[string]any{
		"opsConcurrency":       decodeJSONValue(data),
		"opsConcurrencyStatus": statusCode,
	})
}

func decodeJSONValue(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return string(data)
	}
	return value
}

func summarizeAccountTestSSE(body string) (bool, string, string, string, string) {
	ok := false
	message := "未检测到完成事件"
	model := ""
	hint := ""
	resetAt := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Model   string `json:"model"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			continue
		}
		if event.Model != "" {
			model = event.Model
		}
		if event.Text != "" {
			if parsedHint, parsedReset := parseKnownTestHint(event.Text); parsedHint != "" {
				hint = parsedHint
				resetAt = parsedReset
			}
		}
		if event.Error != "" {
			if parsedHint, parsedReset := parseKnownTestHint(event.Error); parsedHint != "" {
				hint = parsedHint
				resetAt = parsedReset
			}
		}
		switch event.Type {
		case "error":
			if hint != "" {
				return false, event.Error, model, hint, resetAt
			}
			return false, event.Error, model, "", ""
		case "content":
			if event.Text != "" {
				message = event.Text
			}
		case "test_complete":
			ok = event.Success
			if ok {
				return true, "测试成功", model, hint, resetAt
			}
			return false, "测试未成功完成", model, hint, resetAt
		}
	}
	if hint == "" {
		hint, resetAt = parseKnownTestHint(body)
	}
	return ok, message, model, hint, resetAt
}

func parseKnownTestHint(text string) (string, string) {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "token_revoked") {
		return "令牌撤销", ""
	}
	if strings.Contains(lower, "token_invalidated") || strings.Contains(lower, "invalidated oauth token") {
		return "令牌失效", ""
	}
	if strings.Contains(lower, "deactivated_workspace") {
		return "工作区失效", ""
	}
	if strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "connection refused") {
		return "网络或超时异常", ""
	}
	if !strings.Contains(lower, "429") && !strings.Contains(lower, "rate_limit") && !strings.Contains(lower, "rate limit") && !strings.Contains(lower, "usage_limit_reached") {
		if strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "authentication") || strings.Contains(lower, "invalid api key") {
			return "上游认证失败", ""
		}
		if strings.Contains(lower, "cloudflare") || strings.Contains(lower, "cf blocked") || strings.Contains(lower, "cf_blocked") {
			return "上游服务异常: Cloudflare 阻断", ""
		}
		if strings.Contains(lower, "529") || strings.Contains(lower, "502") || strings.Contains(lower, "503") || strings.Contains(lower, "504") || strings.Contains(lower, "5xx") || strings.Contains(lower, "server error") || strings.Contains(lower, "bad gateway") || strings.Contains(lower, "service unavailable") {
			return "上游服务异常", ""
		}
		return "", ""
	}
	parts := []string{"账号限流或额度耗尽"}
	if planType := firstKnownTimeValue(text, "plan_type"); planType != "" {
		parts = append(parts, "套餐: "+planType)
	}
	resetAt := firstKnownTimeValue(text, "resets_at", "reset_at", "rate_limit_reset_at")
	formattedReset := formatKnownTimeValue(resetAt)
	if resetAt != "" {
		parts = append(parts, "恢复时间: "+formattedReset)
	}
	retryAfter := firstKnownTimeValue(text, "retry_after", "resets_in_seconds", "reset_after_seconds")
	if retryAfter != "" {
		parts = append(parts, "建议等待: "+retryAfter+" 秒")
	}
	return strings.Join(parts, "，"), formattedReset
}

func formatKnownTimeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if unixSeconds, err := strconv.ParseInt(value, 10, 64); err == nil && unixSeconds > 946684800 {
		return time.Unix(unixSeconds, 0).UTC().Format(time.RFC3339)
	}
	return value
}

func firstKnownTimeValue(text string, keys ...string) string {
	for _, key := range keys {
		marker := `"` + key + `"`
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		rest := text[idx+len(marker):]
		colon := strings.Index(rest, ":")
		if colon < 0 {
			continue
		}
		value := strings.TrimSpace(rest[colon+1:])
		value = strings.TrimLeft(value, `"`)
		end := strings.IndexAny(value, `",}\n `)
		if end >= 0 {
			value = value[:end]
		}
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func writeBatchTestLog(baseDir string, siteID, accountID int64, body string) (string, error) {
	dir := strings.TrimSpace(baseDir)
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	path := filepath.Join(dir, fmt.Sprintf("%s_site-%d_account-%d.log", stamp, siteID, accountID))
	cleanBody := redactSensitiveText(body)
	if err := os.WriteFile(path, []byte(cleanBody), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func redactSensitiveText(text string) string {
	result := redactSensitiveAssignments(text)
	result = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`).ReplaceAllString(result, "$1[redacted]")
	result = regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]+`).ReplaceAllString(result, "[redacted]")
	return result
}

func redactSensitiveAssignments(text string) string {
	keyPattern := `(?i)("?(?:access_token|refresh_token|id_token|token|api_key|key|secret|password|cookie|authorization|credentials)"?\s*[:=]\s*)`
	result := regexp.MustCompile(keyPattern+`"[^"\r\n]*"`).ReplaceAllString(text, `${1}"[redacted]"`)
	result = regexp.MustCompile(keyPattern+`[^"\s,}\{\[]+`).ReplaceAllString(result, `${1}[redacted]`)
	return result
}

func shellPage() string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>SubAdmin</title></head><body><main style="font-family: sans-serif; padding: 24px;"><h1>SubAdmin</h1><p>前端资源尚未构建。请先在 web 目录执行构建命令。</p></main></body></html>`
}
