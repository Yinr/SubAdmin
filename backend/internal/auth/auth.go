package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"subadmin/internal/config"
)

const CookieName = "subadmin_session"

var ErrUnauthorized = errors.New("unauthorized")

type Manager struct {
	db  *sql.DB
	cfg config.Config
}

type Session struct {
	TokenHash string
	ExpiresAt time.Time
}

func NewManager(db *sql.DB, cfg config.Config) *Manager {
	return &Manager{db: db, cfg: cfg}
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request, secret string) error {
	if m.cfg.LoginSecret == "" || secret != m.cfg.LoginSecret {
		return ErrUnauthorized
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		return err
	}
	now := time.Now()
	expiresAt := now.Add(m.cfg.SessionTTL)
	_, err = m.db.Exec(`
INSERT INTO sessions (token_hash, expires_at, ip, user_agent, created_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?)
`, tokenHash, expiresAt.Unix(), clientIP(r), r.UserAgent(), now.Unix(), now.Unix())
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     m.cfg.CookiePath,
		Expires:  expiresAt,
		MaxAge:   int(m.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   m.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (m *Manager) Current(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, ErrUnauthorized
	}
	tokenHash := hashToken(cookie.Value)
	now := time.Now().Unix()
	var expiresAt int64
	err = m.db.QueryRow(`
SELECT expires_at FROM sessions
WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?
`, tokenHash, now).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	_, _ = m.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, now, tokenHash)
	return &Session{TokenHash: tokenHash, ExpiresAt: time.Unix(expiresAt, 0)}, nil
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(CookieName)
	if err == nil && cookie.Value != "" {
		_, err = m.db.Exec(`UPDATE sessions SET revoked_at = ? WHERE token_hash = ?`, time.Now().Unix(), hashToken(cookie.Value))
		if err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     m.cfg.CookiePath,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func newSessionToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
