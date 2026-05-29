package sites

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"subadmin/internal/db"
	"subadmin/internal/secretbox"
)

func newTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "sites.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	box, err := secretbox.New("test-secret")
	if err != nil {
		t.Fatalf("new secretbox: %v", err)
	}
	return NewService(store.DB(), box), store.DB()
}

func boolPtr(value bool) *bool { return &value }

func createTestSite(t *testing.T, svc *Service, baseURL, adminKey string) *Site {
	t.Helper()
	site, err := svc.Create(context.Background(), CreateInput{
		Name:      "primary",
		BaseURL:   baseURL,
		AdminKey:  adminKey,
		IsDefault: true,
		Enabled:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return site
}

func TestCreateEncryptsAdminKeyAndReturnsOnlyHint(t *testing.T) {
	svc, database := newTestService(t)
	site := createTestSite(t, svc, "https://example.com/admin/?debug=1#frag", "admin-key-123")

	if site.BaseURL != "https://example.com/admin" {
		t.Fatalf("BaseURL = %q, want normalized URL", site.BaseURL)
	}
	if site.AdminKeyHint == "" || strings.Contains(site.AdminKeyHint, "admin-key-123") {
		t.Fatalf("AdminKeyHint = %q", site.AdminKeyHint)
	}
	var ciphertext string
	if err := database.QueryRow(`SELECT admin_key_ciphertext FROM sites WHERE id = ?`, site.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("select ciphertext: %v", err)
	}
	if ciphertext == "admin-key-123" || strings.Contains(ciphertext, "admin-key-123") {
		t.Fatalf("admin key stored in plaintext: %q", ciphertext)
	}

	items, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("sites length = %d, want 1", len(items))
	}
	if strings.Contains(items[0].AdminKeyHint, "admin-key-123") {
		t.Fatalf("listed AdminKeyHint leaks key: %q", items[0].AdminKeyHint)
	}
}

func TestUpdatePreservesAndRotatesAdminKey(t *testing.T) {
	var expectedKey atomic.Value
	expectedKey.Store("old-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/settings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != expectedKey.Load().(string) {
			t.Errorf("x-api-key = %q, want %q", got, expectedKey.Load().(string))
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	svc, _ := newTestService(t)
	site := createTestSite(t, svc, server.URL, "old-key")
	name := "renamed"
	if _, err := svc.Update(context.Background(), site.ID, UpdateInput{Name: &name}); err != nil {
		t.Fatalf("Update without admin key: %v", err)
	}
	result, err := svc.Test(context.Background(), site.ID)
	if err != nil {
		t.Fatalf("Test old key: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("Test old key result = %#v", result)
	}

	newKey := "new-key"
	if _, err := svc.Update(context.Background(), site.ID, UpdateInput{AdminKey: &newKey}); err != nil {
		t.Fatalf("Update admin key: %v", err)
	}
	expectedKey.Store("new-key")
	result, err = svc.Test(context.Background(), site.ID)
	if err != nil {
		t.Fatalf("Test new key: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("Test new key result = %#v", result)
	}
}

func TestAdminPOSTJSONWithHeadersSendsServerSideKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/admin/accounts/batch" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "admin-key" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "subadmin-import-1" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if !strings.Contains(string(body), `"accounts"`) {
			t.Errorf("body = %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"results":[{"success":true,"id":42}]}}`))
	}))
	defer server.Close()

	svc, _ := newTestService(t)
	site := createTestSite(t, svc, server.URL, "admin-key")
	body, status, err := svc.AdminPOSTJSONWithHeaders(context.Background(), site.ID, "/api/v1/admin/accounts/batch", map[string]any{"accounts": []any{}}, map[string]string{"Idempotency-Key": "subadmin-import-1"})
	if err != nil {
		t.Fatalf("AdminPOSTJSONWithHeaders: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want %d", status, http.StatusCreated)
	}
	if !strings.Contains(string(body), `"success":true`) {
		t.Fatalf("body = %s", body)
	}
}

func TestDefaultSiteUniquenessAndURLValidation(t *testing.T) {
	svc, _ := newTestService(t)
	first, err := svc.Create(context.Background(), CreateInput{Name: "one", BaseURL: "https://one.example", AdminKey: "one", IsDefault: true})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := svc.Create(context.Background(), CreateInput{Name: "two", BaseURL: "https://two.example", AdminKey: "two", IsDefault: true})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	items, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	defaults := map[int64]bool{}
	for _, item := range items {
		if item.IsDefault {
			defaults[item.ID] = true
		}
	}
	if defaults[first.ID] || !defaults[second.ID] || len(defaults) != 1 {
		t.Fatalf("defaults = %#v, want only second default", defaults)
	}
	if _, err := svc.Create(context.Background(), CreateInput{Name: "bad", BaseURL: "ftp://bad.example", AdminKey: "bad"}); err == nil {
		t.Fatal("Create accepted invalid baseUrl scheme")
	}
}
