package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, shellPage())
	})
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
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

func shellPage() string {
	return strings.TrimSpace(`
<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>subAdmin</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #0b1020;
      --panel: rgba(17, 24, 39, 0.88);
      --border: rgba(148, 163, 184, 0.18);
      --text: #e5e7eb;
      --muted: #94a3b8;
      --accent: #7c3aed;
      --accent-2: #22c55e;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color: var(--text);
      background:
        radial-gradient(circle at top left, rgba(124, 58, 237, 0.22), transparent 28%),
        radial-gradient(circle at top right, rgba(34, 197, 94, 0.18), transparent 24%),
        var(--bg);
      display: grid;
      place-items: center;
      padding: 24px;
    }
    .shell {
      width: min(960px, 100%);
      display: grid;
      gap: 20px;
    }
    .hero {
      padding: 28px;
      border: 1px solid var(--border);
      border-radius: 24px;
      background: linear-gradient(180deg, rgba(17, 24, 39, 0.96), rgba(15, 23, 42, 0.9));
      box-shadow: 0 24px 80px rgba(0, 0, 0, 0.35);
    }
    .eyebrow {
      margin: 0 0 8px;
      color: #c4b5fd;
      letter-spacing: 0.16em;
      text-transform: uppercase;
      font-size: 12px;
    }
    h1 {
      margin: 0;
      font-size: clamp(32px, 5vw, 56px);
      line-height: 1.02;
    }
    p {
      margin: 16px 0 0;
      color: var(--muted);
      line-height: 1.7;
      max-width: 70ch;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 16px;
    }
    .card {
      padding: 18px;
      border: 1px solid var(--border);
      border-radius: 18px;
      background: var(--panel);
    }
    .card h2 {
      margin: 0 0 8px;
      font-size: 16px;
    }
    code {
      padding: 2px 6px;
      border-radius: 6px;
      background: rgba(148, 163, 184, 0.14);
      color: #f8fafc;
    }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      margin-top: 18px;
      padding: 10px 14px;
      border-radius: 999px;
      border: 1px solid rgba(34, 197, 94, 0.35);
      background: rgba(34, 197, 94, 0.08);
      color: #bbf7d0;
      font-size: 14px;
    }
    form {
      display: grid;
      gap: 12px;
      margin-top: 22px;
      max-width: 440px;
    }
    label {
      color: #cbd5e1;
      font-size: 14px;
    }
    input {
      width: 100%;
      padding: 13px 14px;
      border: 1px solid rgba(148, 163, 184, 0.28);
      border-radius: 12px;
      color: #f8fafc;
      background: rgba(15, 23, 42, 0.86);
      outline: none;
    }
    input:focus {
      border-color: rgba(196, 181, 253, 0.8);
      box-shadow: 0 0 0 3px rgba(124, 58, 237, 0.18);
    }
    button {
      border: 0;
      border-radius: 12px;
      padding: 13px 16px;
      color: #fff;
      background: linear-gradient(135deg, #7c3aed, #2563eb);
      cursor: pointer;
      font-weight: 700;
    }
    button.secondary {
      background: rgba(148, 163, 184, 0.14);
      color: #e2e8f0;
    }
    .hidden { display: none; }
    .error { color: #fecaca; }
    .dot {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: var(--accent-2);
      box-shadow: 0 0 18px rgba(34, 197, 94, 0.8);
    }
    a { color: #c4b5fd; text-decoration: none; }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <p class="eyebrow">subAdmin</p>
      <h1>Management console</h1>
      <p id="intro">
        Sign in with the subAdmin management secret. sub2api admin keys stay on the server side.
      </p>
      <form id="login-form">
        <label for="secret">Management secret</label>
        <input id="secret" name="secret" type="password" autocomplete="current-password" required />
        <button type="submit">Sign in</button>
        <p id="login-error" class="error hidden"></p>
      </form>
      <div id="console" class="hidden">
        <div class="status"><span class="dot"></span> authenticated session active</div>
        <p id="expires"></p>
        <form id="logout-form"><button class="secondary" type="submit">Sign out</button></form>
      </div>
    </section>

    <section class="grid">
      <article class="card">
        <h2>Health</h2>
        <p><code>/healthz</code> returns a JSON readiness check.</p>
      </article>
      <article class="card">
        <h2>Runtime</h2>
        <p>This shell is the starting point for the management console UI and backend API.</p>
      </article>
      <article class="card">
        <h2>Docs</h2>
        <p>The API docs will be available inside the authenticated management console.</p>
      </article>
    </section>
  </main>
  <script>
    const loginForm = document.querySelector('#login-form');
    const logoutForm = document.querySelector('#logout-form');
    const loginError = document.querySelector('#login-error');
    const consolePanel = document.querySelector('#console');
    const expires = document.querySelector('#expires');

    async function api(path, options = {}) {
      const res = await fetch(path, {
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
        ...options,
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || 'request failed');
      return data;
    }

    function showAuthed(data) {
      loginForm.classList.add('hidden');
      consolePanel.classList.remove('hidden');
      expires.textContent = data.expiresAt ? 'Session expires at ' + data.expiresAt : '';
    }

    function showLogin() {
      loginForm.classList.remove('hidden');
      consolePanel.classList.add('hidden');
      expires.textContent = '';
    }

    async function refreshMe() {
      const me = await api('api/auth/me');
      me.authenticated ? showAuthed(me) : showLogin();
    }

    loginForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      loginError.classList.add('hidden');
      const secret = new FormData(loginForm).get('secret');
      try {
        await api('api/auth/login', { method: 'POST', body: JSON.stringify({ secret }) });
        loginForm.reset();
        await refreshMe();
      } catch (error) {
        loginError.textContent = error.message;
        loginError.classList.remove('hidden');
      }
    });

    logoutForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      await api('api/auth/logout', { method: 'POST', body: '{}' });
      showLogin();
    });

    refreshMe().catch(showLogin);
  </script>
</body>
</html>
`)
}
