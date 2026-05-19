package sites

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"subadmin/internal/secretbox"
)

type Service struct {
	db     *sql.DB
	box    *secretbox.Box
	client *http.Client
}

type Site struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	BaseURL         string `json:"baseUrl"`
	AdminKeyHint    string `json:"adminKeyHint"`
	Note            string `json:"note"`
	IsDefault       bool   `json:"isDefault"`
	Enabled         bool   `json:"enabled"`
	LastCheckAt     *int64 `json:"lastCheckAt,omitempty"`
	LastCheckStatus string `json:"lastCheckStatus,omitempty"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type CreateInput struct {
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	AdminKey  string `json:"adminKey"`
	Note      string `json:"note"`
	IsDefault bool   `json:"isDefault"`
	Enabled   *bool  `json:"enabled"`
}

type UpdateInput struct {
	Name      *string `json:"name"`
	BaseURL   *string `json:"baseUrl"`
	AdminKey  *string `json:"adminKey"`
	Note      *string `json:"note"`
	IsDefault *bool   `json:"isDefault"`
	Enabled   *bool   `json:"enabled"`
}

func NewService(db *sql.DB, box *secretbox.Box) *Service {
	return &Service{
		db:  db,
		box: box,
		client: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

func (s *Service) List(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, base_url, admin_key_ciphertext, note, is_default, enabled, last_check_at, last_check_status, created_at, updated_at
FROM sites ORDER BY is_default DESC, id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Site
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, site)
	}
	return result, rows.Err()
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*Site, error) {
	name := strings.TrimSpace(input.Name)
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return nil, err
	}
	adminKey := strings.TrimSpace(input.AdminKey)
	if name == "" || adminKey == "" {
		return nil, errors.New("name and adminKey are required")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	ciphertext, err := s.box.Encrypt(adminKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE sites SET is_default = 0`); err != nil {
			return nil, err
		}
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO sites (name, base_url, admin_key_ciphertext, note, is_default, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, name, baseURL, ciphertext, strings.TrimSpace(input.Note), boolInt(input.IsDefault), boolInt(enabled), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Get(ctx context.Context, id int64) (*Site, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, base_url, admin_key_ciphertext, note, is_default, enabled, last_check_at, last_check_status, created_at, updated_at
FROM sites WHERE id = ?
`, id)
	site, err := scanSite(row)
	if err != nil {
		return nil, err
	}
	return &site, nil
}

func (s *Service) Update(ctx context.Context, id int64, input UpdateInput) (*Site, error) {
	current, err := s.getStored(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
	}
	if input.BaseURL != nil {
		current.BaseURL, err = normalizeBaseURL(*input.BaseURL)
		if err != nil {
			return nil, err
		}
	}
	if input.AdminKey != nil && strings.TrimSpace(*input.AdminKey) != "" {
		current.AdminKeyCiphertext, err = s.box.Encrypt(strings.TrimSpace(*input.AdminKey))
		if err != nil {
			return nil, err
		}
	}
	if input.Note != nil {
		current.Note = strings.TrimSpace(*input.Note)
	}
	if input.IsDefault != nil {
		current.IsDefault = *input.IsDefault
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	if current.Name == "" {
		return nil, errors.New("name is required")
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if current.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE sites SET is_default = 0 WHERE id != ?`, id); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `
UPDATE sites SET name = ?, base_url = ?, admin_key_ciphertext = ?, note = ?, is_default = ?, enabled = ?, updated_at = ?
WHERE id = ?
`, current.Name, current.BaseURL, current.AdminKeyCiphertext, current.Note, boolInt(current.IsDefault), boolInt(current.Enabled), now, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) Test(ctx context.Context, id int64) (map[string]any, error) {
	stored, err := s.getStored(ctx, id)
	if err != nil {
		return nil, err
	}
	adminKey, err := s.box.Decrypt(stored.AdminKeyCiphertext)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(stored.BaseURL, "/") + "/api/v1/admin/settings"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", adminKey)
	started := time.Now()
	resp, err := s.client.Do(req)
	latencyMS := time.Since(started).Milliseconds()
	now := time.Now().Unix()
	status := "failed"
	result := map[string]any{"ok": false, "latencyMs": latencyMS}
	if err != nil {
		result["error"] = err.Error()
	} else {
		defer resp.Body.Close()
		status = fmt.Sprintf("http_%d", resp.StatusCode)
		result["statusCode"] = resp.StatusCode
		result["ok"] = resp.StatusCode >= 200 && resp.StatusCode < 300
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sites SET last_check_at = ?, last_check_status = ?, updated_at = ? WHERE id = ?`, now, status, now, id)
	return result, nil
}

type storedSite struct {
	ID                 int64
	Name               string
	BaseURL            string
	AdminKeyCiphertext string
	Note               string
	IsDefault          bool
	Enabled            bool
}

func (s *Service) getStored(ctx context.Context, id int64) (*storedSite, error) {
	var rawDefault, rawEnabled int
	var result storedSite
	err := s.db.QueryRowContext(ctx, `SELECT id, name, base_url, admin_key_ciphertext, note, is_default, enabled FROM sites WHERE id = ?`, id).
		Scan(&result.ID, &result.Name, &result.BaseURL, &result.AdminKeyCiphertext, &result.Note, &rawDefault, &rawEnabled)
	if err != nil {
		return nil, err
	}
	result.IsDefault = rawDefault == 1
	result.Enabled = rawEnabled == 1
	return &result, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSite(row rowScanner) (Site, error) {
	var site Site
	var ciphertext string
	var isDefault, enabled int
	var lastCheckAt sql.NullInt64
	var lastCheckStatus sql.NullString
	err := row.Scan(&site.ID, &site.Name, &site.BaseURL, &ciphertext, &site.Note, &isDefault, &enabled, &lastCheckAt, &lastCheckStatus, &site.CreatedAt, &site.UpdatedAt)
	if err != nil {
		return site, err
	}
	site.AdminKeyHint = keyHint(ciphertext)
	site.IsDefault = isDefault == 1
	site.Enabled = enabled == 1
	if lastCheckAt.Valid {
		site.LastCheckAt = &lastCheckAt.Int64
	}
	if lastCheckStatus.Valid {
		site.LastCheckStatus = lastCheckStatus.String
	}
	return site, nil
}

func normalizeBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("baseUrl is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("baseUrl must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("baseUrl scheme must be http or https")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func keyHint(ciphertext string) string {
	if len(ciphertext) <= 8 {
		return "stored"
	}
	return "stored:" + ciphertext[:4] + "..." + ciphertext[len(ciphertext)-4:]
}

func IDFromPath(path, prefix string) (int64, string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimPrefix(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
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

func DecodeJSON(r *http.Request, value any) error {
	defer r.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		return err
	}
	if strings.TrimSpace(buf.String()) == "" {
		return nil
	}
	return json.Unmarshal(buf.Bytes(), value)
}
