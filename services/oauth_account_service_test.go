package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOAuthPKCEChallengeUsesRawURLBase64SHA256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	if got := pkceChallenge(verifier); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("unexpected PKCE challenge: %s", got)
	}
}

func TestNormalizeCodexImportFromSub2API(t *testing.T) {
	root := map[string]any{
		"name":     "team account",
		"platform": "openai",
		"type":     "oauth",
		"credentials": map[string]any{
			"access_token":       "access-token",
			"refresh_token":      "refresh-token",
			"chatgpt_account_id": "acct-1",
			"chatgpt_user_id":    "user-1",
			"plan_type":          "pro",
			"expires_at":         time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	item, err := normalizeCodexImport("sub2api", root)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if item.AccountID != "acct-1" || item.UserID != "user-1" || item.PlanType != "pro" {
		t.Fatalf("identity not normalized: %+v", item)
	}
	if strings.Contains(item.Source, "token") {
		t.Fatalf("source unexpectedly contains credential material: %q", item.Source)
	}
}

func TestOAuthImportAcceptsNewlineDelimitedJSON(t *testing.T) {
	service := newOAuthTestService(t)
	content := strings.Join([]string{
		`{"type":"codex","account_id":"acct-line-1","access_token":"access-1","refresh_token":"refresh-1","expires_at":"2099-01-01T00:00:00Z"}`,
		`{"type":"codex","account_id":"acct-line-2","access_token":"access-2","refresh_token":"refresh-2","expires_at":"2099-01-01T00:00:00Z"}`,
	}, "\n")
	result, err := service.ImportJSON(OAuthImportRequest{Platform: "codex", Source: "cpa", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 2 || result.Failed != 0 || len(result.Items) != 2 {
		t.Fatalf("JSONL 导入结果错误: %#v", result)
	}
}

func TestNormalizeCodexRejectsAgentIdentity(t *testing.T) {
	_, err := normalizeCodexImport("cpa", map[string]any{
		"type":           "codex",
		"auth_mode":      "agentIdentity",
		"agent_identity": map[string]any{"agent_runtime_id": "runtime"},
	})
	if err == nil || !strings.Contains(err.Error(), "Agent Identity") {
		t.Fatalf("expected Agent Identity rejection, got %v", err)
	}
}

func TestNormalizeClaudeSnapshotKeepsOAuthOnly(t *testing.T) {
	item, err := normalizeClaudeImport("claude_code_snapshot", map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "access-token",
			"refreshToken": "refresh-token",
			"expiresAt":    time.Now().Add(time.Hour).UnixMilli(),
		},
		"oauthAccount": map[string]any{
			"emailAddress": "person@example.com",
			"accountUuid":  "account-uuid",
		},
	})
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if item.Email != "person@example.com" || item.AccountID != "account-uuid" || item.RefreshToken == "" {
		t.Fatalf("Claude snapshot not normalized: %+v", item)
	}
}

func TestOAuthImportResultDoesNotMarshalSecrets(t *testing.T) {
	result := OAuthImportResult{Items: []OAuthImportItem{{Account: &OAuthAccountSummary{ID: "codex-id", Email: "person@example.com"}}}}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{`"accessToken":`, `"refreshToken":`, `"idToken":`, `"access_token":`, `"refresh_token":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("DTO contains secret field %q: %s", forbidden, text)
		}
	}
}

func TestProviderEligibleForOAuthRequiresReference(t *testing.T) {
	provider := Provider{Enabled: true, APIURL: "https://upstream.example", CredentialType: "oauth"}
	if ProviderEligibleForRelay(provider, "codex") {
		t.Fatal("OAuth provider without credentialRef must not be eligible")
	}
	provider.CredentialRef = "codex-id"
	if !ProviderEligibleForRelay(provider, "codex") {
		t.Fatal("OAuth provider with credentialRef should be eligible")
	}
}

func TestResolveProviderOAuthAddsProtectedPlatformHeaders(t *testing.T) {
	service := newOAuthTestService(t)
	service.now = func() time.Time { return time.Unix(1700000000, 0) }
	claude, err := service.upsertNormalized(oauthNormalizedItem{
		Platform: OAuthPlatformClaude, AuthMode: OAuthAuthModeClaudeCode,
		Email: "claude@example.com", AccessToken: "claude-access", RefreshToken: "claude-refresh",
		AccessTokenExpiresAt: service.now().Add(time.Hour).UnixMilli(), Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := Provider{APIURL: "https://upstream.example", CredentialType: "oauth", CredentialRef: claude.ID}
	resolved, headers, err := ResolveProviderCredential(service, provider, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIKey != "claude-access" || resolved.AuthScheme != "bearer" {
		t.Fatalf("OAuth access token 未解析到临时 Provider: %#v", resolved)
	}
	if headers["Anthropic-Beta"] != claudeOAuthBetaHeader {
		t.Fatalf("Claude OAuth beta header 错误: %#v", headers)
	}
	base := map[string]string{"anthropic-beta": "claude-code-20250219", "Authorization": "client"}
	if err := ApplyOAuthCredentialHeaders(base, headers); err != nil {
		t.Fatal(err)
	}
	if got := headerValue(base, "anthropic-beta"); got != "claude-code-20250219,oauth-2025-04-20" {
		t.Fatalf("OAuth beta header 未合并: %q", got)
	}
	if headerValue(base, "Authorization") != "client" {
		t.Fatal("OAuth beta header 测试不应修改普通 Authorization")
	}
}

func TestResolveProviderCodexOAuthAddsAccountHeader(t *testing.T) {
	service := newOAuthTestService(t)
	service.now = func() time.Time { return time.Unix(1700000000, 0) }
	account, err := service.upsertNormalized(oauthNormalizedItem{
		Platform: OAuthPlatformCodex, AuthMode: OAuthAuthModeCodex, AccountID: "acct-test",
		UserID: "user-test", AccessToken: "codex-access", RefreshToken: "codex-refresh",
		AccessTokenExpiresAt: service.now().Add(time.Hour).UnixMilli(), Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, headers, err := ResolveProviderCredential(service, Provider{APIURL: "https://upstream.example", CredentialType: "oauth", CredentialRef: account.ID}, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if headers["ChatGPT-Account-Id"] != "acct-test" {
		t.Fatalf("Codex account header 错误: %#v", headers)
	}
	if err := ApplyOAuthCredentialHeaders(map[string]string{"ChatGPT-Account-Id": "client-acct"}, headers); err != nil {
		t.Fatal(err)
	}
	if err := ApplyOAuthCredentialHeaders(map[string]string{}, map[string]string{"Authorization": "forbidden"}); err == nil {
		t.Fatal("OAuth 解析器不应允许注入任意 Header")
	}
}

func TestOAuthRefreshLockRotatesRefreshTokenOnce(t *testing.T) {
	service := newOAuthTestService(t)
	service.now = func() time.Time { return time.Unix(1700000000, 0) }
	account, err := service.upsertNormalized(oauthNormalizedItem{
		Platform: OAuthPlatformCodex, AuthMode: OAuthAuthModeCodex, AccountID: "acct-lock",
		AccessToken: "expired-access", RefreshToken: "old-refresh", AccessTokenExpiresAt: service.now().Add(-time.Minute).UnixMilli(), Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	refreshCalls := 0
	service.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		refreshCalls++
		mu.Unlock()
		return jsonHTTPResponse(http.StatusOK, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"id_token":""}`), nil
	})
	const callers = 8
	results := make(chan string, callers)
	errors := make(chan error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, token, resolveErr := service.resolveAccessToken(account.ID, false)
			if resolveErr != nil {
				errors <- resolveErr
				return
			}
			results <- token
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	for token := range results {
		if token != "new-access" {
			t.Fatalf("并发刷新返回旧 Token: %q", token)
		}
	}
	mu.Lock()
	gotCalls := refreshCalls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("同一账号并发刷新应只请求一次，实际 %d 次", gotCalls)
	}
	_, secrets, err := service.loadStoresLocked()
	if err != nil {
		t.Fatal(err)
	}
	if secrets.Credentials[account.ID].RefreshToken != "new-refresh" {
		t.Fatalf("refresh token rotation 未持久化: %#v", secrets.Credentials[account.ID])
	}
}

func TestOAuthRefreshFailureKeepsPreviousCredential(t *testing.T) {
	service := newOAuthTestService(t)
	service.now = func() time.Time { return time.Unix(1700000000, 0) }
	account, err := service.upsertNormalized(oauthNormalizedItem{
		Platform: OAuthPlatformClaude, AuthMode: OAuthAuthModeClaudeCode, Email: "keep@example.com",
		AccessToken: "old-access", RefreshToken: "old-refresh", AccessTokenExpiresAt: service.now().Add(-time.Minute).UnixMilli(), Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusUnauthorized, `{"error":"invalid_grant"}`), nil
	})
	if _, _, err := service.resolveAccessToken(account.ID, false); err == nil {
		t.Fatal("refresh 失败应返回错误")
	}
	stored, secrets, err := service.loadStoresLocked()
	if err != nil {
		t.Fatal(err)
	}
	if secrets.Credentials[account.ID].AccessToken != "old-access" || secrets.Credentials[account.ID].RefreshToken != "old-refresh" {
		t.Fatalf("refresh 失败覆盖了旧凭据: %#v", secrets.Credentials[account.ID])
	}
	if len(stored.Accounts) != 1 || stored.Accounts[0].Status != string(OAuthAccountRefreshError) {
		t.Fatalf("refresh 失败状态未保留: %#v", stored.Accounts)
	}
}

func TestOAuthCLIApplyRejectsExternalChange(t *testing.T) {
	service := newOAuthTestService(t)
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"OPENAI_API_KEY":"keep"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{"access_token": "oauth-access"}}
	if err := service.applyCLIFiles("codex", []oauthCLIFileInput{{Path: path, Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"OPENAI_API_KEY":"changed-by-user"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.applyCLIFiles("codex", []oauthCLIFileInput{{Path: path, Payload: payload}}); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("CLI 外部修改应拒绝覆盖，实际: %v", err)
	}
	if err := service.restoreCLIFiles("codex"); err == nil || !strings.Contains(err.Error(), "外部修改") {
		t.Fatalf("CLI 外部修改应拒绝恢复，实际: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func newOAuthTestService(t *testing.T) *OAuthAccountService {
	t.Helper()
	service := NewOAuthAccountService()
	service.configDir = t.TempDir()
	return service
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d", status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
