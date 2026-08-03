package services

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	oauthAccountStoreVersion = 1
	oauthIndexFileName       = "oauth-accounts.json"
	oauthSecretFileName      = "oauth-credentials.json"
	oauthCLIStateFileName    = "oauth-cli-state.json"

	claudeOAuthAuthorizeURL   = "https://claude.com/cai/oauth/authorize"
	claudeOAuthManualRedirect = "https://platform.claude.com/oauth/code/callback"
	claudeOAuthTokenURL       = "https://platform.claude.com/v1/oauth/token"
	claudeOAuthProfileURL     = "https://api.anthropic.com/api/oauth/profile"
	claudeOAuthUsageURL       = "https://api.anthropic.com/api/oauth/usage"
	claudeOAuthClientID       = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeOAuthBetaHeader     = "oauth-2025-04-20"

	codexOAuthAuthorizeURL     = "https://auth.openai.com/oauth/authorize"
	codexOAuthTokenURL         = "https://auth.openai.com/oauth/token"
	codexOAuthClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexDeviceUserCodeURL     = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDeviceTokenURL        = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexDeviceVerificationURL = "https://auth.openai.com/codex/device"
	codexDeviceRedirectURL     = "https://auth.openai.com/deviceauth/callback"
	codexOAuthRedirectURL      = "http://localhost:1455/auth/callback"
	codexUsageURL              = "https://chatgpt.com/backend-api/wham/usage"

	oauthLoginTTL       = 10 * time.Minute
	oauthRefreshLead    = 5 * time.Minute
	oauthDeviceCodeTTL  = 15 * time.Minute
	oauthHTTPTimeout    = 25 * time.Second
	oauthMaxResponseLen = 2 << 20
	oauthMaxImportBytes = 8 << 20
	oauthMaxImportItems = 1000
)

type OAuthPlatform string

const (
	OAuthPlatformClaude OAuthPlatform = "claude"
	OAuthPlatformCodex  OAuthPlatform = "codex"
)

type OAuthAuthMode string

const (
	OAuthAuthModeClaudeCode OAuthAuthMode = "claude_code_oauth"
	OAuthAuthModeCodex      OAuthAuthMode = "codex_oauth"
)

type OAuthAccountStatus string

const (
	OAuthAccountActive       OAuthAccountStatus = "active"
	OAuthAccountExpired      OAuthAccountStatus = "expired"
	OAuthAccountRefreshError OAuthAccountStatus = "refresh_failed"
	OAuthAccountRevoked      OAuthAccountStatus = "revoked"
	OAuthAccountDisabled     OAuthAccountStatus = "disabled"
)

// OAuthQuotaSnapshot 保存最后一次成功读取的额度，不保存上游原始响应。
// 失败信息单独放在 OAuthAccountSummary.QuotaError，避免把一次失败伪装成 0。
type OAuthQuotaSnapshot struct {
	ShortWindowPercent int    `json:"shortWindowPercent,omitempty"`
	WeeklyPercent      int    `json:"weeklyPercent,omitempty"`
	MonthlyPercent     int    `json:"monthlyPercent,omitempty"`
	ShortWindowResetAt int64  `json:"shortWindowResetAt,omitempty"`
	WeeklyResetAt      int64  `json:"weeklyResetAt,omitempty"`
	MonthlyResetAt     int64  `json:"monthlyResetAt,omitempty"`
	FetchedAt          string `json:"fetchedAt,omitempty"`
}

// OAuthAccountSummary 是可返回到前端的脱敏账号摘要，绝不包含任何 token 或原始 JWT。
type OAuthAccountSummary struct {
	ID                   string             `json:"id"`
	Platform             string             `json:"platform"`
	AuthMode             string             `json:"authMode"`
	DisplayName          string             `json:"displayName"`
	Email                string             `json:"email"`
	AccountID            string             `json:"accountId"`
	UserID               string             `json:"userId"`
	OrganizationID       string             `json:"organizationId"`
	OrganizationName     string             `json:"organizationName"`
	PlanType             string             `json:"planType"`
	Status               string             `json:"status"`
	AccessTokenExpiresAt int64              `json:"accessTokenExpiresAt"`
	LastRefreshAt        string             `json:"lastRefreshAt"`
	LastSuccessAt        string             `json:"lastSuccessAt"`
	RefreshError         string             `json:"refreshError"`
	QuotaError           string             `json:"quotaError"`
	Quota                OAuthQuotaSnapshot `json:"quota"`
	Source               string             `json:"source"`
	CreatedAt            string             `json:"createdAt"`
	UpdatedAt            string             `json:"updatedAt"`
}

type OAuthLoginStart struct {
	LoginID                string `json:"loginId"`
	AuthorizationURL       string `json:"authorizationUrl"`
	VerificationURL        string `json:"verificationUrl"`
	UserCode               string `json:"userCode"`
	ExpiresAt              int64  `json:"expiresAt"`
	PollIntervalSeconds    int    `json:"pollIntervalSeconds"`
	ManualCallbackRequired bool   `json:"manualCallbackRequired"`
}

type OAuthLoginStatus struct {
	LoginID string               `json:"loginId"`
	Status  string               `json:"status"`
	Message string               `json:"message"`
	Account *OAuthAccountSummary `json:"account,omitempty"`
}

type OAuthImportRequest struct {
	Platform string `json:"platform"`
	Source   string `json:"source"`
	Content  string `json:"content"`
}

type OAuthImportItem struct {
	Source   string               `json:"source"`
	Platform string               `json:"platform"`
	Action   string               `json:"action"`
	Message  string               `json:"message"`
	Warnings []string             `json:"warnings"`
	Account  *OAuthAccountSummary `json:"account,omitempty"`
}

type OAuthImportResult struct {
	Items   []OAuthImportItem `json:"items"`
	Created int               `json:"created"`
	Updated int               `json:"updated"`
	Skipped int               `json:"skipped"`
	Failed  int               `json:"failed"`
}

type oauthAccountRecord struct {
	ID                    string             `json:"id"`
	Platform              string             `json:"platform"`
	AuthMode              string             `json:"authMode"`
	DisplayName           string             `json:"displayName"`
	Email                 string             `json:"email"`
	AccountID             string             `json:"accountId"`
	UserID                string             `json:"userId"`
	OrganizationID        string             `json:"organizationId"`
	OrganizationName      string             `json:"organizationName"`
	PlanType              string             `json:"planType"`
	Status                string             `json:"status"`
	AccessTokenExpiresAt  int64              `json:"accessTokenExpiresAt"`
	LastRefreshAt         string             `json:"lastRefreshAt"`
	LastSuccessAt         string             `json:"lastSuccessAt"`
	RefreshError          string             `json:"refreshError"`
	QuotaError            string             `json:"quotaError"`
	Quota                 OAuthQuotaSnapshot `json:"quota"`
	Source                string             `json:"source"`
	CredentialFingerprint string             `json:"credentialFingerprint"`
	CreatedAt             string             `json:"createdAt"`
	UpdatedAt             string             `json:"updatedAt"`
}

type oauthSecretRecord struct {
	AccountID    string   `json:"accountId"`
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	IDToken      string   `json:"idToken"`
	TokenType    string   `json:"tokenType"`
	Scopes       []string `json:"scopes,omitempty"`
}

type oauthAccountStore struct {
	Version  int                  `json:"version"`
	Accounts []oauthAccountRecord `json:"accounts"`
}

type oauthSecretStore struct {
	Version     int                          `json:"version"`
	Credentials map[string]oauthSecretRecord `json:"credentials"`
}

type oauthCLIFileState struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	Original     []byte `json:"original,omitempty"`
	InjectedHash string `json:"injectedHash"`
}

type oauthCLIState struct {
	Claude []oauthCLIFileState `json:"claude,omitempty"`
	Codex  []oauthCLIFileState `json:"codex,omitempty"`
}

type oauthPendingLogin struct {
	LoginID          string
	Platform         OAuthPlatform
	State            string
	CodeVerifier     string
	DeviceAuthID     string
	DeviceUserCode   string
	ExpiresAt        time.Time
	PollInterval     int
	AuthorizationURL string
}

type oauthNormalizedItem struct {
	Platform             OAuthPlatform
	AuthMode             OAuthAuthMode
	DisplayName          string
	Email                string
	AccountID            string
	UserID               string
	OrganizationID       string
	OrganizationName     string
	PlanType             string
	AccessToken          string
	RefreshToken         string
	IDToken              string
	Scopes               []string
	AccessTokenExpiresAt int64
	Source               string
	Warnings             []string
}

// OAuthAccountService owns OAuth account metadata and secrets separately from Provider.
// It intentionally keeps the portable file backend so the desktop app does not require
// an OS-specific keychain to move between machines.
type OAuthAccountService struct {
	mu           sync.Mutex
	refreshLocks map[string]*sync.Mutex
	pending      map[string]oauthPendingLogin
	client       *http.Client
	now          func() time.Time
	configDir    string
}

func NewOAuthAccountService() *OAuthAccountService {
	return &OAuthAccountService{
		refreshLocks: make(map[string]*sync.Mutex),
		pending:      make(map[string]oauthPendingLogin),
		client:       &http.Client{Timeout: oauthHTTPTimeout},
		now:          time.Now,
	}
}

func (s *OAuthAccountService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, err := s.loadStoresLocked()
	return err
}

func (s *OAuthAccountService) Stop() error { return nil }

func (s *OAuthAccountService) ListAccounts(platform string) ([]OAuthAccountSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, _, err := s.loadStoresLocked()
	if err != nil {
		return nil, err
	}
	result := make([]OAuthAccountSummary, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if platform != "" && !strings.EqualFold(account.Platform, strings.TrimSpace(platform)) {
			continue
		}
		result = append(result, account.summary())
	}
	return result, nil
}

func (s *OAuthAccountService) DeleteAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err := s.loadStoresLocked()
	if err != nil {
		return err
	}
	index := -1
	for i := range store.Accounts {
		if store.Accounts[i].ID == strings.TrimSpace(id) {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("OAuth 账号不存在: %s", strings.TrimSpace(id))
	}
	for _, provider := range []string{"claude", "codex"} {
		refs, err := providersReferencingCredential(provider, id)
		if err != nil {
			return fmt.Errorf("检查 Provider 引用失败: %w", err)
		}
		if len(refs) > 0 {
			return fmt.Errorf("账号仍被 Provider 引用: %s", strings.Join(refs, ", "))
		}
	}
	store.Accounts = append(store.Accounts[:index], store.Accounts[index+1:]...)
	delete(secrets.Credentials, id)
	return s.saveStoresLocked(store, secrets)
}

// StartClaudeOAuth starts a manual callback PKCE session. The callback URL is intentionally
// returned for copy/paste because a desktop Wails app cannot assume a free local callback port.
func (s *OAuthAccountService) StartClaudeOAuth() (*OAuthLoginStart, error) {
	state, err := randomOAuthString(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomOAuthString(48)
	if err != nil {
		return nil, err
	}
	loginID, err := randomOAuthString(18)
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(oauthLoginTTL)
	authURL := buildOAuthURL(claudeOAuthAuthorizeURL, map[string]string{
		"response_type":         "code",
		"client_id":             claudeOAuthClientID,
		"redirect_uri":          claudeOAuthManualRedirect,
		"scope":                 "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload",
		"code_challenge":        pkceChallenge(verifier),
		"code_challenge_method": "S256",
		"state":                 state,
	})
	s.mu.Lock()
	s.pending[loginID] = oauthPendingLogin{LoginID: loginID, Platform: OAuthPlatformClaude, State: state, CodeVerifier: verifier, ExpiresAt: expires, AuthorizationURL: authURL}
	s.prunePendingLocked()
	s.mu.Unlock()
	return &OAuthLoginStart{LoginID: loginID, AuthorizationURL: authURL, ExpiresAt: expires.UnixMilli(), ManualCallbackRequired: true}, nil
}

func (s *OAuthAccountService) CompleteClaudeOAuth(loginID, callback string) (*OAuthAccountSummary, error) {
	pending, err := s.takePending(loginID, OAuthPlatformClaude)
	if err != nil {
		return nil, err
	}
	code, callbackState, err := parseOAuthCallback(callback)
	if err != nil {
		return nil, err
	}
	if callbackState == "" || callbackState != pending.State {
		return nil, errors.New("Claude OAuth state 校验失败")
	}
	tokens, err := s.exchangeOAuthCode(OAuthPlatformClaude, code, pending.CodeVerifier, claudeOAuthManualRedirect, pending.State)
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		return nil, errors.New("Claude OAuth 响应缺少 refresh_token")
	}
	profile, profileErr := s.fetchProfile(tokens.AccessToken, OAuthPlatformClaude, "")
	if profileErr != nil {
		return nil, profileErr
	}
	item := normalizedClaudeFromTokens(tokens, profile, "oauth")
	return s.upsertNormalized(item)
}

// StartCodexOAuth starts the official browser PKCE flow. The callback URL can be pasted into
// CompleteCodexOAuth; no listener is opened implicitly.
func (s *OAuthAccountService) StartCodexOAuth() (*OAuthLoginStart, error) {
	state, err := randomOAuthString(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomOAuthString(48)
	if err != nil {
		return nil, err
	}
	loginID, err := randomOAuthString(18)
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(oauthLoginTTL)
	authURL := buildOAuthURL(codexOAuthAuthorizeURL, map[string]string{
		"response_type":              "code",
		"client_id":                  codexOAuthClientID,
		"redirect_uri":               codexOAuthRedirectURL,
		"scope":                      "openid profile email offline_access api.connectors.read api.connectors.invoke",
		"code_challenge":             pkceChallenge(verifier),
		"code_challenge_method":      "S256",
		"state":                      state,
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 "codex_cli_rs",
	})
	s.mu.Lock()
	s.pending[loginID] = oauthPendingLogin{LoginID: loginID, Platform: OAuthPlatformCodex, State: state, CodeVerifier: verifier, ExpiresAt: expires, AuthorizationURL: authURL}
	s.prunePendingLocked()
	s.mu.Unlock()
	return &OAuthLoginStart{LoginID: loginID, AuthorizationURL: authURL, ExpiresAt: expires.UnixMilli(), ManualCallbackRequired: true}, nil
}

func (s *OAuthAccountService) CompleteCodexOAuth(loginID, callback string) (*OAuthAccountSummary, error) {
	pending, err := s.takePending(loginID, OAuthPlatformCodex)
	if err != nil {
		return nil, err
	}
	code, callbackState, err := parseOAuthCallback(callback)
	if err != nil {
		return nil, err
	}
	if callbackState == "" || callbackState != pending.State {
		return nil, errors.New("Codex OAuth state 校验失败")
	}
	tokens, err := s.exchangeOAuthCode(OAuthPlatformCodex, code, pending.CodeVerifier, codexOAuthRedirectURL, "")
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		return nil, errors.New("Codex OAuth 响应缺少 refresh_token")
	}
	item := normalizedCodexFromTokens(tokens, "oauth")
	return s.upsertNormalized(item)
}

// StartCodexDeviceCode starts the OpenAI device flow used by official Codex CLI.
func (s *OAuthAccountService) StartCodexDeviceCode() (*OAuthLoginStart, error) {
	body, err := json.Marshal(map[string]string{"client_id": codexOAuthClientID})
	if err != nil {
		return nil, err
	}
	response, err := s.doJSON(http.MethodPost, codexDeviceUserCodeURL, body, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("Codex Device Code 请求失败: %w", err)
	}
	deviceID := stringValue(response, "device_auth_id")
	userCode := stringValue(response, "user_code", "usercode")
	if deviceID == "" || userCode == "" {
		return nil, errors.New("Codex Device Code 响应缺少 device_auth_id 或 user_code")
	}
	interval := intValue(response, "interval")
	if interval < 1 {
		interval = 5
	}
	expiresSeconds := intValue(response, "expires_in")
	if expiresSeconds < 1 {
		expiresSeconds = int(oauthDeviceCodeTTL / time.Second)
	}
	loginID, err := randomOAuthString(18)
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(time.Duration(expiresSeconds) * time.Second)
	s.mu.Lock()
	s.pending[loginID] = oauthPendingLogin{LoginID: loginID, Platform: OAuthPlatformCodex, DeviceAuthID: deviceID, DeviceUserCode: userCode, ExpiresAt: expires, PollInterval: interval, AuthorizationURL: codexDeviceVerificationURL}
	s.prunePendingLocked()
	s.mu.Unlock()
	return &OAuthLoginStart{LoginID: loginID, VerificationURL: codexDeviceVerificationURL, UserCode: userCode, ExpiresAt: expires.UnixMilli(), PollIntervalSeconds: interval}, nil
}

func (s *OAuthAccountService) PollCodexDeviceCode(loginID string) (*OAuthLoginStatus, error) {
	s.mu.Lock()
	pending, ok := s.pending[strings.TrimSpace(loginID)]
	s.mu.Unlock()
	if !ok || pending.Platform != OAuthPlatformCodex || pending.DeviceAuthID == "" {
		return nil, errors.New("Codex Device Code 登录会话不存在")
	}
	if !s.now().Before(pending.ExpiresAt) {
		s.mu.Lock()
		delete(s.pending, loginID)
		s.mu.Unlock()
		return &OAuthLoginStatus{LoginID: loginID, Status: "expired", Message: "Device Code 已过期"}, nil
	}
	body, err := json.Marshal(map[string]string{"device_auth_id": pending.DeviceAuthID, "user_code": pending.DeviceUserCode})
	if err != nil {
		return nil, err
	}
	response, status, err := s.doJSONWithStatus(http.MethodPost, codexDeviceTokenURL, body, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		if status == http.StatusForbidden || status == http.StatusNotFound {
			return &OAuthLoginStatus{LoginID: loginID, Status: "pending", Message: "等待用户完成授权"}, nil
		}
		if status == http.StatusGone {
			return &OAuthLoginStatus{LoginID: loginID, Status: "expired", Message: "Device Code 已过期"}, nil
		}
		return nil, fmt.Errorf("Codex Device Code 轮询失败: %w", err)
	}
	authorizationCode := stringValue(response, "authorization_code")
	codeVerifier := stringValue(response, "code_verifier")
	if authorizationCode == "" || codeVerifier == "" {
		return nil, errors.New("Codex Device Code 响应缺少 authorization_code 或 code_verifier")
	}
	tokens, err := s.exchangeOAuthCode(OAuthPlatformCodex, authorizationCode, codeVerifier, codexDeviceRedirectURL, "")
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		return nil, errors.New("Codex OAuth 响应缺少 refresh_token")
	}
	account, err := s.upsertNormalized(normalizedCodexFromTokens(tokens, "device_code"))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.pending, loginID)
	s.mu.Unlock()
	return &OAuthLoginStatus{LoginID: loginID, Status: "authorized", Account: account}, nil
}

func (s *OAuthAccountService) CancelOAuthLogin(loginID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pending[strings.TrimSpace(loginID)]; !ok {
		return errors.New("OAuth 登录会话不存在")
	}
	delete(s.pending, strings.TrimSpace(loginID))
	return nil
}

func (s *OAuthAccountService) RefreshAccount(id string) (*OAuthAccountSummary, error) {
	if _, _, err := s.resolveAccessToken(id, true); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store, _, err := s.loadStoresLocked()
	if err != nil {
		return nil, err
	}
	for _, account := range store.Accounts {
		if account.ID == id {
			return account.summaryPtr(), nil
		}
	}
	return nil, fmt.Errorf("OAuth 账号不存在: %s", id)
}

func (s *OAuthAccountService) RefreshQuota(id string) (*OAuthAccountSummary, error) {
	_, token, err := s.resolveAccessToken(id, false)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	store, secrets, loadErr := s.loadStoresLocked()
	var account *oauthAccountRecord
	if loadErr == nil {
		for i := range store.Accounts {
			if store.Accounts[i].ID == id {
				account = &store.Accounts[i]
				break
			}
		}
	}
	s.mu.Unlock()
	if loadErr != nil {
		return nil, loadErr
	}
	if account == nil {
		return nil, fmt.Errorf("OAuth 账号不存在: %s", id)
	}
	endpoint := claudeOAuthUsageURL
	headers := map[string]string{}
	if account.Platform == string(OAuthPlatformCodex) {
		endpoint = codexUsageURL
		headers["ChatGPT-Account-Id"] = account.AccountID
		headers["User-Agent"] = "codex_cli_rs/0.1.0"
	}
	headers["Authorization"] = "Bearer " + token
	response, status, requestErr := s.getJSONWithStatus(endpoint, headers)
	if requestErr != nil {
		message := requestErr.Error()
		if status > 0 {
			message = fmt.Sprintf("额度接口返回 HTTP %d", status)
		}
		s.recordQuotaError(id, sanitizeOAuthError(message))
		return nil, fmt.Errorf("额度请求失败: %w", requestErr)
	}
	quota := parseOAuthQuota(response, s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err = s.loadStoresLocked()
	if err != nil {
		return nil, err
	}
	for i := range store.Accounts {
		if store.Accounts[i].ID == id {
			store.Accounts[i].Quota = quota
			store.Accounts[i].QuotaError = ""
			store.Accounts[i].LastSuccessAt = s.now().UTC().Format(time.RFC3339)
			store.Accounts[i].UpdatedAt = s.now().UTC().Format(time.RFC3339)
			if err := s.saveStoresLocked(store, secrets); err != nil {
				return nil, err
			}
			return store.Accounts[i].summaryPtr(), nil
		}
	}
	return nil, fmt.Errorf("OAuth 账号不存在: %s", id)
}

func (s *OAuthAccountService) ImportJSON(request OAuthImportRequest) (*OAuthImportResult, error) {
	platform := normalizeOAuthPlatform(request.Platform)
	if platform == "" {
		return nil, errors.New("只支持导入 claude 或 codex OAuth 账号")
	}
	content := strings.TrimSpace(request.Content)
	if content == "" {
		return nil, errors.New("导入内容不能为空")
	}
	if len([]byte(content)) > oauthMaxImportBytes {
		return nil, fmt.Errorf("导入 JSON 超过 %d 字节限制", oauthMaxImportBytes)
	}
	documents, err := decodeOAuthImportDocuments(content)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0)
	for _, document := range documents {
		items = append(items, flattenOAuthImportRoot(document)...)
		if len(items) > oauthMaxImportItems {
			return nil, fmt.Errorf("导入账号条目超过 %d 条限制", oauthMaxImportItems)
		}
	}
	if len(items) == 0 {
		return nil, errors.New("导入 JSON 没有可识别的账号条目")
	}
	result := &OAuthImportResult{Items: make([]OAuthImportItem, 0, len(items))}
	for _, item := range items {
		normalized, err := normalizeOAuthImportItem(platform, request.Source, item)
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, OAuthImportItem{Source: request.Source, Platform: string(platform), Action: "failed", Message: err.Error()})
			continue
		}
		account, action, err := s.upsertNormalizedWithAction(normalized)
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, OAuthImportItem{Source: normalized.Source, Platform: string(platform), Action: "failed", Message: err.Error(), Warnings: normalized.Warnings})
			continue
		}
		switch action {
		case "created":
			result.Created++
		case "updated":
			result.Updated++
		default:
			result.Skipped++
		}
		result.Items = append(result.Items, OAuthImportItem{Source: normalized.Source, Platform: string(platform), Action: action, Message: "导入成功", Warnings: normalized.Warnings, Account: account})
	}
	return result, nil
}

// decodeOAuthImportDocuments accepts a normal JSON document, JSON arrays and
// newline-delimited JSON exports without treating the second document as an
// accidental silent discard. Every document is still parsed by encoding/json.
func decodeOAuthImportDocuments(content string) ([]any, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	documents := make([]any, 0, 1)
	for {
		var document any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if len(documents) == 0 {
				return nil, fmt.Errorf("导入 JSON 无效: %w", err)
			}
			return nil, fmt.Errorf("导入 JSON 末尾无效: %w", err)
		}
		documents = append(documents, document)
		if len(documents) > oauthMaxImportItems {
			return nil, fmt.Errorf("导入 JSON 文档超过 %d 个限制", oauthMaxImportItems)
		}
	}
	return documents, nil
}

// ImportClaudeLocal reads Claude Code's two OAuth snapshots without importing
// Desktop cookies or the browser profile.
func (s *OAuthAccountService) ImportClaudeLocal() (*OAuthImportResult, error) {
	home, err := getUserHomeDir()
	if err != nil {
		return nil, err
	}
	credentialsPath := filepath.Join(home, ".claude", ".credentials.json")
	credentials, err := readJSONObjectOrEmpty(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("读取 Claude Code credentials 失败: %w", err)
	}
	configPath := filepath.Join(home, ".claude", ".config.json")
	if _, statErr := os.Stat(configPath); errors.Is(statErr, os.ErrNotExist) {
		configPath = filepath.Join(home, ".claude.json")
	}
	config, err := readJSONObjectOrEmpty(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取 Claude Code config 失败: %w", err)
	}
	content, err := json.Marshal(map[string]any{"credentials": credentials, "config": config})
	if err != nil {
		return nil, err
	}
	return s.ImportJSON(OAuthImportRequest{Platform: string(OAuthPlatformClaude), Source: "claude_code_local", Content: string(content)})
}

// ImportCodexLocal imports only the official OAuth tokens under ~/.codex/auth.json.
// OPENAI_API_KEY-only auth and agentIdentity are rejected by the normalizer.
func (s *OAuthAccountService) ImportCodexLocal() (*OAuthImportResult, error) {
	home, err := getUserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".codex", "auth.json")
	payload, err := readJSONObjectOrEmpty(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Codex auth.json 失败: %w", err)
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return s.ImportJSON(OAuthImportRequest{Platform: string(OAuthPlatformCodex), Source: "codex_local", Content: string(content)})
}

// ResolveProvider resolves an OAuth credential for exactly one relay attempt. The returned
// Provider is a transient copy; access tokens are never written back to Provider storage.
func (s *OAuthAccountService) resolveProvider(provider Provider, platform string) (Provider, map[string]string, error) {
	if !strings.EqualFold(strings.TrimSpace(provider.CredentialType), "oauth") {
		return provider, nil, nil
	}
	if strings.TrimSpace(provider.CredentialRef) == "" {
		return provider, nil, errors.New("OAuth Provider 缺少 credentialRef")
	}
	account, token, err := s.resolveAccessToken(provider.CredentialRef, false)
	if err != nil {
		return provider, nil, err
	}
	if !strings.EqualFold(account.Platform, strings.TrimSpace(platform)) {
		return provider, nil, fmt.Errorf("OAuth 账号平台 %q 与 Provider 平台 %q 不匹配", account.Platform, platform)
	}
	provider.APIKey = token
	provider.AuthScheme = "bearer"
	provider.ConnectivityAuthType = "bearer"
	headers := make(map[string]string)
	switch account.Platform {
	case string(OAuthPlatformClaude):
		// Claude OAuth access tokens require the OAuth beta marker on inference
		// requests as well as on profile and quota requests.
		headers["Anthropic-Beta"] = claudeOAuthBetaHeader
	case string(OAuthPlatformCodex):
		if account.AccountID != "" {
			headers["ChatGPT-Account-Id"] = account.AccountID
		}
	}
	return provider, headers, nil
}

// ResolveProviderCredential is the internal relay bridge. It is a package function
// rather than a service method so Wails cannot expose a transient access token result.
func ResolveProviderCredential(service *OAuthAccountService, provider Provider, platform string) (Provider, map[string]string, error) {
	if service == nil {
		return provider, nil, nil
	}
	return service.resolveProvider(provider, platform)
}

// ApplyOAuthCredentialHeaders applies only headers generated by the account resolver.
// Keeping this separate prevents arbitrary Provider or client headers from setting identity headers.
func ApplyOAuthCredentialHeaders(headers map[string]string, credentialHeaders map[string]string) error {
	for key, value := range credentialHeaders {
		switch {
		case strings.EqualFold(key, "ChatGPT-Account-Id"):
			if err := validateHeaderNameAndValue(key, value); err != nil {
				return err
			}
			setHeader(headers, key, value)
		case strings.EqualFold(key, "Anthropic-Beta"):
			if err := validateHeaderNameAndValue(key, value); err != nil {
				return err
			}
			mergeCommaSeparatedHeader(headers, key, value)
		default:
			return fmt.Errorf("OAuth 解析器不允许注入 Header: %s", key)
		}
	}
	return nil
}

// OAuthCredentialLogID returns a stable pseudonymous ID for diagnostics. It is
// deliberately a second hash of credentialRef, never an access/refresh token.
func OAuthCredentialLogID(credentialRef string) string {
	credentialRef = strings.TrimSpace(credentialRef)
	if credentialRef == "" {
		return ""
	}
	return oauthDigest([]byte(credentialRef))[:16]
}

func OAuthCredentialLogMode(platform string) string {
	switch normalizeOAuthPlatform(platform) {
	case OAuthPlatformClaude:
		return string(OAuthAuthModeClaudeCode)
	case OAuthPlatformCodex:
		return string(OAuthAuthModeCodex)
	default:
		return "oauth"
	}
}

func (s *OAuthAccountService) ApplyClaudeAccount(id string) error {
	account, secret, err := s.loadAccountAndSecret(id, OAuthPlatformClaude)
	if err != nil {
		return err
	}
	if _, _, err := s.resolveAccessToken(id, true); err != nil {
		return err
	}
	account, secret, err = s.loadAccountAndSecret(id, OAuthPlatformClaude)
	if err != nil {
		return err
	}
	home, err := getUserHomeDir()
	if err != nil {
		return err
	}
	credentialsPath := filepath.Join(home, ".claude", ".credentials.json")
	configPath := filepath.Join(home, ".claude", ".config.json")
	if _, statErr := os.Stat(configPath); errors.Is(statErr, os.ErrNotExist) {
		// Claude Code's default global profile is ~/.claude.json; custom config
		// directories may instead contain ~/.claude/.config.json.
		configPath = filepath.Join(home, ".claude.json")
	}
	credentialPayload, err := readJSONObjectOrEmpty(credentialsPath)
	if err != nil {
		return fmt.Errorf("读取 Claude credentials 失败: %w", err)
	}
	configPayload, err := readJSONObjectOrEmpty(configPath)
	if err != nil {
		return fmt.Errorf("读取 Claude config 失败: %w", err)
	}
	credentialPayload["claudeAiOauth"] = map[string]any{
		"accessToken":  secret.AccessToken,
		"refreshToken": secret.RefreshToken,
		"expiresAt":    account.AccessTokenExpiresAt,
		"scopes":       secret.Scopes,
	}
	configPayload["oauthAccount"] = map[string]any{
		"emailAddress":     account.Email,
		"accountUuid":      account.AccountID,
		"organizationUuid": account.OrganizationID,
		"organizationName": account.OrganizationName,
	}
	return s.applyCLIFiles("claude", []oauthCLIFileInput{{Path: credentialsPath, Payload: credentialPayload}, {Path: configPath, Payload: configPayload}})
}

func (s *OAuthAccountService) ClearClaudeAccount() error { return s.restoreCLIFiles("claude") }

func (s *OAuthAccountService) ApplyCodexAccount(id string) error {
	account, secret, err := s.loadAccountAndSecret(id, OAuthPlatformCodex)
	if err != nil {
		return err
	}
	if _, _, err := s.resolveAccessToken(id, true); err != nil {
		return err
	}
	account, secret, err = s.loadAccountAndSecret(id, OAuthPlatformCodex)
	if err != nil {
		return err
	}
	home, err := getUserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".codex", "auth.json")
	payload, err := readJSONObjectOrEmpty(path)
	if err != nil {
		return fmt.Errorf("读取 Codex auth.json 失败: %w", err)
	}
	payload["auth_mode"] = "chatgpt"
	payload["tokens"] = map[string]any{
		"access_token":  secret.AccessToken,
		"refresh_token": secret.RefreshToken,
		"id_token":      secret.IDToken,
		"account_id":    account.AccountID,
	}
	payload["last_refresh"] = account.LastRefreshAt
	return s.applyCLIFiles("codex", []oauthCLIFileInput{{Path: path, Payload: payload}})
}

func (s *OAuthAccountService) ClearCodexAccount() error { return s.restoreCLIFiles("codex") }

type oauthCLIFileInput struct {
	Path    string
	Payload map[string]any
}

func (s *OAuthAccountService) applyCLIFiles(platform string, inputs []oauthCLIFileInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadCLIStateLocked()
	if err != nil {
		return err
	}
	files := cliStateForPlatform(&state, platform)
	byPath := make(map[string]oauthCLIFileState, len(*files))
	for _, file := range *files {
		byPath[file.Path] = file
	}
	type preparedCLIWrite struct {
		path    string
		current []byte
		existed bool
		data    []byte
	}
	writes := make([]preparedCLIWrite, 0, len(inputs))
	prepared := make([]oauthCLIFileState, 0, len(inputs))
	for _, input := range inputs {
		current, readErr := os.ReadFile(input.Path)
		existed := readErr == nil
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nilSafeCLIError(input.Path, readErr)
		}
		if previous, ok := byPath[input.Path]; ok && oauthDigest(current) != previous.InjectedHash {
			return fmt.Errorf("CLI 文件已被外部修改，拒绝覆盖: %s", input.Path)
		}
		original := current
		if previous, ok := byPath[input.Path]; ok {
			original = previous.Original
			existed = previous.Existed
		}
		data, marshalErr := json.MarshalIndent(input.Payload, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		writes = append(writes, preparedCLIWrite{path: input.Path, current: current, existed: readErr == nil, data: data})
		prepared = append(prepared, oauthCLIFileState{Path: input.Path, Existed: existed, Original: original, InjectedHash: oauthDigest(data)})
	}
	written := make([]preparedCLIWrite, 0, len(writes))
	for _, write := range writes {
		if err := AtomicWriteBytes(write.path, write.data); err != nil {
			for _, rollback := range written {
				if rollback.existed {
					_ = AtomicWriteBytes(rollback.path, rollback.current)
				} else {
					_ = os.Remove(rollback.path)
				}
			}
			return err
		}
		written = append(written, write)
	}
	*files = prepared
	if err := s.saveCLIStateLocked(state); err != nil {
		for _, rollback := range written {
			if rollback.existed {
				_ = AtomicWriteBytes(rollback.path, rollback.current)
			} else {
				_ = os.Remove(rollback.path)
			}
		}
		return err
	}
	return nil
}

func (s *OAuthAccountService) restoreCLIFiles(platform string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadCLIStateLocked()
	if err != nil {
		return err
	}
	files := cliStateForPlatform(&state, platform)
	for _, file := range *files {
		current, readErr := os.ReadFile(file.Path)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if oauthDigest(current) != file.InjectedHash {
			return fmt.Errorf("CLI 文件已被外部修改，拒绝恢复: %s", file.Path)
		}
	}
	for _, file := range *files {
		if file.Existed {
			if err := AtomicWriteBytes(file.Path, file.Original); err != nil {
				return err
			}
		} else if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	*files = nil
	return s.saveCLIStateLocked(state)
}

func (s *OAuthAccountService) loadAccountAndSecret(id string, platform OAuthPlatform) (oauthAccountRecord, oauthSecretRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err := s.loadStoresLocked()
	if err != nil {
		return oauthAccountRecord{}, oauthSecretRecord{}, err
	}
	for _, account := range store.Accounts {
		if account.ID == id {
			if !strings.EqualFold(account.Platform, string(platform)) {
				return oauthAccountRecord{}, oauthSecretRecord{}, errors.New("账号平台不匹配")
			}
			secret, ok := secrets.Credentials[id]
			if !ok {
				return oauthAccountRecord{}, oauthSecretRecord{}, errors.New("账号凭据文件中缺少账号")
			}
			return account, secret, nil
		}
	}
	return oauthAccountRecord{}, oauthSecretRecord{}, fmt.Errorf("OAuth 账号不存在: %s", id)
}

func (s *OAuthAccountService) resolveAccessToken(id string, forceRefresh bool) (oauthAccountRecord, string, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	store, secrets, err := s.loadStoresLocked()
	if err != nil {
		s.mu.Unlock()
		return oauthAccountRecord{}, "", err
	}
	account, secret, err := findOAuthAccount(store, secrets, id)
	if err != nil {
		s.mu.Unlock()
		return oauthAccountRecord{}, "", err
	}
	lock := s.refreshLockLocked(id)
	s.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()

	s.mu.Lock()
	store, secrets, err = s.loadStoresLocked()
	if err != nil {
		s.mu.Unlock()
		return oauthAccountRecord{}, "", err
	}
	account, secret, err = findOAuthAccount(store, secrets, id)
	if err != nil {
		s.mu.Unlock()
		return oauthAccountRecord{}, "", err
	}
	if !forceRefresh && secret.AccessToken != "" && account.AccessTokenExpiresAt > s.now().Add(oauthRefreshLead).UnixMilli() {
		s.mu.Unlock()
		return account, secret.AccessToken, nil
	}
	if !forceRefresh && secret.AccessToken != "" && account.AccessTokenExpiresAt == 0 && secret.RefreshToken == "" {
		s.mu.Unlock()
		return account, secret.AccessToken, nil
	}
	if secret.RefreshToken == "" {
		s.mu.Unlock()
		s.markRefreshError(id, "账号缺少 refresh token")
		return account, "", errors.New("OAuth 账号缺少 refresh token")
	}
	s.mu.Unlock()

	refreshed, err := s.refreshTokens(account, secret)
	if err != nil {
		s.markRefreshError(id, err.Error())
		return account, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err = s.loadStoresLocked()
	if err != nil {
		return account, "", err
	}
	for i := range store.Accounts {
		if store.Accounts[i].ID != id {
			continue
		}
		store.Accounts[i] = mergeRefreshedAccount(store.Accounts[i], refreshed.Account, s.now())
		secrets.Credentials[id] = refreshed.Secret
		if err := s.saveStoresLocked(store, secrets); err != nil {
			return account, "", err
		}
		return store.Accounts[i], refreshed.Secret.AccessToken, nil
	}
	return account, "", fmt.Errorf("OAuth 账号不存在: %s", id)
}

type refreshedOAuthTokens struct {
	Account oauthAccountRecord
	Secret  oauthSecretRecord
}

func (s *OAuthAccountService) refreshTokens(account oauthAccountRecord, secret oauthSecretRecord) (refreshedOAuthTokens, error) {
	var response map[string]any
	var err error
	if account.Platform == string(OAuthPlatformClaude) {
		body, _ := json.Marshal(map[string]string{"grant_type": "refresh_token", "refresh_token": secret.RefreshToken, "client_id": claudeOAuthClientID})
		response, err = s.doJSON(http.MethodPost, claudeOAuthTokenURL, body, map[string]string{"Content-Type": "application/json"})
	} else {
		form := url.Values{"grant_type": {"refresh_token"}, "client_id": {codexOAuthClientID}, "refresh_token": {secret.RefreshToken}}
		response, err = s.doForm(codexOAuthTokenURL, form)
	}
	if err != nil {
		return refreshedOAuthTokens{}, err
	}
	access := stringValue(response, "access_token")
	if access == "" {
		return refreshedOAuthTokens{}, errors.New("刷新响应缺少 access_token")
	}
	refresh := stringValue(response, "refresh_token")
	if refresh == "" {
		refresh = secret.RefreshToken
	}
	idToken := stringValue(response, "id_token")
	if idToken == "" {
		idToken = secret.IDToken
	}
	expiresAt := parseOAuthTimestamp(response["expires_at"])
	if expiresAt == 0 {
		expiresAt = parseOAuthTimestamp(response["expiresAt"])
	}
	if expiresAt == 0 {
		if expires := intValue(response, "expires_in"); expires > 0 {
			expiresAt = s.now().Add(time.Duration(expires) * time.Second).UnixMilli()
		}
	}
	if expiresAt == 0 {
		expiresAt = jwtExpiryMillis(access)
	}
	if expiresAt == 0 && account.Platform == string(OAuthPlatformClaude) {
		expiresAt = s.now().Add(time.Hour).UnixMilli()
	}
	updatedAccount := account
	updatedAccount.AccessTokenExpiresAt = expiresAt
	updatedAccount.Status = string(OAuthAccountActive)
	updatedAccount.RefreshError = ""
	updatedAccount.LastRefreshAt = s.now().UTC().Format(time.RFC3339)
	updatedAccount.UpdatedAt = updatedAccount.LastRefreshAt
	if account.Platform == string(OAuthPlatformCodex) {
		mergeCodexClaims(&updatedAccount, idToken)
	}
	if account.Platform == string(OAuthPlatformClaude) {
		profile, profileErr := s.fetchProfile(access, OAuthPlatformClaude, account.Email)
		if profileErr == nil {
			mergeClaudeProfile(&updatedAccount, profile)
		}
	}
	return refreshedOAuthTokens{Account: updatedAccount, Secret: oauthSecretRecord{AccountID: account.ID, AccessToken: access, RefreshToken: refresh, IDToken: idToken, TokenType: "Bearer", Scopes: secret.Scopes}}, nil
}

func (s *OAuthAccountService) exchangeOAuthCode(platform OAuthPlatform, code, verifier, redirect, state string) (oauthTokenResponse, error) {
	var response map[string]any
	var err error
	if platform == OAuthPlatformClaude {
		payload := map[string]string{"grant_type": "authorization_code", "client_id": claudeOAuthClientID, "code": code, "redirect_uri": redirect, "code_verifier": verifier}
		if state != "" {
			payload["state"] = state
		}
		body, _ := json.Marshal(payload)
		response, err = s.doJSON(http.MethodPost, claudeOAuthTokenURL, body, map[string]string{"Content-Type": "application/json"})
	} else {
		form := url.Values{"grant_type": {"authorization_code"}, "client_id": {codexOAuthClientID}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
		response, err = s.doForm(codexOAuthTokenURL, form)
	}
	if err != nil {
		return oauthTokenResponse{}, err
	}
	access := stringValue(response, "access_token")
	if access == "" {
		return oauthTokenResponse{}, errors.New("OAuth token 响应缺少 access_token")
	}
	expires := parseOAuthTimestamp(response["expires_at"])
	if expires == 0 {
		expires = parseOAuthTimestamp(response["expiresAt"])
	}
	if expires == 0 {
		if n := intValue(response, "expires_in"); n > 0 {
			expires = s.now().Add(time.Duration(n) * time.Second).UnixMilli()
		}
	}
	if expires == 0 {
		expires = jwtExpiryMillis(access)
	}
	if expires == 0 && platform == OAuthPlatformClaude {
		expires = s.now().Add(time.Hour).UnixMilli()
	}
	return oauthTokenResponse{AccessToken: access, RefreshToken: stringValue(response, "refresh_token"), IDToken: stringValue(response, "id_token"), ExpiresAt: expires, Scopes: splitScopes(stringValue(response, "scope"))}, nil
}

type oauthTokenResponse struct {
	AccessToken, RefreshToken, IDToken string
	ExpiresAt                          int64
	Scopes                             []string
}

func (s *OAuthAccountService) fetchProfile(access string, platform OAuthPlatform, emailHint string) (map[string]any, error) {
	if platform != OAuthPlatformClaude {
		return map[string]any{"email": emailHint}, nil
	}
	response, _, err := s.getJSONWithStatus(claudeOAuthProfileURL, map[string]string{"Authorization": "Bearer " + access, "anthropic-beta": claudeOAuthBetaHeader})
	if err != nil {
		return nil, fmt.Errorf("读取 Claude OAuth profile 失败: %w", err)
	}
	return response, nil
}

func normalizedClaudeFromTokens(tokens oauthTokenResponse, profile map[string]any, source string) oauthNormalizedItem {
	item := oauthNormalizedItem{Platform: OAuthPlatformClaude, AuthMode: OAuthAuthModeClaudeCode, DisplayName: stringValue(profile, "name", "display_name"), Email: stringValue(profile, "email", "emailAddress"), AccountID: nestedString(profile, []string{"account", "uuid"}, []string{"account", "id"}, []string{"accountUuid"}), OrganizationID: nestedString(profile, []string{"organization", "uuid"}, []string{"organizationUuid"}), OrganizationName: nestedString(profile, []string{"organization", "name"}, []string{"organizationName"}), PlanType: stringValue(profile, "subscription_type", "subscriptionType", "plan_type"), AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, IDToken: tokens.IDToken, AccessTokenExpiresAt: tokens.ExpiresAt, Scopes: tokens.Scopes, Source: source}
	if item.DisplayName == "" {
		item.DisplayName = item.Email
	}
	if item.AccessTokenExpiresAt == 0 {
		item.AccessTokenExpiresAt = jwtExpiryMillis(tokens.AccessToken)
	}
	return item
}

func normalizedCodexFromTokens(tokens oauthTokenResponse, source string) oauthNormalizedItem {
	item := oauthNormalizedItem{Platform: OAuthPlatformCodex, AuthMode: OAuthAuthModeCodex, AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, IDToken: tokens.IDToken, AccessTokenExpiresAt: tokens.ExpiresAt, Scopes: tokens.Scopes, Source: source}
	mergeCodexClaimsItem(&item, tokens.IDToken)
	if item.DisplayName == "" {
		item.DisplayName = item.Email
		if item.DisplayName == "" {
			item.DisplayName = item.AccountID
		}
	}
	if item.AccessTokenExpiresAt == 0 {
		item.AccessTokenExpiresAt = jwtExpiryMillis(tokens.AccessToken)
	}
	return item
}

func (s *OAuthAccountService) upsertNormalized(item oauthNormalizedItem) (*OAuthAccountSummary, error) {
	account, _, err := s.upsertNormalizedWithAction(item)
	return account, err
}

func (s *OAuthAccountService) upsertNormalizedWithAction(item oauthNormalizedItem) (*OAuthAccountSummary, string, error) {
	if item.AccessToken == "" {
		return nil, "", errors.New("账号缺少 access_token")
	}
	if item.RefreshToken == "" && (item.AccessTokenExpiresAt == 0 || item.AccessTokenExpiresAt <= s.now().UnixMilli()) {
		return nil, "", errors.New("access-only 账号缺少有效的未过期时间")
	}
	if item.Platform == OAuthPlatformCodex && item.AccountID == "" && item.UserID == "" {
		item.Warnings = append(item.Warnings, "Codex token 没有可验证的 account/user identity，仅按 token 指纹去重")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err := s.loadStoresLocked()
	if err != nil {
		return nil, "", err
	}
	match := -1
	for i := range store.Accounts {
		if !strings.EqualFold(store.Accounts[i].Platform, string(item.Platform)) {
			continue
		}
		if item.AccountID != "" && store.Accounts[i].AccountID == item.AccountID {
			if item.UserID != "" && store.Accounts[i].UserID != "" && item.UserID != store.Accounts[i].UserID {
				return nil, "", errors.New("同一 account_id 对应不同 user_id，拒绝合并")
			}
			match = i
			break
		}
		if match < 0 && store.Accounts[i].CredentialFingerprint == oauthCredentialFingerprint(item.RefreshToken, item.AccessToken) {
			match = i
		}
	}
	now := s.now().UTC().Format(time.RFC3339)
	account := oauthAccountRecord{ID: oauthAccountID(item), Platform: string(item.Platform), AuthMode: string(item.AuthMode), DisplayName: item.DisplayName, Email: item.Email, AccountID: item.AccountID, UserID: item.UserID, OrganizationID: item.OrganizationID, OrganizationName: item.OrganizationName, PlanType: item.PlanType, Status: string(OAuthAccountActive), AccessTokenExpiresAt: item.AccessTokenExpiresAt, LastRefreshAt: now, Source: item.Source, CredentialFingerprint: oauthCredentialFingerprint(item.RefreshToken, item.AccessToken), CreatedAt: now, UpdatedAt: now}
	secret := oauthSecretRecord{AccountID: account.ID, AccessToken: item.AccessToken, RefreshToken: item.RefreshToken, IDToken: item.IDToken, TokenType: "Bearer", Scopes: item.Scopes}
	action := "created"
	if match >= 0 {
		old := store.Accounts[match]
		account.ID = old.ID
		account.CreatedAt = old.CreatedAt
		account.Quota = old.Quota
		account.LastSuccessAt = old.LastSuccessAt
		if account.DisplayName == "" {
			account.DisplayName = old.DisplayName
		}
		if account.Email == "" {
			account.Email = old.Email
		}
		if account.AccountID == "" {
			account.AccountID = old.AccountID
		}
		if account.UserID == "" {
			account.UserID = old.UserID
		}
		store.Accounts[match] = account
		action = "updated"
	} else {
		store.Accounts = append(store.Accounts, account)
	}
	secrets.Credentials[account.ID] = secret
	if err := s.saveStoresLocked(store, secrets); err != nil {
		return nil, "", err
	}
	return account.summaryPtr(), action, nil
}

func (s *OAuthAccountService) recordQuotaError(id, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err := s.loadStoresLocked()
	if err != nil {
		return
	}
	for i := range store.Accounts {
		if store.Accounts[i].ID == id {
			store.Accounts[i].QuotaError = message
			store.Accounts[i].UpdatedAt = s.now().UTC().Format(time.RFC3339)
			_ = s.saveStoresLocked(store, secrets)
			return
		}
	}
}

func (s *OAuthAccountService) markRefreshError(id, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, secrets, err := s.loadStoresLocked()
	if err != nil {
		return
	}
	for i := range store.Accounts {
		if store.Accounts[i].ID == id {
			store.Accounts[i].Status = string(OAuthAccountRefreshError)
			store.Accounts[i].RefreshError = sanitizeOAuthError(message)
			store.Accounts[i].UpdatedAt = s.now().UTC().Format(time.RFC3339)
			_ = s.saveStoresLocked(store, secrets)
			return
		}
	}
}

func (s *OAuthAccountService) refreshLockLocked(id string) *sync.Mutex {
	if lock := s.refreshLocks[id]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	s.refreshLocks[id] = lock
	return lock
}

func (s *OAuthAccountService) takePending(id string, platform OAuthPlatform) (oauthPendingLogin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pending[strings.TrimSpace(id)]
	if !ok || pending.Platform != platform {
		return oauthPendingLogin{}, errors.New("OAuth 登录会话不存在")
	}
	delete(s.pending, id)
	if !s.now().Before(pending.ExpiresAt) {
		return oauthPendingLogin{}, errors.New("OAuth 登录会话已过期")
	}
	return pending, nil
}

func (s *OAuthAccountService) prunePendingLocked() {
	now := s.now()
	for id, pending := range s.pending {
		if !now.Before(pending.ExpiresAt) {
			delete(s.pending, id)
		}
	}
}

func (s *OAuthAccountService) loadStoresLocked() (oauthAccountStore, oauthSecretStore, error) {
	indexPath := s.oauthStorePath(oauthIndexFileName)
	secretPath := s.oauthStorePath(oauthSecretFileName)
	store := oauthAccountStore{Version: oauthAccountStoreVersion, Accounts: []oauthAccountRecord{}}
	secrets := oauthSecretStore{Version: oauthAccountStoreVersion, Credentials: map[string]oauthSecretRecord{}}
	if data, err := os.ReadFile(indexPath); err == nil {
		if err := json.Unmarshal(data, &store); err != nil {
			return store, secrets, fmt.Errorf("解析 OAuth 账号索引失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return store, secrets, err
	}
	if data, err := os.ReadFile(secretPath); err == nil {
		if err := json.Unmarshal(data, &secrets); err != nil {
			return store, secrets, fmt.Errorf("解析 OAuth 凭据文件失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return store, secrets, err
	}
	if secrets.Credentials == nil {
		secrets.Credentials = map[string]oauthSecretRecord{}
	}
	return store, secrets, nil
}

func (s *OAuthAccountService) saveStoresLocked(store oauthAccountStore, secrets oauthSecretStore) error {
	secretPath := s.oauthStorePath(oauthSecretFileName)
	indexPath := s.oauthStorePath(oauthIndexFileName)
	secretData, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 OAuth 凭据失败: %w", err)
	}
	indexData, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 OAuth 账号索引失败: %w", err)
	}
	secretBefore, secretExisted, err := readOAuthStoreFile(secretPath)
	if err != nil {
		return fmt.Errorf("读取 OAuth 凭据旧快照失败: %w", err)
	}
	indexBefore, indexExisted, err := readOAuthStoreFile(indexPath)
	if err != nil {
		return fmt.Errorf("读取 OAuth 账号索引旧快照失败: %w", err)
	}
	if err := AtomicWriteBytes(secretPath, secretData); err != nil {
		return fmt.Errorf("保存 OAuth 凭据失败: %w", err)
	}
	if err := AtomicWriteBytes(indexPath, indexData); err != nil {
		rollbackErrs := []string{fmt.Sprintf("账号索引: %v", restoreOAuthStoreFile(indexPath, indexExisted, indexBefore))}
		if rollbackErr := restoreOAuthStoreFile(secretPath, secretExisted, secretBefore); rollbackErr != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("凭据: %v", rollbackErr))
		}
		return fmt.Errorf("保存 OAuth 账号索引失败: %w；回滚结果: %s", err, strings.Join(rollbackErrs, "; "))
	}
	return nil
}

func readOAuthStoreFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func restoreOAuthStoreFile(path string, existed bool, data []byte) error {
	if existed {
		return AtomicWriteBytes(path, data)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *OAuthAccountService) loadCLIStateLocked() (oauthCLIState, error) {
	state := oauthCLIState{}
	data, err := os.ReadFile(s.oauthStorePath(oauthCLIStateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("解析 OAuth CLI 状态失败: %w", err)
	}
	return state, nil
}

func (s *OAuthAccountService) saveCLIStateLocked(state oauthCLIState) error {
	return AtomicWriteJSON(s.oauthStorePath(oauthCLIStateFileName), state)
}

func (s *OAuthAccountService) oauthStorePath(name string) string {
	dir := strings.TrimSpace(s.configDir)
	if dir == "" {
		dir = mustGetAppConfigDir()
	}
	return filepath.Join(dir, name)
}

func cliStateForPlatform(state *oauthCLIState, platform string) *[]oauthCLIFileState {
	if platform == "codex" {
		return &state.Codex
	}
	return &state.Claude
}

func findOAuthAccount(store oauthAccountStore, secrets oauthSecretStore, id string) (oauthAccountRecord, oauthSecretRecord, error) {
	for _, account := range store.Accounts {
		if account.ID == id {
			secret, ok := secrets.Credentials[id]
			if !ok {
				return oauthAccountRecord{}, oauthSecretRecord{}, errors.New("OAuth 凭据不存在")
			}
			return account, secret, nil
		}
	}
	return oauthAccountRecord{}, oauthSecretRecord{}, fmt.Errorf("OAuth 账号不存在: %s", id)
}

func (a oauthAccountRecord) summary() OAuthAccountSummary {
	return OAuthAccountSummary{ID: a.ID, Platform: a.Platform, AuthMode: a.AuthMode, DisplayName: a.DisplayName, Email: a.Email, AccountID: a.AccountID, UserID: a.UserID, OrganizationID: a.OrganizationID, OrganizationName: a.OrganizationName, PlanType: a.PlanType, Status: a.Status, AccessTokenExpiresAt: a.AccessTokenExpiresAt, LastRefreshAt: a.LastRefreshAt, LastSuccessAt: a.LastSuccessAt, RefreshError: a.RefreshError, QuotaError: a.QuotaError, Quota: a.Quota, Source: a.Source, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}
func (a oauthAccountRecord) summaryPtr() *OAuthAccountSummary { value := a.summary(); return &value }

func mergeRefreshedAccount(old, next oauthAccountRecord, now time.Time) oauthAccountRecord {
	next.ID = old.ID
	next.CreatedAt = old.CreatedAt
	next.Quota = old.Quota
	next.LastSuccessAt = old.LastSuccessAt
	next.UpdatedAt = now.UTC().Format(time.RFC3339)
	return next
}

func buildOAuthURL(raw string, params map[string]string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func randomOAuthString(length int) (string, error) {
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("生成 OAuth 随机状态失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
func oauthDigest(data []byte) string { sum := sha256.Sum256(data); return fmt.Sprintf("%x", sum[:]) }
func oauthCredentialFingerprint(refresh, access string) string {
	value := strings.TrimSpace(refresh)
	if value == "" {
		value = strings.TrimSpace(access)
	}
	return oauthDigest([]byte(value))
}
func oauthAccountID(item oauthNormalizedItem) string {
	identity := strings.Join([]string{string(item.Platform), string(item.AuthMode), item.AccountID, item.UserID, strings.ToLower(item.Email), item.RefreshToken}, "|")
	return string(item.Platform) + "-" + oauthDigest([]byte(identity))[:24]
}

func parseOAuthCallback(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("OAuth 回调不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Query().Get("code") == "" {
		return "", "", errors.New("OAuth 回调必须是包含 code 和 state 的完整 URL")
	}
	if parsed.Query().Get("error") != "" {
		return "", "", fmt.Errorf("OAuth 授权失败: %s", parsed.Query().Get("error"))
	}
	return parsed.Query().Get("code"), parsed.Query().Get("state"), nil
}

func (s *OAuthAccountService) doJSON(method, endpoint string, body []byte, headers map[string]string) (map[string]any, error) {
	response, _, err := s.doJSONWithStatus(method, endpoint, body, headers)
	return response, err
}
func (s *OAuthAccountService) doJSONWithStatus(method, endpoint string, body []byte, headers map[string]string) (map[string]any, int, error) {
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return s.executeJSON(req)
}
func (s *OAuthAccountService) doForm(endpoint string, form url.Values) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, _, err := s.executeJSON(req)
	return response, err
}
func (s *OAuthAccountService) getJSONWithStatus(endpoint string, headers map[string]string) (map[string]any, int, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return s.executeJSON(req)
}
func (s *OAuthAccountService) executeJSON(req *http.Request) (map[string]any, int, error) {
	response, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	status := response.StatusCode
	limited := io.LimitReader(response.Body, oauthMaxResponseLen)
	data, readErr := io.ReadAll(limited)
	if readErr != nil {
		return nil, status, readErr
	}
	if status < 200 || status >= 300 {
		return nil, status, safeOAuthHTTPError(status, data)
	}
	var value map[string]any
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, status, nil
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, status, fmt.Errorf("OAuth 响应 JSON 无效")
	}
	return value, status, nil
}
func safeOAuthHTTPError(status int, data []byte) error {
	message := ""
	var value map[string]any
	if json.Unmarshal(data, &value) == nil {
		message = stringValue(value, "error", "error_description", "message", "code")
	}
	if message != "" {
		message = sanitizeOAuthError(message)
	}
	if message == "" {
		return fmt.Errorf("HTTP %d", status)
	}
	return fmt.Errorf("HTTP %d (%s)", status, message)
}
func sanitizeOAuthError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 160 {
		message = message[:160]
	}
	return strings.NewReplacer("Bearer ", "Bearer [REDACTED]", "access_token", "access_token", "refresh_token", "refresh_token").Replace(message)
}

func flattenOAuthImportRoot(root any) []map[string]any {
	switch value := root.(type) {
	case []any:
		result := make([]map[string]any, 0, len(value))
		for _, entry := range value {
			if object, ok := entry.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	case map[string]any:
		if accounts, ok := value["accounts"].([]any); ok {
			result := make([]map[string]any, 0, len(accounts))
			for _, entry := range accounts {
				if object, ok := entry.(map[string]any); ok {
					result = append(result, object)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
		return []map[string]any{value}
	default:
		return nil
	}
}

func normalizeOAuthImportItem(platform OAuthPlatform, source string, root map[string]any) (oauthNormalizedItem, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "unknown"
	}
	if platform == OAuthPlatformClaude {
		return normalizeClaudeImport(source, root)
	}
	return normalizeCodexImport(source, root)
}

func normalizeClaudeImport(source string, root map[string]any) (oauthNormalizedItem, error) {
	if strings.EqualFold(stringValue(root, "auth_mode", "authMode"), "desktop_oauth") || root["desktop_oauth"] != nil {
		return oauthNormalizedItem{}, errors.New("Claude Desktop profile 不是 Claude Code OAuth，拒绝导入")
	}
	oauth := firstObject(root, "claudeAiOauth", "claude_ai_oauth")
	for _, container := range []string{"credentials", "claude_credentials_raw", "claudeConfigRaw", "config"} {
		if nested := firstObject(root, container); nested != nil {
			if candidate := firstObject(nested, "claudeAiOauth", "claude_ai_oauth"); candidate != nil {
				oauth = candidate
				break
			}
		}
	}
	if oauth == nil {
		return oauthNormalizedItem{}, errors.New("JSON 缺少 claudeAiOauth OAuth 凭据")
	}
	access := stringValue(oauth, "accessToken", "access_token")
	refresh := stringValue(oauth, "refreshToken", "refresh_token")
	expires := parseOAuthTimestamp(firstValue(oauth, "expiresAt", "expires_at"))
	if expires == 0 {
		expires = jwtExpiryMillis(access)
	}
	if access == "" {
		return oauthNormalizedItem{}, errors.New("Claude JSON 缺少 access token")
	}
	if refresh == "" && (expires == 0 || expires <= time.Now().UnixMilli()) {
		return oauthNormalizedItem{}, errors.New("Claude access-only 凭据已过期或缺少 expiresAt")
	}
	account := firstObject(root, "oauthAccount")
	if account == nil {
		if config := firstObject(root, "config"); config != nil {
			account = firstObject(config, "oauthAccount")
		}
	}
	item := oauthNormalizedItem{Platform: OAuthPlatformClaude, AuthMode: OAuthAuthModeClaudeCode, DisplayName: stringValue(root, "name", "displayName"), Email: firstNonEmpty(stringValue(account, "emailAddress", "email_address", "email"), stringValue(root, "email", "emailAddress")), AccountID: firstNonEmpty(stringValue(account, "accountUuid", "account_uuid", "accountId"), stringValue(root, "accountUuid", "account_id")), OrganizationID: firstNonEmpty(stringValue(account, "organizationUuid", "organization_uuid", "organizationId"), stringValue(root, "organizationUuid")), OrganizationName: firstNonEmpty(stringValue(account, "organizationName", "organization_name")), PlanType: firstNonEmpty(stringValue(account, "subscriptionType", "subscription_type", "planType"), stringValue(root, "plan_type")), AccessToken: access, RefreshToken: refresh, IDToken: stringValue(oauth, "idToken", "id_token"), AccessTokenExpiresAt: expires, Source: source}
	if item.DisplayName == "" {
		item.DisplayName = item.Email
	}
	return item, nil
}

func normalizeCodexImport(source string, root map[string]any) (oauthNormalizedItem, error) {
	mode := stringValue(root, "auth_mode", "authMode")
	if strings.EqualFold(mode, "agentIdentity") || root["agent_identity"] != nil {
		return oauthNormalizedItem{}, errors.New("Codex Agent Identity 不是 OAuth token，拒绝导入")
	}
	if platform := strings.ToLower(stringValue(root, "platform")); platform != "" && platform != "openai" && platform != "codex" {
		return oauthNormalizedItem{}, fmt.Errorf("不支持的 Codex 导入平台: %s", platform)
	}
	credentials := firstObject(root, "credentials")
	tokens := firstObject(root, "tokens")
	access := firstNonEmpty(stringValue(tokens, "access_token", "accessToken"), stringValue(credentials, "access_token", "accessToken"), stringValue(root, "access_token", "accessToken"))
	refresh := firstNonEmpty(stringValue(tokens, "refresh_token", "refreshToken"), stringValue(credentials, "refresh_token", "refreshToken"), stringValue(root, "refresh_token", "refreshToken"))
	idToken := firstNonEmpty(stringValue(tokens, "id_token", "idToken"), stringValue(credentials, "id_token", "idToken"), stringValue(root, "id_token", "idToken"))
	if access == "" {
		return oauthNormalizedItem{}, errors.New("Codex JSON 缺少 access token")
	}
	claims := jwtClaims(idToken)
	accountID := firstNonEmpty(stringValue(tokens, "account_id", "accountId", "chatgpt_account_id"), stringValue(credentials, "account_id", "accountId", "chatgpt_account_id"), stringValue(root, "account_id", "accountId", "chatgpt_account_id"), stringValue(claims, "chatgpt_account_id"), nestedString(claims, []string{"https://api.openai.com/auth", "chatgpt_account_id"}))
	userID := firstNonEmpty(stringValue(credentials, "chatgpt_user_id", "user_id"), stringValue(root, "chatgpt_user_id", "user_id"), stringValue(claims, "sub"), nestedString(claims, []string{"https://api.openai.com/auth", "chatgpt_user_id"}, []string{"https://api.openai.com/auth", "user_id"}))
	email := firstNonEmpty(stringValue(credentials, "email"), stringValue(root, "email"), stringValue(claims, "email"), nestedString(claims, []string{"https://api.openai.com/profile", "email"}))
	plan := firstNonEmpty(stringValue(credentials, "plan_type", "planType"), stringValue(root, "plan_type", "planType"), nestedString(claims, []string{"https://api.openai.com/auth", "chatgpt_plan_type"}))
	expires := parseOAuthTimestamp(firstValue(credentials, "expires_at", "expiresAt"))
	if expires == 0 {
		expires = parseOAuthTimestamp(firstValue(root, "expires_at", "expiresAt", "expired"))
	}
	if expires == 0 {
		expires = jwtExpiryMillis(access)
	}
	if refresh == "" && (expires == 0 || expires <= time.Now().UnixMilli()) {
		return oauthNormalizedItem{}, errors.New("Codex access-only 凭据已过期或缺少 expires_at")
	}
	item := oauthNormalizedItem{Platform: OAuthPlatformCodex, AuthMode: OAuthAuthModeCodex, DisplayName: firstNonEmpty(stringValue(root, "name", "account_name"), email, accountID), Email: email, AccountID: accountID, UserID: userID, PlanType: plan, AccessToken: access, RefreshToken: refresh, IDToken: idToken, AccessTokenExpiresAt: expires, Source: source}
	return item, nil
}

func normalizeOAuthPlatform(value string) OAuthPlatform {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude", "claude_code", "anthropic":
		return OAuthPlatformClaude
	case "codex", "openai":
		return OAuthPlatformCodex
	default:
		return ""
	}
}
func firstObject(root map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := root[key].(map[string]any); ok {
			return value
		}
		if text, ok := root[key].(string); ok {
			var object map[string]any
			if json.Unmarshal([]byte(text), &object) == nil {
				return object
			}
		}
	}
	return nil
}
func firstValue(root map[string]any, keys ...string) any {
	if root == nil {
		return nil
	}
	for _, key := range keys {
		if value, ok := root[key]; ok {
			return value
		}
	}
	return nil
}
func stringValue(root map[string]any, keys ...string) string {
	if root == nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := root[key]; ok {
			switch typed := value.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					return strings.TrimSpace(typed)
				}
			case json.Number:
				return typed.String()
			case float64:
				return strconv.FormatFloat(typed, 'f', -1, 64)
			}
		}
	}
	return ""
}
func intValue(root map[string]any, key string) int {
	raw := stringValue(root, key)
	if raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			return value
		}
	}
	if value, ok := root[key].(float64); ok {
		return int(value)
	}
	return 0
}
func nestedString(root map[string]any, paths ...[]string) string {
	for _, path := range paths {
		current := any(root)
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[key]
		}
		if text, ok := current.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func splitScopes(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Fields(value) {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func parseOAuthTimestamp(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		value = typed.String()
	case float64:
		value = strconv.FormatFloat(typed, 'f', -1, 64)
	}
	switch typed := value.(type) {
	case int64:
		return normalizeTimestampNumber(typed)
	case int:
		return normalizeTimestampNumber(int64(typed))
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0
		}
		if number, err := strconv.ParseInt(text, 10, 64); err == nil {
			return normalizeTimestampNumber(number)
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.UnixMilli()
		}
	}
	return 0
}
func normalizeTimestampNumber(value int64) int64 {
	if value <= 0 {
		return 0
	}
	if value < 100000000000 {
		return value * 1000
	}
	return value
}
func jwtExpiryMillis(token string) int64 {
	claims := jwtClaims(token)
	if claims == nil {
		return 0
	}
	return parseOAuthTimestamp(claims["exp"])
}
func jwtClaims(token string) map[string]any {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(data, &claims) != nil {
		return nil
	}
	return claims
}
func mergeCodexClaimsItem(item *oauthNormalizedItem, idToken string) {
	claims := jwtClaims(idToken)
	if claims == nil {
		return
	}
	item.Email = firstNonEmpty(item.Email, stringValue(claims, "email"), nestedString(claims, []string{"https://api.openai.com/profile", "email"}))
	item.AccountID = firstNonEmpty(item.AccountID, nestedString(claims, []string{"https://api.openai.com/auth", "chatgpt_account_id"}), stringValue(claims, "chatgpt_account_id"))
	item.UserID = firstNonEmpty(item.UserID, nestedString(claims, []string{"https://api.openai.com/auth", "chatgpt_user_id"}, []string{"https://api.openai.com/auth", "user_id"}), stringValue(claims, "sub"))
	item.PlanType = firstNonEmpty(item.PlanType, nestedString(claims, []string{"https://api.openai.com/auth", "chatgpt_plan_type"}))
}
func mergeCodexClaims(account *oauthAccountRecord, idToken string) {
	item := oauthNormalizedItem{Email: account.Email, AccountID: account.AccountID, UserID: account.UserID, PlanType: account.PlanType}
	mergeCodexClaimsItem(&item, idToken)
	account.Email = item.Email
	account.AccountID = item.AccountID
	account.UserID = item.UserID
	account.PlanType = item.PlanType
}
func mergeClaudeProfile(account *oauthAccountRecord, profile map[string]any) {
	item := normalizedClaudeFromTokens(oauthTokenResponse{}, profile, account.Source)
	account.Email = firstNonEmpty(account.Email, item.Email)
	account.AccountID = firstNonEmpty(account.AccountID, item.AccountID)
	account.OrganizationID = firstNonEmpty(account.OrganizationID, item.OrganizationID)
	account.OrganizationName = firstNonEmpty(account.OrganizationName, item.OrganizationName)
	account.PlanType = firstNonEmpty(account.PlanType, item.PlanType)
	account.DisplayName = firstNonEmpty(account.DisplayName, item.DisplayName)
}

func parseOAuthQuota(root map[string]any, now time.Time) OAuthQuotaSnapshot {
	quota := OAuthQuotaSnapshot{FetchedAt: now.UTC().Format(time.RFC3339)}
	walkQuota(root, &quota, "")
	return quota
}
func walkQuota(value any, quota *OAuthQuotaSnapshot, context string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			next := context + " " + lower
			if percent, ok := numberValue(item); ok && strings.Contains(lower, "percent") {
				assignQuotaPercent(quota, next, int(percent))
			}
			if utilization, ok := numberValue(item); ok && strings.Contains(lower, "utilization") {
				if utilization >= 0 && utilization <= 1 {
					utilization *= 100
				}
				assignQuotaPercent(quota, next, int(utilization))
			}
			if strings.Contains(lower, "reset") || strings.Contains(lower, "at") {
				if timestamp := parseOAuthTimestamp(item); timestamp > 0 {
					assignQuotaReset(quota, next, timestamp)
				}
			}
			walkQuota(item, quota, next)
		}
	case []any:
		for _, item := range typed {
			walkQuota(item, quota, context)
		}
	}
}
func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	case string:
		value, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return value, err == nil
	}
	return 0, false
}
func assignQuotaPercent(quota *OAuthQuotaSnapshot, context string, value int) {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	switch {
	case strings.Contains(context, "month") || strings.Contains(context, "30d") || strings.Contains(context, "monthly"):
		quota.MonthlyPercent = value
	case strings.Contains(context, "week") || strings.Contains(context, "7d") || strings.Contains(context, "weekly"):
		quota.WeeklyPercent = value
	default:
		if quota.ShortWindowPercent == 0 {
			quota.ShortWindowPercent = value
		}
	}
}
func assignQuotaReset(quota *OAuthQuotaSnapshot, context string, value int64) {
	switch {
	case strings.Contains(context, "month") || strings.Contains(context, "30d"):
		quota.MonthlyResetAt = value
	case strings.Contains(context, "week") || strings.Contains(context, "7d"):
		quota.WeeklyResetAt = value
	default:
		if quota.ShortWindowResetAt == 0 {
			quota.ShortWindowResetAt = value
		}
	}
}

func readJSONObjectOrEmpty(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return map[string]any{}, nil
	}
	return value, nil
}
func nilSafeCLIError(path string, err error) error {
	return fmt.Errorf("读取 CLI 文件失败 %s: %w", path, err)
}

func providersReferencingCredential(platform, id string) ([]string, error) {
	service := NewProviderService()
	providers, err := service.LoadProviders(platform)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0)
	for _, provider := range providers {
		if strings.EqualFold(provider.CredentialType, "oauth") && provider.CredentialRef == id {
			refs = append(refs, provider.Name)
		}
	}
	return refs, nil
}
