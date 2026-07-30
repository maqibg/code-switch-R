package services

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type grokRoundTripFunc func(*http.Request) (*http.Response, error)

func (function grokRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestParseGrokOAuthCredentialsSupportsCPAAndNativeAuth(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	token := grokTestJWT(map[string]any{"sub": "user-1", "email": "one@example.com", "exp": expiresAt.Unix()})
	cpa := []byte(`{"type":"xai","access_token":"` + token + `","refresh_token":"refresh","expired":"` + expiresAt.Format(time.RFC3339) + `"}`)
	parsed, err := parseGrokOAuthCredentials(cpa, "xai-one.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Account.Subject != "user-1" || parsed[0].Account.Email != "one@example.com" {
		t.Fatalf("CPA 解析结果 = %#v", parsed)
	}
	if parsed[0].Account.AuthEntry["key"] != token || parsed[0].Account.Source != "cpa-xai" {
		t.Fatalf("CPA 凭据规范化错误: %#v", parsed[0].Account)
	}

	native := []byte(`{
  "other::scope": {"key":"other-token","oidc_issuer":"https://example.com"},
  "https://auth.x.ai::client-a": {"key":"` + token + `","refresh_token":"r1","oidc_issuer":"https://auth.x.ai","oidc_client_id":"client-a"},
  "https://auth.x.ai::client-b": {"key":"` + grokTestJWT(map[string]any{"sub": "user-2"}) + `","refresh_token":"r2","oidc_issuer":"https://auth.x.ai","oidc_client_id":"client-b"}
}`)
	parsed, err = parseGrokOAuthCredentials(native, "auth.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 {
		t.Fatalf("native auth 账号数 = %d, want 2", len(parsed))
	}
	if parsed[0].Account.ID == parsed[1].Account.ID {
		t.Fatal("不同 subject/client ID 不应得到相同账号 ID")
	}
}

func TestMergeGrokAccountIntoAuthPreservesOtherScopes(t *testing.T) {
	current := []byte(`{
  "other::scope": {"key":"keep","unknown":true},
  "https://auth.x.ai::client": {"key":"old","user_id":"same","runtime_only":"keep"}
}`)
	account := grokOAuthAccount{
		Issuer: "https://auth.x.ai", ClientID: "client", ScopeKey: "https://auth.x.ai::client",
		AuthEntry: map[string]any{"key": "new", "refresh_token": "rotated", "user_id": "same"},
	}
	merged, err := mergeGrokAccountIntoAuth(current, account)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]map[string]any
	if err := json.Unmarshal(merged, &root); err != nil {
		t.Fatal(err)
	}
	if root["other::scope"]["key"] != "keep" || root["other::scope"]["unknown"] != true {
		t.Fatalf("其他 scope 被破坏: %#v", root["other::scope"])
	}
	xai := root["https://auth.x.ai::client"]
	if xai["key"] != "new" || xai["refresh_token"] != "rotated" || xai["runtime_only"] != "keep" {
		t.Fatalf("xAI scope 合并错误: %#v", xai)
	}
}

func TestEnsureFreshGrokAccountRotatesTokens(t *testing.T) {
	var received url.Values
	service := NewGrokBuildService("127.0.0.1:18100", nil, nil)
	service.httpClient = &http.Client{Transport: grokRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		received, _ = url.ParseQuery(string(body))
		return grokHTTPJSON(200, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`), nil
	})}
	account := grokOAuthAccount{
		ClientID: "client", Issuer: grokXAIIssuer, TokenEndpoint: "https://auth.x.ai/oauth2/token",
		AuthEntry: map[string]any{"key": "old-access", "refresh_token": "old-refresh"},
	}
	updated, err := service.ensureFreshGrokAccount(t.Context(), account, true)
	if err != nil {
		t.Fatal(err)
	}
	if received.Get("grant_type") != "refresh_token" || received.Get("client_id") != "client" || received.Get("refresh_token") != "old-refresh" {
		t.Fatalf("刷新表单 = %#v", received)
	}
	if updated.AuthEntry["key"] != "new-access" || updated.AuthEntry["refresh_token"] != "new-refresh" || updated.ExpiresAt == "" {
		t.Fatalf("Token 未正确轮换: %#v", updated)
	}
}

func TestEnsureFreshGrokAccountMarksInvalidGrantWithoutTokenLeak(t *testing.T) {
	service := NewGrokBuildService("127.0.0.1:18100", nil, nil)
	service.httpClient = &http.Client{Transport: grokRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return grokHTTPJSON(400, `{"error":"invalid_grant","error_description":"revoked"}`), nil
	})}
	account := grokOAuthAccount{
		ClientID: grokXAIClientID, Issuer: grokXAIIssuer, TokenEndpoint: "https://auth.x.ai/oauth2/token",
		AuthEntry: map[string]any{"key": "secret-access", "refresh_token": "secret-refresh"},
	}
	updated, err := service.ensureFreshGrokAccount(t.Context(), account, true)
	if err == nil || !updated.NeedsRelogin || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("invalid_grant 结果 = %#v, err=%v", updated, err)
	}
	if strings.Contains(err.Error(), "secret-access") || strings.Contains(err.Error(), "secret-refresh") {
		t.Fatalf("错误泄露 Token: %v", err)
	}
}

func TestFetchGrokQuotaParsesThreeEndpoints(t *testing.T) {
	service := NewGrokBuildService("127.0.0.1:18100", nil, nil)
	service.httpClient = &http.Client{Transport: grokRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.RequestURI() {
		case "/v1/billing?format=credits":
			return grokHTTPJSON(200, `{"config":{"credit_usage_percent":{"val":"25"},"current_period":{"end":"2030-01-02T03:04:05Z"}}}`), nil
		case "/v1/billing":
			return grokHTTPJSON(200, `{"config":{"monthly_limit":"200","totalUsed":50,"billing_period_end":"2030-02-01T00:00:00Z"}}`), nil
		case "/v1/user?include=subscription":
			return grokHTTPJSON(200, `{"user":{"subscription_tier":"supergrok"}}`), nil
		default:
			return grokHTTPJSON(404, `{}`), nil
		}
	})}
	account := grokOAuthAccount{AuthEntry: map[string]any{"key": "access"}}
	updated, err := service.fetchGrokQuota(t.Context(), account)
	if err != nil {
		t.Fatal(err)
	}
	quota := updated.Quota
	if quota.WeeklyRemainingPercent == nil || *quota.WeeklyRemainingPercent != 75 {
		t.Fatalf("周额度 = %#v", quota.WeeklyRemainingPercent)
	}
	if quota.MonthlyRemainingPercent == nil || *quota.MonthlyRemainingPercent != 75 {
		t.Fatalf("月额度 = %#v", quota.MonthlyRemainingPercent)
	}
	if quota.PlanType != "supergrok" || quota.WeeklyResetAt == "" || quota.MonthlyResetAt == "" || quota.DataUpdatedAt == "" {
		t.Fatalf("额度快照不完整: %#v", quota)
	}
	if updated.LastError != "" {
		t.Fatalf("正常额度刷新不应有错误: %s", updated.LastError)
	}
}

func TestFetchGrokQuotaKeepsPartialSuccessVisible(t *testing.T) {
	service := NewGrokBuildService("127.0.0.1:18100", nil, nil)
	service.httpClient = &http.Client{Transport: grokRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.RawQuery == "format=credits" {
			return grokHTTPJSON(200, `{"creditUsagePercent":40}`), nil
		}
		return grokHTTPJSON(503, `{}`), nil
	})}
	updated, err := service.fetchGrokQuota(t.Context(), grokOAuthAccount{AuthEntry: map[string]any{"key": "access"}})
	if err != nil {
		t.Fatalf("部分成功不应整体失败: %v", err)
	}
	if updated.Quota.WeeklyRemainingPercent == nil || *updated.Quota.WeeklyRemainingPercent != 60 {
		t.Fatalf("周额度 = %#v", updated.Quota.WeeklyRemainingPercent)
	}
	if updated.Quota.LastError == "" || updated.Quota.MonthlyRemainingPercent != nil {
		t.Fatalf("部分失败状态错误: %#v", updated.Quota)
	}
}

func TestGrokOAuthAccountDTODoesNotExposeTokens(t *testing.T) {
	account := grokOAuthAccount{
		ID: "id", Name: "name", Source: "test", CreatedAt: "now", UpdatedAt: "now",
		AuthEntry: map[string]any{"key": "secret-access", "refresh_token": "secret-refresh"},
	}
	encoded, err := json.Marshal(grokOAuthAccountDTO(account, ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-access") || strings.Contains(string(encoded), "secret-refresh") {
		t.Fatalf("DTO 泄露 Token: %s", encoded)
	}
}

func grokHTTPJSON(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func grokTestJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "none"})
	payload, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
