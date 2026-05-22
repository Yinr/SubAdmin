package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	if cfg.SecretKey != "" {
		box, err := secretbox.New(cfg.SecretKey)
		if err != nil {
			log.Fatalf("init secret box: %v", err)
		}
		siteService = sites.NewService(store.DB(), box)
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
				writeBatchAccountTest(w, r, siteService, id)
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
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeBatchAccountTest(w http.ResponseWriter, r *http.Request, siteService *sites.Service, siteID int64) {
	var input struct {
		IDs     []int64 `json:"ids"`
		ModelID string  `json:"modelId"`
		Prompt  string  `json:"prompt"`
		Mode    string  `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	if len(input.IDs) > 20 {
		writeError(w, http.StatusBadRequest, "at most 20 accounts can be tested at once")
		return
	}
	results := make([]map[string]any, 0, len(input.IDs))
	for _, accountID := range input.IDs {
		if accountID <= 0 {
			results = append(results, map[string]any{"id": accountID, "ok": false, "error": "invalid account id"})
			continue
		}
		started := time.Now()
		data, statusCode, err := siteService.AdminPOSTJSON(r.Context(), siteID, fmt.Sprintf("/api/v1/admin/accounts/%d/test", accountID), map[string]any{
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
		} else {
			ok, message, model := summarizeAccountTestSSE(string(sanitizeJSONForBrowser(data)))
			result["ok"] = ok
			result["message"] = message
			if model != "" {
				result["model"] = model
			}
			result["body"] = string(sanitizeJSONForBrowser(data))
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func summarizeAccountTestSSE(body string) (bool, string, string) {
	ok := false
	message := "未检测到完成事件"
	model := ""
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
		switch event.Type {
		case "error":
			return false, event.Error, model
		case "content":
			if event.Text != "" {
				message = event.Text
			}
		case "test_complete":
			ok = event.Success
			if ok {
				return true, "测试成功", model
			}
			return false, "测试未成功完成", model
		}
	}
	return ok, message, model
}

func shellPage() string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>SubAdmin</title></head><body><main style="font-family: sans-serif; padding: 24px;"><h1>SubAdmin</h1><p>前端资源尚未构建。请先在 web 目录执行构建命令。</p></main></body></html>`
}
