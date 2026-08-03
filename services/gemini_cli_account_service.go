package services

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GeminiCliQuotaSnapshot struct {
	Plan             string  `json:"plan,omitempty"`
	RemainingPercent float64 `json:"remainingPercent,omitempty"`
	ResetAt          string  `json:"resetAt,omitempty"`
	FetchedAt        string  `json:"fetchedAt,omitempty"`
	Error            string  `json:"error,omitempty"`
}

type GeminiCliAccount struct {
	ID              string                  `json:"id"`
	Email           string                  `json:"email,omitempty"`
	ProjectID       string                  `json:"projectId,omitempty"`
	RuntimeRoot     string                  `json:"runtimeRoot"`
	TokenExpiresAt  string                  `json:"tokenExpiresAt,omitempty"`
	HasAccessToken  bool                    `json:"hasAccessToken"`
	HasRefreshToken bool                    `json:"hasRefreshToken"`
	Applied         bool                    `json:"applied"`
	LastRefreshAt   string                  `json:"lastRefreshAt,omitempty"`
	RefreshError    string                  `json:"refreshError,omitempty"`
	Quota           *GeminiCliQuotaSnapshot `json:"quota,omitempty"`
}

type GeminiCliOAuthLogin struct {
	SessionID        string `json:"sessionId"`
	AuthorizationURL string `json:"authorizationUrl"`
	RedirectURI      string `json:"redirectUri"`
	ExpiresAt        string `json:"expiresAt"`
}

type GeminiCliOAuthLoginStatus struct {
	SessionID string `json:"sessionId"`
	Completed bool   `json:"completed"`
	AccountID string `json:"accountId,omitempty"`
	Error     string `json:"error,omitempty"`
	ExpiresAt string `json:"expiresAt"`
}

type geminiOAuthLoginSession struct {
	status       GeminiCliOAuthLoginStatus
	listener     net.Listener
	clientID     string
	clientSecret string
	redirectURI  string
}

type GeminiCliAccountService struct {
	mu            sync.Mutex
	oauthMu       sync.Mutex
	oauthSessions map[string]*geminiOAuthLoginSession
}

func NewGeminiCliAccountService() *GeminiCliAccountService {
	return &GeminiCliAccountService{oauthSessions: make(map[string]*geminiOAuthLoginSession)}
}

func (s *GeminiCliAccountService) Start() error { return nil }
func (s *GeminiCliAccountService) Stop() error  { return nil }

// StartOAuthLogin starts a loopback OAuth callback. The OAuth client ID and
// optional secret are process configuration, never persisted or returned to UI.
func (s *GeminiCliAccountService) StartOAuthLogin() (*GeminiCliOAuthLogin, error) {
	clientID := strings.TrimSpace(os.Getenv("GEMINI_CLI_OAUTH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GEMINI_CLI_OAUTH_CLIENT_SECRET"))
	if clientID == "" {
		return nil, fmt.Errorf("未配置 GEMINI_CLI_OAUTH_CLIENT_ID，无法启动 Gemini OAuth 登录")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("启动 Gemini OAuth 回调监听失败: %w", err)
	}
	sessionID := randomSessionID()
	state := randomSessionID()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth2callback", listener.Addr().(*net.TCPAddr).Port)
	expiresAt := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	query := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile"},
		"state":         {state},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}
	login := &GeminiCliOAuthLogin{
		SessionID:        sessionID,
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?" + query.Encode(),
		RedirectURI:      redirectURI,
		ExpiresAt:        expiresAt,
	}
	s.oauthMu.Lock()
	s.oauthSessions[sessionID] = &geminiOAuthLoginSession{
		status:       GeminiCliOAuthLoginStatus{SessionID: sessionID, ExpiresAt: expiresAt},
		listener:     listener,
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
	}
	s.oauthMu.Unlock()
	go s.completeOAuthLogin(sessionID, state)
	return login, nil
}

func (s *GeminiCliAccountService) GetOAuthLoginStatus(sessionID string) (*GeminiCliOAuthLoginStatus, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	session, ok := s.oauthSessions[strings.TrimSpace(sessionID)]
	if !ok {
		return nil, fmt.Errorf("未找到 Gemini OAuth 登录会话")
	}
	status := session.status
	return &status, nil
}

func (s *GeminiCliAccountService) GetAccounts() ([]GeminiCliAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAccountsLocked()
}

func (s *GeminiCliAccountService) completeOAuthLogin(sessionID, expectedState string) {
	s.oauthMu.Lock()
	session := s.oauthSessions[sessionID]
	s.oauthMu.Unlock()
	if session == nil {
		return
	}
	defer session.listener.Close()
	if tcpListener, ok := session.listener.(*net.TCPListener); ok {
		_ = tcpListener.SetDeadline(time.Now().Add(5 * time.Minute))
	}
	connection, err := session.listener.Accept()
	if err != nil {
		s.setOAuthLoginError(sessionID, fmt.Errorf("等待 Gemini OAuth 回调失败: %w", err))
		return
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	request, err := http.ReadRequest(bufio.NewReader(connection))
	if err != nil {
		s.setOAuthLoginError(sessionID, fmt.Errorf("读取 Gemini OAuth 回调失败: %w", err))
		return
	}
	query := request.URL.Query()
	if query.Get("state") != expectedState {
		writeOAuthCallbackResponse(connection, http.StatusBadRequest, "Gemini OAuth state 校验失败，请重试")
		s.setOAuthLoginError(sessionID, fmt.Errorf("Gemini OAuth state 校验失败"))
		return
	}
	if callbackError := strings.TrimSpace(query.Get("error")); callbackError != "" {
		writeOAuthCallbackResponse(connection, http.StatusBadRequest, "Gemini OAuth 登录被取消")
		s.setOAuthLoginError(sessionID, fmt.Errorf("Gemini OAuth 登录失败: %s", callbackError))
		return
	}
	code := strings.TrimSpace(query.Get("code"))
	if code == "" {
		writeOAuthCallbackResponse(connection, http.StatusBadRequest, "Gemini OAuth 回调缺少授权码")
		s.setOAuthLoginError(sessionID, fmt.Errorf("Gemini OAuth 回调缺少授权码"))
		return
	}
	writeOAuthCallbackResponse(connection, http.StatusOK, "Gemini OAuth 登录完成，可以返回 code-switch-R")

	token, err := exchangeGeminiOAuthCode(request.Context(), session.clientID, session.clientSecret, code, session.redirectURI)
	if err != nil {
		s.setOAuthLoginError(sessionID, err)
		return
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		s.setOAuthLoginError(sessionID, fmt.Errorf("Gemini OAuth 响应缺少 access_token"))
		return
	}
	runtime, err := ResolveGeminiCLIRuntime()
	if err != nil {
		s.setOAuthLoginError(sessionID, err)
		return
	}
	userInfo := fetchGeminiOAuthUserInfo(request.Context(), token.AccessToken)
	credentials := map[string]any{
		"access_token": token.AccessToken,
		"token_type":   firstString(token.Raw, "token_type", "tokenType"),
		"scope":        firstString(token.Raw, "scope"),
		"expiry_date":  time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UnixMilli(),
		"token_uri":    geminiOAuthTokenURL(),
	}
	if credentials["token_type"] == "" {
		credentials["token_type"] = "Bearer"
	}
	if refreshToken := firstString(token.Raw, "refresh_token", "refreshToken"); refreshToken != "" {
		credentials["refresh_token"] = refreshToken
	}
	if email := firstString(userInfo, "email"); email != "" {
		credentials["email"] = email
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		s.setOAuthLoginError(sessionID, fmt.Errorf("序列化 Gemini OAuth 凭据失败: %w", err))
		return
	}
	if err := atomicWriteFile(filepathJoin(runtime.Root, "oauth_creds.json"), data, 0o600); err != nil {
		s.setOAuthLoginError(sessionID, fmt.Errorf("写入 Gemini OAuth 凭据失败: %w", err))
		return
	}
	account := accountFromOAuth(runtime.Root, credentials)
	if err := s.upsertAccountMetadata(account); err != nil {
		s.setOAuthLoginError(sessionID, err)
		return
	}
	s.oauthMu.Lock()
	if current := s.oauthSessions[sessionID]; current != nil {
		current.status.Completed = true
		current.status.AccountID = account.ID
	}
	s.oauthMu.Unlock()
}

func (s *GeminiCliAccountService) setOAuthLoginError(sessionID string, err error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	if session := s.oauthSessions[sessionID]; session != nil {
		session.status.Error = err.Error()
	}
}

func writeOAuthCallbackResponse(connection net.Conn, status int, body string) {
	statusText := http.StatusText(status)
	_, _ = fmt.Fprintf(connection, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, statusText, len(body), body)
}

type geminiOAuthToken struct {
	AccessToken string
	ExpiresIn   int64
	Raw         map[string]any
}

func exchangeGeminiOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (geminiOAuthToken, error) {
	values := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {clientID},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	if clientSecret != "" {
		values.Set("client_secret", clientSecret)
	}
	requestContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, geminiOAuthTokenURL(), strings.NewReader(values.Encode()))
	if err != nil {
		return geminiOAuthToken{}, fmt.Errorf("创建 Gemini OAuth token 请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return geminiOAuthToken{}, fmt.Errorf("Gemini OAuth token 请求失败: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return geminiOAuthToken{}, fmt.Errorf("读取 Gemini OAuth token 响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return geminiOAuthToken{}, fmt.Errorf("Gemini OAuth token 请求失败: HTTP %d", response.StatusCode)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return geminiOAuthToken{}, fmt.Errorf("Gemini OAuth token 响应不是合法 JSON: %w", err)
	}
	return geminiOAuthToken{AccessToken: firstString(raw, "access_token", "accessToken"), ExpiresIn: int64FromAny(raw["expires_in"]), Raw: raw}, nil
}

func fetchGeminiOAuthUserInfo(ctx context.Context, accessToken string) map[string]any {
	requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil)
	if err != nil {
		return nil
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	var userInfo map[string]any
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&userInfo) != nil {
		return nil
	}
	return userInfo
}

func geminiOAuthTokenURL() string {
	if value := strings.TrimSpace(os.Getenv("GEMINI_CLI_OAUTH_TOKEN_URL")); value != "" {
		return value
	}
	return "https://oauth2.googleapis.com/token"
}

func randomSessionID() string {
	bytes := make([]byte, 16)
	if _, err := cryptorand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(hash[:16])
}

// RefreshAccount 只在 oauth_creds.json 提供标准 refresh_token 和 token URL 时执行刷新。
// 缺少刷新材料时返回错误，不把重新读取旧 token 伪装成刷新成功。
func (s *GeminiCliAccountService) RefreshAccount(accountID string) (*GeminiCliAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, err := ResolveGeminiCLIRuntime()
	if err != nil {
		return nil, err
	}
	path := filepathJoin(runtime.Root, "oauth_creds.json")
	raw, err := readGeminiOAuthJSONObject(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Gemini OAuth 文件失败: %w", err)
	}
	account := accountFromOAuth(runtime.Root, raw)
	if account.ID != accountID {
		return nil, fmt.Errorf("未找到 Gemini CLI 账号 %s", accountID)
	}
	refreshToken := firstString(raw, "refresh_token", "refreshToken")
	tokenURL := firstString(raw, "token_uri", "tokenUrl", "token_url")
	clientID := firstString(raw, "client_id", "clientId")
	clientSecret := firstString(raw, "client_secret", "clientSecret")
	if tokenURL == "" {
		tokenURL = geminiOAuthTokenURL()
	}
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv("GEMINI_CLI_OAUTH_CLIENT_ID"))
	}
	if clientSecret == "" {
		clientSecret = strings.TrimSpace(os.Getenv("GEMINI_CLI_OAUTH_CLIENT_SECRET"))
	}
	if refreshToken == "" {
		return nil, fmt.Errorf("Gemini CLI 账号缺少 refresh_token，无法刷新")
	}
	values := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	if clientID != "" {
		values.Set("client_id", clientID)
	}
	if clientSecret != "" {
		values.Set("client_secret", clientSecret)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建 Gemini OAuth 刷新请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Gemini OAuth 刷新请求失败: %w", err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return nil, fmt.Errorf("读取 Gemini OAuth 刷新响应失败: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Gemini OAuth 刷新失败: HTTP %d", response.StatusCode)
	}
	var refreshed map[string]any
	if err := json.Unmarshal(body, &refreshed); err != nil {
		return nil, fmt.Errorf("Gemini OAuth 刷新响应不是合法 JSON: %w", err)
	}
	accessToken := firstString(refreshed, "access_token", "accessToken")
	if accessToken == "" {
		return nil, fmt.Errorf("Gemini OAuth 刷新响应缺少 access_token")
	}
	raw["access_token"] = accessToken
	if expiresIn := int64FromAny(refreshed["expires_in"]); expiresIn > 0 {
		raw["expiry_date"] = time.Now().Add(time.Duration(expiresIn) * time.Second).UnixMilli()
	}
	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("写入 Gemini OAuth 文件失败: %w", err)
	}
	account.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	account.RefreshError = ""
	if err := s.upsertAccountMetadata(account); err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *GeminiCliAccountService) RefreshQuota(accountID string) (*GeminiCliQuotaSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.readAccountsLocked()
	if err != nil {
		return nil, err
	}
	var account *GeminiCliAccount
	for index := range accounts {
		if accounts[index].ID == accountID {
			account = &accounts[index]
			break
		}
	}
	if account == nil {
		return nil, fmt.Errorf("未找到 Gemini CLI 账号 %s", accountID)
	}
	quotaURL := strings.TrimSpace(os.Getenv("GEMINI_CODE_ASSIST_QUOTA_URL"))
	if quotaURL == "" {
		err := fmt.Errorf("未配置 GEMINI_CODE_ASSIST_QUOTA_URL，不能伪造实时额度")
		return s.saveQuotaError(account, err)
	}
	raw, err := readGeminiOAuthJSONObject(filepathJoin(account.RuntimeRoot, "oauth_creds.json"))
	if err != nil {
		return s.saveQuotaError(account, fmt.Errorf("读取 Gemini OAuth 文件失败: %w", err))
	}
	accessToken := firstString(raw, "access_token", "accessToken")
	if accessToken == "" {
		return s.saveQuotaError(account, fmt.Errorf("Gemini CLI 账号缺少 access_token，无法查询额度"))
	}
	requestBody, err := json.Marshal(map[string]any{"projectId": account.ProjectID})
	if err != nil {
		return s.saveQuotaError(account, fmt.Errorf("构建 Gemini 配额请求失败: %w", err))
	}
	requestContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, quotaURL, strings.NewReader(string(requestBody)))
	if err != nil {
		return s.saveQuotaError(account, fmt.Errorf("创建 Gemini 配额请求失败: %w", err))
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return s.saveQuotaError(account, fmt.Errorf("Gemini 配额请求失败: %w", err))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return s.saveQuotaError(account, fmt.Errorf("读取 Gemini 配额响应失败: %w", err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return s.saveQuotaError(account, fmt.Errorf("Gemini 配额请求失败: HTTP %d", response.StatusCode))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return s.saveQuotaError(account, fmt.Errorf("Gemini 配额响应不是合法 JSON: %w", err))
	}
	quota, err := parseGeminiQuota(payload)
	if err != nil {
		return s.saveQuotaError(account, err)
	}
	account.Quota = quota
	account.RefreshError = ""
	if err := s.upsertAccountMetadata(*account); err != nil {
		return nil, err
	}
	return quota, nil
}

func (s *GeminiCliAccountService) saveQuotaError(account *GeminiCliAccount, err error) (*GeminiCliQuotaSnapshot, error) {
	if account.Quota == nil {
		account.Quota = &GeminiCliQuotaSnapshot{}
	}
	account.Quota.Error = err.Error()
	if metadataErr := s.upsertAccountMetadata(*account); metadataErr != nil {
		return account.Quota, fmt.Errorf("%w；保存额度错误状态失败: %v", err, metadataErr)
	}
	return account.Quota, err
}

func parseGeminiQuota(payload map[string]any) (*GeminiCliQuotaSnapshot, error) {
	if nested, ok := payload["quota"].(map[string]any); ok {
		for key, value := range nested {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	quota := &GeminiCliQuotaSnapshot{
		Plan:             firstString(payload, "plan", "planType", "plan_type"),
		RemainingPercent: float64FromAny(payload["remainingPercent"]),
		ResetAt:          firstString(payload, "resetAt", "reset_at", "resetTime"),
		FetchedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	if quota.RemainingPercent == 0 {
		quota.RemainingPercent = float64FromAny(payload["remaining_percent"])
	}
	if quota.RemainingPercent > 0 && quota.RemainingPercent <= 1 {
		quota.RemainingPercent *= 100
	}
	if quota.Plan == "" && quota.RemainingPercent == 0 && quota.ResetAt == "" {
		return nil, fmt.Errorf("Gemini 配额响应缺少可识别字段")
	}
	return quota, nil
}

func (s *GeminiCliAccountService) readAccountsLocked() ([]GeminiCliAccount, error) {
	runtime, err := ResolveGeminiCLIRuntime()
	if err != nil {
		return nil, err
	}
	path := filepathJoin(runtime.Root, "oauth_creds.json")
	raw, err := readGeminiOAuthJSONObject(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []GeminiCliAccount{}, nil
		}
		return nil, fmt.Errorf("读取 Gemini OAuth 文件失败: %w", err)
	}
	account := accountFromOAuth(runtime.Root, raw)
	metadata, _ := readAccountMetadata()
	for _, saved := range metadata {
		if saved.ID == account.ID {
			mergeSavedGeminiAccount(&account, saved)
		}
	}
	return []GeminiCliAccount{account}, nil
}

func accountFromOAuth(root string, raw map[string]any) GeminiCliAccount {
	email := firstString(raw, "email", "accountEmail", "emailAddress")
	project := firstString(raw, "project_id", "projectId", "quotaProjectId")
	accessToken := firstString(raw, "access_token", "accessToken")
	refreshToken := firstString(raw, "refresh_token", "refreshToken")
	expiresAt := oauthExpiry(raw)
	identity := email + "\x00" + project
	hash := sha256.Sum256([]byte(identity))
	return GeminiCliAccount{
		ID:              "gemini-cli-" + hex.EncodeToString(hash[:8]),
		Email:           email,
		ProjectID:       project,
		RuntimeRoot:     root,
		TokenExpiresAt:  expiresAt,
		HasAccessToken:  accessToken != "",
		HasRefreshToken: refreshToken != "",
		Applied:         true,
	}
}

func readGeminiOAuthJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if stringValue, ok := value[key].(string); ok && strings.TrimSpace(stringValue) != "" {
			return strings.TrimSpace(stringValue)
		}
	}
	return ""
}

func oauthExpiry(value map[string]any) string {
	for _, key := range []string{"expiry_date", "expiryDate", "expires_at", "expiresAt"} {
		switch typed := value[key].(type) {
		case float64:
			return time.UnixMilli(int64(typed)).UTC().Format(time.RFC3339)
		case string:
			return strings.TrimSpace(typed)
		}
	}
	return ""
}

func (s *GeminiCliAccountService) writeAccountMetadata(accounts []GeminiCliAccount) error {
	path, err := accountMetadataPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}

func (s *GeminiCliAccountService) upsertAccountMetadata(account GeminiCliAccount) error {
	accounts, err := readAccountMetadata()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated := false
	for index := range accounts {
		if accounts[index].ID == account.ID {
			mergeSavedGeminiAccount(&account, accounts[index])
			accounts[index] = account
			updated = true
			break
		}
	}
	if !updated {
		accounts = append(accounts, account)
	}
	return s.writeAccountMetadata(accounts)
}

func mergeSavedGeminiAccount(account *GeminiCliAccount, saved GeminiCliAccount) {
	if account.LastRefreshAt == "" {
		account.LastRefreshAt = saved.LastRefreshAt
	}
	if account.RefreshError == "" {
		account.RefreshError = saved.RefreshError
	}
	if account.Quota == nil {
		account.Quota = saved.Quota
	}
}

func readAccountMetadata() ([]GeminiCliAccount, error) {
	path, err := accountMetadataPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var accounts []GeminiCliAccount
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func accountMetadataPath() (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepathJoin(dir, "gemini-cli-accounts.json"), nil
}

func filepathJoin(left, right string) string {
	return strings.TrimRight(left, `/\\`) + string(os.PathSeparator) + strings.TrimLeft(right, `/\\`)
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}

func float64FromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	default:
		return 0
	}
}
