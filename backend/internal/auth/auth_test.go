package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"subadmin/internal/config"
	"subadmin/internal/db"
)

func newTestManager(t *testing.T, loginSecret string) (*Manager, *sql.DB) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mgr := NewManager(store.DB(), config.Config{
		LoginSecret:  loginSecret,
		CookiePath:   "/",
		CookieSecure: true,
		SessionTTL:   time.Hour,
	})
	return mgr, store.DB()
}

func loginForTest(t *testing.T, mgr *Manager, secret string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://subadmin.test/api/auth/login", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	if err := mgr.Login(rec, req, secret); err != nil {
		t.Fatalf("Login: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies length = %d, want 1", len(cookies))
	}
	return cookies[0]
}

func TestLoginStoresHashedSessionAndSecureCookie(t *testing.T) {
	mgr, database := newTestManager(t, "login-secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://subadmin.test/api/auth/login", nil)
	req.RemoteAddr = "203.0.113.10:1234"

	if err := mgr.Login(rec, req, "login-secret"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies length = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != CookieName || cookie.Value == "" {
		t.Fatalf("cookie = %#v", cookie)
	}
	setCookie := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(setCookie, want) {
			t.Fatalf("Set-Cookie %q missing %q", setCookie, want)
		}
	}

	var tokenHash string
	if err := database.QueryRow(`SELECT token_hash FROM sessions`).Scan(&tokenHash); err != nil {
		t.Fatalf("select token hash: %v", err)
	}
	if tokenHash != hashToken(cookie.Value) {
		t.Fatalf("stored token hash = %q, want hash of cookie", tokenHash)
	}
	if tokenHash == cookie.Value {
		t.Fatal("stored token hash equals plaintext cookie token")
	}

	currentReq := httptest.NewRequest(http.MethodGet, "https://subadmin.test/api/auth/me", nil)
	currentReq.AddCookie(cookie)
	session, err := mgr.Current(currentReq)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if session.TokenHash != tokenHash {
		t.Fatalf("session hash = %q, want %q", session.TokenHash, tokenHash)
	}
}

func TestLoginRejectsInvalidSecret(t *testing.T) {
	mgr, database := newTestManager(t, "login-secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://subadmin.test/api/auth/login", nil)

	if err := mgr.Login(rec, req, "wrong-secret"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Login error = %v, want ErrUnauthorized", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("sessions count = %d, want 0", count)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	mgr, database := newTestManager(t, "login-secret")
	cookie := loginForTest(t, mgr, "login-secret")

	logoutReq := httptest.NewRequest(http.MethodPost, "https://subadmin.test/api/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	if err := mgr.Logout(httptest.NewRecorder(), logoutReq); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	var revokedAt sql.NullInt64
	if err := database.QueryRow(`SELECT revoked_at FROM sessions WHERE token_hash = ?`, hashToken(cookie.Value)).Scan(&revokedAt); err != nil {
		t.Fatalf("select revoked_at: %v", err)
	}
	if !revokedAt.Valid {
		t.Fatal("revoked_at is NULL")
	}

	currentReq := httptest.NewRequest(http.MethodGet, "https://subadmin.test/api/auth/me", nil)
	currentReq.AddCookie(cookie)
	if _, err := mgr.Current(currentReq); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Current error = %v, want ErrUnauthorized", err)
	}
}

func TestCurrentRejectsExpiredSession(t *testing.T) {
	mgr, database := newTestManager(t, "login-secret")
	token := "expired-token"
	now := time.Now().Unix()
	_, err := database.Exec(`
INSERT INTO sessions (token_hash, expires_at, ip, user_agent, created_at, last_seen_at)
VALUES (?, ?, '', '', ?, ?)
`, hashToken(token), now-60, now-120, now-120)
	if err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://subadmin.test/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if _, err := mgr.Current(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Current error = %v, want ErrUnauthorized", err)
	}
}
