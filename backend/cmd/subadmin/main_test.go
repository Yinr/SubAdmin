package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
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
