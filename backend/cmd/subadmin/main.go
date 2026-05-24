package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"subadmin/internal/auth"
	"subadmin/internal/config"
	"subadmin/internal/db"
	"subadmin/internal/secretbox"
	"subadmin/internal/sites"
)

func main() {
	cfg := config.Load()
	dbPath := cfg.DBPath
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}

	store, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer store.Close()
	authManager := auth.NewManager(store.DB(), cfg)
	var siteService *sites.Service
	var jobService *jobManager
	if cfg.SecretKey != "" {
		box, err := secretbox.New(cfg.SecretKey)
		if err != nil {
			log.Fatalf("init secret box: %v", err)
		}
		siteService = sites.NewService(store.DB(), box)
		jobService = newJobManager(store.DB(), siteService, cfg.LogDir)
		if err := jobService.markInterrupted(context.Background()); err != nil {
			log.Printf("mark interrupted jobs: %v", err)
		}
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
				writeError(w, http.StatusUnauthorized, "invalid login secret")
				return
			}
			writeError(w, http.StatusInternalServerError, "login failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := authManager.Logout(w, r); err != nil {
			writeError(w, http.StatusInternalServerError, "logout failed")
			return
		}
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			data = sanitizeJSONForBrowser(data)
			_, _ = w.Write(data)
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
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			if err := siteService.Delete(r.Context(), id); err != nil {
				writeSiteError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	addr := cfg.Addr
	log.Printf("subAdmin listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
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

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
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

func (m *jobManager) runBatchAccountTest(ctx context.Context, jobID, siteID int64, input batchAccountTestInput) {
	now := time.Now().Unix()
	_, _ = m.db.ExecContext(context.Background(), `UPDATE jobs SET status = 'running', started_at = ? WHERE id = ?`, now, jobID)
	items := make([]map[string]any, 0, len(input.IDs))
	successCount := 0
	failedCount := 0
	for index, accountID := range input.IDs {
		if ctx.Err() != nil {
			_ = m.finishJob(jobID, "cancelled", items, successCount, failedCount, ctx.Err().Error())
			return
		}
		if index > 0 && !waitForJobDelay(ctx, len(input.IDs), input.DelayMs, input.JitterMs) {
			_ = m.finishJob(jobID, "cancelled", items, successCount, failedCount, context.Canceled.Error())
			return
		}
		result := runAccountTest(ctx, m.siteService, m.logDir, siteID, accountID, input)
		applyAccountMeta(result, input.AccountMeta)
		if ctx.Err() != nil {
			items = append(items, result)
			failedCount++
			_ = m.updateJobProgress(jobID, items, successCount, failedCount)
			_ = m.finishJob(jobID, "cancelled", items, successCount, failedCount, ctx.Err().Error())
			return
		}
		if result["ok"] == true {
			successCount++
		} else {
			failedCount++
		}
		items = append(items, result)
		_ = m.updateJobProgress(jobID, items, successCount, failedCount)
	}
	status := "succeeded"
	if failedCount > 0 {
		status = "failed"
	}
	_ = m.finishJob(jobID, status, items, successCount, failedCount, "")
}

func (m *jobManager) runBatchTokenRefresh(ctx context.Context, jobID, siteID int64, input batchTokenRefreshInput) {
	now := time.Now().Unix()
	_, _ = m.db.ExecContext(context.Background(), `UPDATE jobs SET status = 'running', started_at = ? WHERE id = ?`, now, jobID)
	if ctx.Err() != nil {
		_ = m.finishJob(jobID, "cancelled", nil, 0, 0, ctx.Err().Error())
		return
	}
	started := time.Now()
	data, statusCode, err := m.siteService.AdminPOSTJSON(ctx, siteID, "/api/v1/admin/accounts/batch-refresh", map[string]any{
		"account_ids": input.IDs,
	})
	durationMS := time.Since(started).Milliseconds()
	if ctx.Err() != nil {
		items := buildTokenRefreshItems(input.IDs, input.AccountMeta, statusCode, durationMS, data, ctx.Err())
		_ = m.updateJobProgress(jobID, items, 0, len(items))
		_ = m.finishJob(jobID, "cancelled", items, 0, len(items), ctx.Err().Error())
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
	if snapshotErr != nil && statsErr != nil && rankingErr != nil && userConcurrencyErr != nil && opsConcurrencyErr != nil {
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
	writeJSON(w, http.StatusOK, result)
}

func writeSiteUserConcurrency(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	data, statusCode, err := siteService.AdminGET(r.Context(), siteID, "/api/v1/admin/ops/user-concurrency", nil)
	if err != nil {
		writeSiteError(w, err)
		return
	}
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
	result := text
	result = regexp.MustCompile(`(?i)(access_token|refresh_token|id_token|token|api_key|key|secret|password|cookie|authorization)\s*[:=]\s*"?[^"\s,}]+"?`).ReplaceAllString(result, "$1:[redacted]")
	result = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`).ReplaceAllString(result, "$1[redacted]")
	result = regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]+`).ReplaceAllString(result, "[redacted]")
	return result
}

func shellPage() string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>SubAdmin</title></head><body><main style="font-family: sans-serif; padding: 24px;"><h1>SubAdmin</h1><p>前端资源尚未构建。请先在 web 目录执行构建命令。</p></main></body></html>`
}
