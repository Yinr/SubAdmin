package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"subadmin/internal/db"
	"subadmin/internal/secretbox"
	"subadmin/internal/sites"
)

func TestSanitizeJSONForBrowserRedactsSensitiveFields(t *testing.T) {
	input := []byte(`{"name":"safe","credentials":{"refresh_token":"refresh-secret"},"nested":{"api_key":"sk-secret","items":[{"password":"pass-secret"}]}}`)
	sanitized := string(sanitizeJSONForBrowser(input))
	for _, secret := range []string{"refresh-secret", "sk-secret", "pass-secret"} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("sanitized JSON leaks %q: %s", secret, sanitized)
		}
	}
	if !strings.Contains(sanitized, "[redacted]") {
		t.Fatalf("sanitized JSON missing redaction marker: %s", sanitized)
	}
	if !strings.Contains(sanitized, "safe") {
		t.Fatalf("sanitized JSON removed safe value: %s", sanitized)
	}
}

func TestBuildImportPreviewItemRecognizesAndSanitizes(t *testing.T) {
	chunk := `{"platform":"anthropic","type":"oauth","name":"account-a","credentials":{"refresh_token":"secret-token"}}`
	item := buildImportPreviewItem(1, chunk)
	if !item.Recognized {
		t.Fatalf("item not recognized: %#v", item)
	}
	if item.Platform != "anthropic" || item.Type != "oauth" || item.Name != "account-a" {
		t.Fatalf("unexpected item fields: %#v", item)
	}
	if !stringSliceContains(item.CredentialFields, "refresh_token") {
		t.Fatalf("CredentialFields = %#v, want refresh_token", item.CredentialFields)
	}
	if strings.Contains(item.RawPreview, "secret-token") {
		t.Fatalf("RawPreview leaks secret: %q", item.RawPreview)
	}
	if !strings.Contains(item.RawPreview, "[redacted]") {
		t.Fatalf("RawPreview missing redaction marker: %q", item.RawPreview)
	}
	var previewJSON map[string]any
	if err := json.Unmarshal([]byte(item.RawPreview), &previewJSON); err != nil {
		t.Fatalf("RawPreview should remain valid JSON: %v; preview=%q", err, item.RawPreview)
	}

	invalid := buildImportPreviewItem(2, `name=missing-only`)
	if invalid.Recognized {
		t.Fatalf("invalid item recognized: %#v", invalid)
	}
	for _, field := range []string{"platform", "type", "credentials"} {
		if !stringSliceContains(invalid.MissingFields, field) {
			t.Fatalf("MissingFields = %#v, want %q", invalid.MissingFields, field)
		}
	}
}

func TestMarkImportDuplicates(t *testing.T) {
	items := []importPreviewItem{
		{Index: 1, DuplicateKey: "anthropic:oauth:a", Warnings: []string{}},
		{Index: 2, DuplicateKey: "anthropic:oauth:a", Warnings: []string{}},
		{Index: 3, DuplicateKey: "openai:apikey:b", Warnings: []string{}},
	}
	markImportDuplicates(items)
	if !stringSliceContains(items[1].Warnings, "疑似重复账号") {
		t.Fatalf("second duplicate warnings = %#v", items[1].Warnings)
	}
	if items[2].DuplicateKey != "" {
		t.Fatalf("unique item DuplicateKey = %q, want empty", items[2].DuplicateKey)
	}
}

func TestBuildImportAccountsAppliesNamePrefixAndModels(t *testing.T) {
	text := `{"platform":"anthropic","type":"oauth","name":"account-a","credentials":{"refresh_token":"secret-token"}}`
	accounts, err := buildImportAccounts(text, importAccountSettings{NamePrefix: "import-", Models: []string{"gpt-5.5", "gpt-5.5", "claude-sonnet"}})
	if err != nil {
		t.Fatalf("buildImportAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts length = %d, want 1", len(accounts))
	}
	account := accounts[0]
	if account.Name != "import-account-a" {
		t.Fatalf("Name = %q, want import-account-a", account.Name)
	}
	if account.Credentials["refresh_token"] != "secret-token" {
		t.Fatalf("refresh_token not preserved in execution payload")
	}
	wantMapping := map[string]string{"gpt-5.5": "gpt-5.5", "claude-sonnet": "claude-sonnet"}
	if !reflect.DeepEqual(account.Credentials["model_mapping"], wantMapping) {
		t.Fatalf("model_mapping = %#v, want %#v", account.Credentials["model_mapping"], wantMapping)
	}
}

func TestBuildImportAccountsRejectsMissingFields(t *testing.T) {
	_, err := buildImportAccounts(`name=missing-only`, importAccountSettings{})
	if err == nil || !strings.Contains(err.Error(), "缺少必要字段") {
		t.Fatalf("error = %v, want missing required fields", err)
	}
}

func TestRenderImportNamePrefixDate(t *testing.T) {
	got := renderImportNamePrefix("batch-{date}-", time.Date(2026, 5, 29, 10, 30, 0, 0, time.UTC))
	if got != "batch-20260529-" {
		t.Fatalf("renderImportNamePrefix = %q", got)
	}
}

func TestBuildImportAccountItemsMapsPartialResults(t *testing.T) {
	accounts := []importAccountExecution{
		{Index: 1, Name: "account-a", Platform: "anthropic", Type: "oauth"},
		{Index: 2, Name: "account-b", Platform: "openai", Type: "apikey"},
	}
	items := buildImportAccountItems(accounts, 207, 120, []byte(`{"data":{"results":[{"success":true,"id":101},{"success":false,"error":"duplicate name"}]}}`), nil)
	if len(items) != 2 {
		t.Fatalf("items length = %d, want 2", len(items))
	}
	if items[0]["ok"] != true || items[0]["accountId"] != int64(101) {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1]["ok"] != false || items[1]["error"] != "duplicate name" {
		t.Fatalf("second item = %#v", items[1])
	}
}

func TestBuildImportAccountItemsMapsRequestError(t *testing.T) {
	accounts := []importAccountExecution{{Index: 1, Name: "account-a", Platform: "anthropic", Type: "oauth"}}
	items := buildImportAccountItems(accounts, 0, 50, nil, errors.New("connection refused"))
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	if items[0]["ok"] != false || items[0]["error"] != "connection refused" {
		t.Fatalf("item = %#v", items[0])
	}
}

func TestBuildTokenRefreshItemsMapsWarningsAndErrors(t *testing.T) {
	items := buildTokenRefreshItems(
		[]int64{1, 2},
		map[string]accountMeta{"1": {Name: "one"}, "2": {Name: "two"}},
		200,
		80,
		[]byte(`{"data":{"warnings":[{"account_id":1,"warning":"near expiry"}],"errors":[{"account_id":2,"error":"refresh failed"}]}}`),
		nil,
	)
	if len(items) != 2 {
		t.Fatalf("items length = %d, want 2", len(items))
	}
	if items[0]["ok"] != true || items[0]["warning"] != "near expiry" || items[0]["name"] != "one" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1]["ok"] != false || items[1]["error"] != "refresh failed" || items[1]["name"] != "two" {
		t.Fatalf("second item = %#v", items[1])
	}
}

func TestAuditSummaryRedactsSensitiveTopLevelKeys(t *testing.T) {
	got := sanitizeAuditSummary(map[string]any{
		"filename":    "accounts.json",
		"credentials": map[string]any{"refresh_token": "secret"},
		"api_key":     "sk-secret",
	})
	if got["filename"] != "accounts.json" {
		t.Fatalf("safe field changed: %#v", got)
	}
	if got["credentials"] != "[redacted]" || got["api_key"] != "[redacted]" {
		t.Fatalf("sensitive fields not redacted: %#v", got)
	}
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestWriteAccountsSearchByNamesQueriesUpstreamAndRedacts(t *testing.T) {
	type upstreamCall struct {
		Search   string
		PageSize string
		APIKey   string
	}
	var calls []upstreamCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/accounts" {
			t.Fatalf("upstream path = %q, want /api/v1/admin/accounts", r.URL.Path)
		}
		calls = append(calls, upstreamCall{Search: r.URL.Query().Get("search"), PageSize: r.URL.Query().Get("page_size"), APIKey: r.Header.Get("x-api-key")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"items":[{"id":1,"name":"` + r.URL.Query().Get("search") + ` account","credentials":{"refresh_token":"secret-token"}}]}}`))
	}))
	defer server.Close()
	svc, siteID := newTestSiteService(t, server.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sites/1/accounts/search-by-names", bytes.NewReader([]byte(`{"names":[" alpha ","# note","alpha","beta",""],"skipComments":true}`)))

	writeAccountsSearchByNames(rec, req, svc, siteID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 2 {
		t.Fatalf("upstream calls = %#v, want 2 effective keywords", calls)
	}
	for i, want := range []string{"alpha", "beta"} {
		if calls[i].Search != want || calls[i].PageSize != "100" || calls[i].APIKey != "admin-key" {
			t.Fatalf("call[%d] = %#v", i, calls[i])
		}
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-token") {
		t.Fatalf("search response leaks credentials: %s", body)
	}
	var out struct {
		Items []struct {
			Keyword  string           `json:"keyword"`
			Accounts []map[string]any `json:"accounts"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if len(out.Items) != 2 || out.Items[0].Keyword != "alpha" || out.Items[1].Keyword != "beta" {
		t.Fatalf("items = %#v, want alpha and beta", out.Items)
	}
}

func TestWriteAccountsSearchByNamesRejectsTooManyKeywords(t *testing.T) {
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
	}))
	defer server.Close()
	svc, siteID := newTestSiteService(t, server.URL)
	names := make([]string, 51)
	for i := range names {
		names[i] = "keyword-" + string(rune('a'+i))
	}
	payload, err := json.Marshal(map[string]any{"names": names, "skipComments": true})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sites/1/accounts/search-by-names", bytes.NewReader(payload))

	writeAccountsSearchByNames(rec, req, svc, siteID)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestWriteAccountsSearchByNamesReportsPerKeywordErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("search") {
		case "ok":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[{"id":7,"name":"ok account"}]}}`))
		case "broken":
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream unavailable"})
		default:
			t.Fatalf("unexpected search query: %q", r.URL.Query().Get("search"))
		}
	}))
	defer server.Close()
	svc, siteID := newTestSiteService(t, server.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sites/1/accounts/search-by-names", bytes.NewReader([]byte(`{"names":["ok","broken"],"skipComments":true}`)))

	writeAccountsSearchByNames(rec, req, svc, siteID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Keyword    string           `json:"keyword"`
			Accounts   []map[string]any `json:"accounts"`
			Error      string           `json:"error"`
			StatusCode int              `json:"statusCode"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items length = %d, want 2", len(out.Items))
	}
	if out.Items[0].Keyword != "ok" || len(out.Items[0].Accounts) != 1 || out.Items[0].Error != "" {
		t.Fatalf("ok item = %#v", out.Items[0])
	}
	if out.Items[1].Keyword != "broken" || out.Items[1].StatusCode != http.StatusBadGateway || !strings.Contains(out.Items[1].Error, "upstream unavailable") {
		t.Fatalf("broken item = %#v, want explicit per-keyword upstream error", out.Items[1])
	}
}

func TestWriteAccountsSearchByNamesReportsTruncatedResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search") != "broad" {
			t.Fatalf("search = %q, want broad", r.URL.Query().Get("search"))
		}
		items := make([]map[string]any, 100)
		for i := range items {
			items[i] = map[string]any{"id": i + 1, "name": "broad account"}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"items": items, "total": 101, "page": 1, "page_size": 100, "pages": 2}})
	}))
	defer server.Close()
	svc, siteID := newTestSiteService(t, server.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sites/1/accounts/search-by-names", bytes.NewReader([]byte(`{"names":["broad"],"skipComments":true}`)))

	writeAccountsSearchByNames(rec, req, svc, siteID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Keyword   string           `json:"keyword"`
			Accounts  []map[string]any `json:"accounts"`
			Total     int64            `json:"total"`
			Returned  int              `json:"returned"`
			Truncated bool             `json:"truncated"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items length = %d, want 1", len(out.Items))
	}
	item := out.Items[0]
	if item.Keyword != "broad" || len(item.Accounts) != 100 || item.Total != 101 || item.Returned != 100 || !item.Truncated {
		t.Fatalf("item = %#v, want visible truncation metadata", item)
	}
}

func newTestSiteService(t *testing.T, baseURL string) (*sites.Service, int64) {
	t.Helper()
	store, err := db.Open(t.TempDir() + "/subadmin.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	box, err := secretbox.New("test-secret")
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	svc := sites.NewService(store.DB(), box)
	site, err := svc.Create(t.Context(), sites.CreateInput{Name: "test", BaseURL: baseURL, AdminKey: "admin-key"})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	return svc, site.ID
}
