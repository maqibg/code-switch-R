package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	grokOAuthStoreVersion  = 1
	grokOAuthMaxFileSize   = 1 << 20
	grokOAuthMaxImportFile = 1000
	grokXAIClientID        = "b1a00492-073a-47ea-816f-4c329264a828"
	grokXAIIssuer          = "https://auth.x.ai"
	grokXAIDiscoveryURL    = "https://auth.x.ai/.well-known/openid-configuration"
	grokXAIScope           = "openid profile email offline_access grok-cli:access api:access conversations:read conversations:write"
	grokDeviceGrantType    = "urn:ietf:params:oauth:grant-type:device_code"
	grokCLIProxyBase       = "https://cli-chat-proxy.grok.com/v1"
	grokCLIClientVersion   = "0.2.111"
	grokCLIUserAgent       = "grok-shell/0.2.111 (windows; x86_64)"
)

type GrokOAuthQuota struct {
	PlanType                string   `json:"planType,omitempty"`
	WeeklyRemainingPercent  *float64 `json:"weeklyRemainingPercent,omitempty"`
	MonthlyRemainingPercent *float64 `json:"monthlyRemainingPercent,omitempty"`
	WeeklyResetAt           string   `json:"weeklyResetAt,omitempty"`
	MonthlyResetAt          string   `json:"monthlyResetAt,omitempty"`
	DataUpdatedAt           string   `json:"dataUpdatedAt,omitempty"`
	LastAttemptAt           string   `json:"lastAttemptAt,omitempty"`
	LastError               string   `json:"lastError,omitempty"`
}

type GrokOAuthAccountDTO struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Email        string         `json:"email,omitempty"`
	Subject      string         `json:"subject,omitempty"`
	Source       string         `json:"source"`
	Configured   bool           `json:"configured"`
	Applied      bool           `json:"applied"`
	ExpiresAt    string         `json:"expiresAt,omitempty"`
	NeedsRefresh bool           `json:"needsRefresh"`
	NeedsRelogin bool           `json:"needsRelogin"`
	LastError    string         `json:"lastError,omitempty"`
	CreatedAt    string         `json:"createdAt"`
	UpdatedAt    string         `json:"updatedAt"`
	Quota        GrokOAuthQuota `json:"quota"`
}

type GrokOAuthImportResult struct {
	Path       string   `json:"path"`
	Success    bool     `json:"success"`
	Imported   int      `json:"imported"`
	Updated    int      `json:"updated"`
	AccountIDs []string `json:"accountIds,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type GrokOAuthRefreshResult struct {
	AccountID string               `json:"accountId"`
	Success   bool                 `json:"success"`
	Account   *GrokOAuthAccountDTO `json:"account,omitempty"`
	Error     string               `json:"error,omitempty"`
}

type GrokDeviceAuthStartResult struct {
	SessionID               string `json:"sessionId"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	UserCode                string `json:"userCode"`
	ExpiresAt               string `json:"expiresAt"`
	PollIntervalSeconds     int    `json:"pollIntervalSeconds"`
}

type GrokDeviceAuthStatus struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

type grokOAuthAccount struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Email         string         `json:"email,omitempty"`
	Subject       string         `json:"subject,omitempty"`
	Issuer        string         `json:"issuer"`
	ClientID      string         `json:"clientId"`
	ScopeKey      string         `json:"scopeKey"`
	TokenEndpoint string         `json:"tokenEndpoint,omitempty"`
	AuthEntry     map[string]any `json:"authEntry"`
	Source        string         `json:"source"`
	SourcePath    string         `json:"sourcePath,omitempty"`
	ExpiresAt     string         `json:"expiresAt,omitempty"`
	NeedsRelogin  bool           `json:"needsRelogin,omitempty"`
	LastError     string         `json:"lastError,omitempty"`
	CreatedAt     string         `json:"createdAt"`
	UpdatedAt     string         `json:"updatedAt"`
	Quota         GrokOAuthQuota `json:"quota"`
}

type grokOAuthStore struct {
	Version  int                `json:"version"`
	Accounts []grokOAuthAccount `json:"accounts"`
}

type parsedGrokCredential struct {
	Account grokOAuthAccount
}

type grokDeviceSession struct {
	status GrokDeviceAuthStatus
	cancel context.CancelFunc
}

type grokOIDCDiscovery struct {
	Issuer                      string `json:"issuer"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	UserinfoEndpoint            string `json:"userinfo_endpoint"`
}

type grokDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type grokTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func grokOAuthStorePath() string {
	return filepath.Join(mustGetAppConfigDir(), "grok-oauth-accounts.json")
}

func loadGrokOAuthStore() (grokOAuthStore, error) {
	store := grokOAuthStore{Version: grokOAuthStoreVersion, Accounts: []grokOAuthAccount{}}
	err := ReadJSONFile(grokOAuthStorePath(), &store)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("读取 Grok OAuth 账号库失败: %w", err)
	}
	if store.Version != grokOAuthStoreVersion {
		return store, fmt.Errorf("不支持的 Grok OAuth 账号库版本: %d", store.Version)
	}
	if store.Accounts == nil {
		store.Accounts = []grokOAuthAccount{}
	}
	return store, nil
}

func saveGrokOAuthStore(store grokOAuthStore) error {
	store.Version = grokOAuthStoreVersion
	return AtomicWriteJSON(grokOAuthStorePath(), store)
}

func (s *GrokBuildService) ListOAuthAccounts() ([]GrokOAuthAccountDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := loadGrokOAuthStore()
	if err != nil {
		return nil, err
	}
	state, err := loadGrokRuntimeState()
	if err != nil {
		return nil, err
	}
	accounts := make([]GrokOAuthAccountDTO, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		accounts = append(accounts, grokOAuthAccountDTO(account, state.AppliedAccountID))
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i].Applied != accounts[j].Applied {
			return accounts[i].Applied
		}
		return accounts[i].CreatedAt < accounts[j].CreatedAt
	})
	return accounts, nil
}

func grokOAuthAccountDTO(account grokOAuthAccount, appliedID string) GrokOAuthAccountDTO {
	expiresAt := parseGrokTime(account.ExpiresAt)
	needsRefresh := !expiresAt.IsZero() && time.Until(expiresAt) <= 30*time.Minute
	return GrokOAuthAccountDTO{
		ID:           account.ID,
		Name:         account.Name,
		Email:        account.Email,
		Subject:      account.Subject,
		Source:       account.Source,
		Configured:   grokAccessToken(account.AuthEntry) != "",
		Applied:      account.ID == appliedID,
		ExpiresAt:    account.ExpiresAt,
		NeedsRefresh: needsRefresh,
		NeedsRelogin: account.NeedsRelogin,
		LastError:    account.LastError,
		CreatedAt:    account.CreatedAt,
		UpdatedAt:    account.UpdatedAt,
		Quota:        account.Quota,
	}
}

func (s *GrokBuildService) ImportCurrentOAuthAccount() (GrokOAuthImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokOAuthImportResult{}, err
	}
	paths, err := resolveGrokConfigPaths(state)
	if err != nil {
		return GrokOAuthImportResult{}, err
	}
	return s.importOAuthFileLocked(paths.AuthPath), nil
}

func (s *GrokBuildService) ImportOAuthFiles(paths []string) ([]GrokOAuthImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := make([]GrokOAuthImportResult, 0, len(paths))
	for _, path := range paths {
		results = append(results, s.importOAuthFileLocked(path))
	}
	return results, nil
}

func (s *GrokBuildService) ImportOAuthDirectory(directory string) ([]GrokOAuthImportResult, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("导入目录不能为空")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, fmt.Errorf("读取导入目录失败: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("导入路径不是目录: %s", directory)
	}
	paths := make([]string, 0)
	err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			paths = append(paths, path)
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			return nil
		}
		if len(paths) >= grokOAuthMaxImportFile {
			return fmt.Errorf("目录中的 JSON 文件超过 %d 个限制", grokOAuthMaxImportFile)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return s.ImportOAuthFiles(paths)
}

func (s *GrokBuildService) importOAuthFileLocked(path string) GrokOAuthImportResult {
	result := GrokOAuthImportResult{Path: path}
	path = strings.TrimSpace(path)
	if path == "" {
		result.Error = "文件路径为空"
		return result
	}
	info, err := os.Stat(path)
	if err != nil {
		result.Error = fmt.Sprintf("读取文件失败: %v", err)
		return result
	}
	if !info.Mode().IsRegular() {
		result.Error = "路径不是普通文件"
		return result
	}
	if info.Size() > grokOAuthMaxFileSize {
		result.Error = "文件超过 1 MiB 限制"
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.Error = fmt.Sprintf("读取文件失败: %v", err)
		return result
	}
	credentials, err := parseGrokOAuthCredentials(data, path)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	store, err := loadGrokOAuthStore()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for _, credential := range credentials {
		index := grokOAuthAccountIndex(store.Accounts, credential.Account.ID)
		if index >= 0 {
			store.Accounts[index] = mergeImportedGrokAccount(store.Accounts[index], credential.Account)
			result.Updated++
		} else {
			store.Accounts = append(store.Accounts, credential.Account)
			result.Imported++
		}
		result.AccountIDs = append(result.AccountIDs, credential.Account.ID)
	}
	if err := saveGrokOAuthStore(store); err != nil {
		result.Error = fmt.Sprintf("保存导入账号失败: %v", err)
		return result
	}
	result.Success = true
	return result
}

func mergeImportedGrokAccount(existing, incoming grokOAuthAccount) grokOAuthAccount {
	entry := cloneGrokJSONMap(existing.AuthEntry)
	for key, value := range incoming.AuthEntry {
		if key == "refresh_token" && strings.TrimSpace(stringFromAny(value)) == "" {
			continue
		}
		entry[key] = value
	}
	incoming.AuthEntry = entry
	incoming.CreatedAt = existing.CreatedAt
	incoming.Quota = existing.Quota
	if incoming.Name == "" {
		incoming.Name = existing.Name
	}
	return incoming
}

func parseGrokOAuthCredentials(data []byte, sourcePath string) ([]parsedGrokCredential, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("认证 JSON 为空")
	}
	if len(data) > grokOAuthMaxFileSize {
		return nil, fmt.Errorf("认证 JSON 超过 1 MiB 限制")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("认证文件不是有效 JSON: %w", err)
	}
	providerType := strings.ToLower(strings.TrimSpace(stringFromAny(root["type"])))
	if providerType != "" && providerType != "xai" && providerType != "grok" {
		return nil, fmt.Errorf("认证文件 type=%q，不是 xai/grok", providerType)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	credentials := make([]parsedGrokCredential, 0)
	if account, ok := grokAccountFromEntry(root, "cpa-xai", sourcePath, "", now); ok {
		credentials = append(credentials, parsedGrokCredential{Account: account})
		return credentials, nil
	}
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry, ok := root[key].(map[string]any)
		if !ok || !isGrokXAIEntry(key, entry) {
			continue
		}
		if account, found := grokAccountFromEntry(entry, "grok-auth-json", sourcePath, key, now); found {
			credentials = append(credentials, parsedGrokCredential{Account: account})
		}
	}
	if len(credentials) == 0 {
		return nil, fmt.Errorf("未找到 xAI access token；支持 CPA xai-*.json 或 Grok CLI auth.json")
	}
	return credentials, nil
}

func isGrokXAIEntry(scopeKey string, entry map[string]any) bool {
	issuer := strings.TrimRight(strings.TrimSpace(firstGrokString(entry, "issuer", "oidc_issuer")), "/")
	if issuer == "" && strings.HasPrefix(strings.ToLower(scopeKey), strings.ToLower(grokXAIIssuer)+"::") {
		issuer = grokXAIIssuer
	}
	return strings.EqualFold(issuer, grokXAIIssuer)
}

func grokAccountFromEntry(entry map[string]any, source, sourcePath, originalScope, now string) (grokOAuthAccount, bool) {
	accessToken := strings.TrimSpace(firstGrokString(entry, "access_token", "key"))
	if accessToken == "" {
		return grokOAuthAccount{}, false
	}
	issuer := strings.TrimRight(strings.TrimSpace(firstGrokString(entry, "issuer", "oidc_issuer")), "/")
	if issuer == "" {
		issuer = grokXAIIssuer
	}
	clientID := strings.TrimSpace(firstGrokString(entry, "client_id", "oidc_client_id"))
	if clientID == "" {
		clientID = grokXAIClientID
	}
	claims := decodeGrokJWTClaims(accessToken)
	subject := strings.TrimSpace(firstGrokString(entry, "sub", "user_id", "principal_id"))
	if subject == "" {
		subject = strings.TrimSpace(stringFromAny(claims["sub"]))
	}
	email := strings.TrimSpace(stringFromAny(entry["email"]))
	if email == "" {
		email = strings.TrimSpace(stringFromAny(claims["email"]))
	}
	expiresAt := parseGrokTime(firstGrokString(entry, "expired", "expires_at"))
	if expiresAt.IsZero() {
		expiresAt = grokJWTExpiry(accessToken)
	}
	scopeKey := originalScope
	if scopeKey == "" || !strings.Contains(scopeKey, "::") {
		scopeKey = grokAuthScopeKey(issuer, clientID)
	}
	authEntry := cloneGrokJSONMap(entry)
	authEntry["key"] = accessToken
	authEntry["auth_mode"] = "oidc"
	authEntry["oidc_issuer"] = issuer
	authEntry["oidc_client_id"] = clientID
	if subject != "" {
		authEntry["user_id"] = subject
	}
	if email != "" {
		authEntry["email"] = email
	}
	if !expiresAt.IsZero() {
		authEntry["expires_at"] = expiresAt.Format(time.RFC3339Nano)
	}
	identity := issuer + "\x00" + clientID + "\x00"
	switch {
	case subject != "":
		identity += "sub:" + subject
	case email != "":
		identity += "email:" + strings.ToLower(email)
	default:
		hash := sha256.Sum256([]byte(accessToken))
		identity += "token:" + hex.EncodeToString(hash[:])
	}
	idHash := sha256.Sum256([]byte(identity))
	name := email
	if name == "" {
		name = subject
	}
	if name == "" {
		name = "xAI account"
	}
	return grokOAuthAccount{
		ID:            "grok-" + hex.EncodeToString(idHash[:16]),
		Name:          name,
		Email:         email,
		Subject:       subject,
		Issuer:        issuer,
		ClientID:      clientID,
		ScopeKey:      scopeKey,
		TokenEndpoint: strings.TrimSpace(stringFromAny(entry["token_endpoint"])),
		AuthEntry:     authEntry,
		Source:        source,
		SourcePath:    sourcePath,
		ExpiresAt:     formatOptionalGrokTime(expiresAt),
		CreatedAt:     now,
		UpdatedAt:     now,
	}, true
}

func (s *GrokBuildService) ApplyOAuthAccount(accountID string) (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if err := s.applyOAuthAccountLocked(strings.TrimSpace(accountID), state); err != nil {
		return GrokRuntimeStatus{}, err
	}
	return s.getStatusLocked()
}

func (s *GrokBuildService) applyOAuthAccountLocked(accountID string, state grokRuntimeState) error {
	store, err := loadGrokOAuthStore()
	if err != nil {
		return err
	}
	index := grokOAuthAccountIndex(store.Accounts, accountID)
	if index < 0 {
		return fmt.Errorf("Grok OAuth 账号不存在: %s", accountID)
	}
	paths, err := resolveGrokConfigPaths(state)
	if err != nil {
		return err
	}
	oldConfig, oldConfigExisted, err := readOptionalGrokFile(paths.ConfigPath)
	if err != nil {
		return fmt.Errorf("读取 Grok config.toml 失败: %w", err)
	}
	oldAuth, oldAuthExisted, err := readOptionalGrokFile(paths.AuthPath)
	if err != nil {
		return fmt.Errorf("读取 Grok auth.json 失败: %w", err)
	}
	oldStateData, oldStateExisted, err := readOptionalGrokFile(grokRuntimeStatePath())
	if err != nil {
		return fmt.Errorf("读取 Grok 运行状态失败: %w", err)
	}
	if state.AppliedAccountID != "" {
		syncGrokAppliedAccountFromLive(&store, state.AppliedAccountID, oldAuth)
	}
	account := store.Accounts[index]
	account, err = s.ensureFreshGrokAccount(context.Background(), account, false)
	if err != nil {
		store.Accounts[index] = account
		_ = saveGrokOAuthStore(store)
		return err
	}
	store.Accounts[index] = account
	mergedAuth, err := mergeGrokAccountIntoAuth(oldAuth, account)
	if err != nil {
		return err
	}
	state.AppliedAccountID = accountID
	if _, err := applyGrokConfigMode(state, GrokModeOAuth, ""); err != nil {
		return err
	}
	rollback := func() {
		_ = restoreOptionalGrokFile(paths.ConfigPath, oldConfig, oldConfigExisted)
		_ = restoreOptionalGrokFile(paths.AuthPath, oldAuth, oldAuthExisted)
		_ = restoreOptionalGrokFile(grokRuntimeStatePath(), oldStateData, oldStateExisted)
	}
	if err := os.MkdirAll(filepath.Dir(paths.AuthPath), 0o700); err != nil {
		rollback()
		return fmt.Errorf("创建 Grok auth.json 目录失败，配置已回滚: %w", err)
	}
	if err := AtomicWriteBytes(paths.AuthPath, mergedAuth); err != nil {
		rollback()
		return fmt.Errorf("写入 Grok auth.json 失败，配置已回滚: %w", err)
	}
	if err := saveGrokOAuthStore(store); err != nil {
		rollback()
		return fmt.Errorf("保存 Grok OAuth 账号失败，配置已回滚: %w", err)
	}
	return nil
}

func (s *GrokBuildService) RemoveOAuthAccount(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return err
	}
	if state.AppliedAccountID == accountID {
		return fmt.Errorf("不能删除当前已应用账号；请先切换账号或停用接管")
	}
	store, err := loadGrokOAuthStore()
	if err != nil {
		return err
	}
	index := grokOAuthAccountIndex(store.Accounts, accountID)
	if index < 0 {
		return fmt.Errorf("Grok OAuth 账号不存在: %s", accountID)
	}
	store.Accounts = append(store.Accounts[:index], store.Accounts[index+1:]...)
	if err := saveGrokOAuthStore(store); err != nil {
		return fmt.Errorf("删除 Grok OAuth 账号失败: %w", err)
	}
	return nil
}

func (s *GrokBuildService) RefreshOAuthAccount(accountID string) (GrokOAuthAccountDTO, error) {
	return s.refreshOAuthAccount(accountID, true, false)
}

func (s *GrokBuildService) RefreshOAuthQuota(accountID string, force bool) (GrokOAuthAccountDTO, error) {
	return s.refreshOAuthAccount(accountID, force, true)
}

func (s *GrokBuildService) refreshOAuthAccount(accountID string, force, withQuota bool) (GrokOAuthAccountDTO, error) {
	s.mu.Lock()
	store, err := loadGrokOAuthStore()
	if err != nil {
		s.mu.Unlock()
		return GrokOAuthAccountDTO{}, err
	}
	index := grokOAuthAccountIndex(store.Accounts, accountID)
	if index < 0 {
		s.mu.Unlock()
		return GrokOAuthAccountDTO{}, fmt.Errorf("Grok OAuth 账号不存在: %s", accountID)
	}
	account := store.Accounts[index]
	state, err := loadGrokRuntimeState()
	s.mu.Unlock()
	if err != nil {
		return GrokOAuthAccountDTO{}, err
	}
	if withQuota && !force && grokQuotaFresh(account.Quota) {
		return grokOAuthAccountDTO(account, state.AppliedAccountID), nil
	}
	account, refreshErr := s.ensureFreshGrokAccount(context.Background(), account, !withQuota && force)
	if refreshErr == nil && withQuota {
		account, refreshErr = s.fetchGrokQuota(context.Background(), account)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	latest, loadErr := loadGrokOAuthStore()
	if loadErr != nil {
		return GrokOAuthAccountDTO{}, loadErr
	}
	latestIndex := grokOAuthAccountIndex(latest.Accounts, accountID)
	if latestIndex < 0 {
		return GrokOAuthAccountDTO{}, fmt.Errorf("Grok OAuth 账号已被删除: %s", accountID)
	}
	latest.Accounts[latestIndex] = account
	if err := s.persistGrokAccountLocked(latest, account, state.AppliedAccountID == accountID); err != nil {
		return GrokOAuthAccountDTO{}, err
	}
	if refreshErr != nil {
		return grokOAuthAccountDTO(account, state.AppliedAccountID), refreshErr
	}
	return grokOAuthAccountDTO(account, state.AppliedAccountID), nil
}

func (s *GrokBuildService) RefreshAllOAuthQuotas(force bool) []GrokOAuthRefreshResult {
	accounts, err := s.ListOAuthAccounts()
	if err != nil {
		return []GrokOAuthRefreshResult{{Success: false, Error: err.Error()}}
	}
	results := make([]GrokOAuthRefreshResult, len(accounts))
	semaphore := make(chan struct{}, 2)
	done := make(chan int, len(accounts))
	for index, account := range accounts {
		go func(index int, accountID string) {
			semaphore <- struct{}{}
			refreshed, refreshErr := s.RefreshOAuthQuota(accountID, force)
			<-semaphore
			result := GrokOAuthRefreshResult{AccountID: accountID, Success: refreshErr == nil}
			if refreshErr != nil {
				result.Error = refreshErr.Error()
			} else {
				result.Account = &refreshed
			}
			results[index] = result
			done <- index
		}(index, account.ID)
	}
	for range accounts {
		<-done
	}
	return results
}

func (s *GrokBuildService) persistGrokAccountLocked(store grokOAuthStore, account grokOAuthAccount, applied bool) error {
	if !applied {
		return saveGrokOAuthStore(store)
	}
	state, err := loadGrokRuntimeState()
	if err != nil {
		return err
	}
	paths, err := resolveGrokConfigPaths(state)
	if err != nil {
		return err
	}
	oldAuth, oldAuthExisted, err := readOptionalGrokFile(paths.AuthPath)
	if err != nil {
		return fmt.Errorf("读取 Grok auth.json 失败: %w", err)
	}
	merged, err := mergeGrokAccountIntoAuth(oldAuth, account)
	if err != nil {
		return err
	}
	if err := AtomicWriteBytes(paths.AuthPath, merged); err != nil {
		return fmt.Errorf("更新 Grok auth.json 失败: %w", err)
	}
	if err := saveGrokOAuthStore(store); err != nil {
		_ = restoreOptionalGrokFile(paths.AuthPath, oldAuth, oldAuthExisted)
		return fmt.Errorf("保存刷新后的 Grok OAuth 账号失败，auth.json 已回滚: %w", err)
	}
	return nil
}

func (s *GrokBuildService) ensureFreshGrokAccount(ctx context.Context, account grokOAuthAccount, force bool) (grokOAuthAccount, error) {
	expiresAt := parseGrokTime(account.ExpiresAt)
	if !force && !expiresAt.IsZero() && time.Until(expiresAt) > 30*time.Minute {
		return account, nil
	}
	refreshToken := strings.TrimSpace(stringFromAny(account.AuthEntry["refresh_token"]))
	if refreshToken == "" {
		account.NeedsRelogin = true
		account.LastError = "账号没有 refresh_token，需要重新进行 Device Code 登录"
		account.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return account, errors.New(account.LastError)
	}
	endpoint := strings.TrimSpace(account.TokenEndpoint)
	if endpoint == "" {
		endpoint = strings.TrimRight(account.Issuer, "/") + "/oauth2/token"
	}
	if err := validateGrokXAIEndpoint(endpoint); err != nil {
		account.LastError = err.Error()
		return account, err
	}
	client, err := s.newHTTPClient(30 * time.Second)
	if err != nil {
		return account, err
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {firstNonEmptyGrok(account.ClientID, grokXAIClientID)},
		"refresh_token": {refreshToken},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return account, fmt.Errorf("创建 xAI Token 刷新请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		account.LastError = fmt.Sprintf("xAI Token 刷新失败: %v", err)
		return account, errors.New(account.LastError)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, grokOAuthMaxFileSize))
	if err != nil {
		return account, fmt.Errorf("读取 xAI Token 刷新响应失败: %w", err)
	}
	var token grokTokenResponse
	_ = json.Unmarshal(body, &token)
	if response.StatusCode < 200 || response.StatusCode >= 300 || token.Error != "" || strings.TrimSpace(token.AccessToken) == "" {
		message := formatGrokTokenError(response.StatusCode, token)
		account.LastError = message
		account.NeedsRelogin = token.Error == "invalid_grant" || response.StatusCode == http.StatusBadRequest
		account.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return account, errors.New(message)
	}
	now := time.Now().UTC()
	account.AuthEntry["key"] = strings.TrimSpace(token.AccessToken)
	if strings.TrimSpace(token.RefreshToken) != "" {
		account.AuthEntry["refresh_token"] = strings.TrimSpace(token.RefreshToken)
	}
	expiresAt = grokJWTExpiry(token.AccessToken)
	if expiresAt.IsZero() && token.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if !expiresAt.IsZero() {
		account.ExpiresAt = expiresAt.Format(time.RFC3339Nano)
		account.AuthEntry["expires_at"] = account.ExpiresAt
	}
	account.TokenEndpoint = endpoint
	account.NeedsRelogin = false
	account.LastError = ""
	account.UpdatedAt = now.Format(time.RFC3339Nano)
	return account, nil
}

func (s *GrokBuildService) fetchGrokQuota(ctx context.Context, account grokOAuthAccount) (grokOAuthAccount, error) {
	accessToken := grokAccessToken(account.AuthEntry)
	if accessToken == "" {
		return account, fmt.Errorf("Grok OAuth 账号缺少 access token")
	}
	client, err := s.newHTTPClient(20 * time.Second)
	if err != nil {
		return account, err
	}
	type endpointResult struct {
		body map[string]any
		err  error
	}
	fetch := func(path string) endpointResult {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, grokCLIProxyBase+path, nil)
		if requestErr != nil {
			return endpointResult{err: requestErr}
		}
		request.Header.Set("Authorization", "Bearer "+accessToken)
		request.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
		request.Header.Set("x-grok-client-version", grokCLIClientVersion)
		request.Header.Set("x-grok-client-identifier", "grok-shell")
		request.Header.Set("x-grok-client-mode", "headless")
		request.Header.Set("User-Agent", grokCLIUserAgent)
		request.Header.Set("Accept", "application/json")
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return endpointResult{err: requestErr}
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return endpointResult{err: fmt.Errorf("HTTP %d", response.StatusCode)}
		}
		var body map[string]any
		decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
		decoder.UseNumber()
		if decodeErr := decoder.Decode(&body); decodeErr != nil {
			return endpointResult{err: fmt.Errorf("解析响应失败: %w", decodeErr)}
		}
		return endpointResult{body: body}
	}
	credits := fetch("/billing?format=credits")
	monthly := fetch("/billing")
	user := fetch("/user?include=subscription")
	quota := account.Quota
	quota.LastAttemptAt = time.Now().UTC().Format(time.RFC3339Nano)
	errorsList := make([]string, 0, 3)
	successes := 0
	if credits.err == nil {
		successes++
		root := grokBillingRoot(credits.body)
		if used, ok := grokJSONNumber(firstGrokValue(root, "creditUsagePercent", "credit_usage_percent")); ok && used >= 0 && used <= 100 {
			remaining := clampGrokPercent(100 - used)
			quota.WeeklyRemainingPercent = &remaining
		} else {
			quota.WeeklyRemainingPercent = nil
		}
		quota.WeeklyResetAt = grokNestedTime(root, []string{"currentPeriod", "current_period"}, []string{"end"})
	} else {
		errorsList = append(errorsList, "周额度: "+credits.err.Error())
	}
	if monthly.err == nil {
		successes++
		root := grokBillingRoot(monthly.body)
		limit, hasLimit := grokJSONNumber(firstGrokValue(root, "monthlyLimit", "monthly_limit"))
		used, hasUsed := grokJSONNumber(firstGrokValue(root, "used", "totalUsed", "includedUsed"))
		if hasLimit && hasUsed && limit > 0 {
			remaining := clampGrokPercent((limit - used) / limit * 100)
			quota.MonthlyRemainingPercent = &remaining
			quota.MonthlyResetAt = grokTimeString(firstGrokValue(root, "billingPeriodEnd", "billing_period_end"))
		} else {
			quota.MonthlyRemainingPercent = nil
			quota.MonthlyResetAt = ""
		}
		if quota.PlanType == "" {
			quota.PlanType = grokPlanType(monthly.body)
		}
	} else {
		errorsList = append(errorsList, "月额度: "+monthly.err.Error())
	}
	if user.err == nil {
		successes++
		if plan := grokPlanType(user.body); plan != "" {
			quota.PlanType = plan
		}
	} else {
		errorsList = append(errorsList, "套餐: "+user.err.Error())
	}
	if quota.PlanType == "" {
		quota.PlanType = grokPlanFromJWT(accessToken)
	}
	if successes == 0 {
		quota.LastError = strings.Join(errorsList, "; ")
		account.Quota = quota
		account.LastError = quota.LastError
		return account, errors.New(quota.LastError)
	}
	quota.DataUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	quota.LastError = strings.Join(errorsList, "; ")
	account.Quota = quota
	account.LastError = quota.LastError
	account.UpdatedAt = quota.DataUpdatedAt
	return account, nil
}

func (s *GrokBuildService) StartDeviceCode() (GrokDeviceAuthStartResult, error) {
	client, err := s.newHTTPClient(30 * time.Second)
	if err != nil {
		return GrokDeviceAuthStartResult{}, err
	}
	var discovery grokOIDCDiscovery
	if err := getGrokJSON(context.Background(), client, grokXAIDiscoveryURL, &discovery); err != nil {
		return GrokDeviceAuthStartResult{}, fmt.Errorf("读取 xAI OIDC 配置失败: %w", err)
	}
	for field, endpoint := range map[string]string{
		"issuer": discovery.Issuer, "device_authorization_endpoint": discovery.DeviceAuthorizationEndpoint,
		"token_endpoint": discovery.TokenEndpoint, "userinfo_endpoint": discovery.UserinfoEndpoint,
	} {
		if err := validateGrokXAIEndpoint(endpoint); err != nil {
			return GrokDeviceAuthStartResult{}, fmt.Errorf("xAI %s 无效: %w", field, err)
		}
	}
	form := url.Values{"client_id": {grokXAIClientID}, "scope": {grokXAIScope}}
	request, err := http.NewRequest(http.MethodPost, discovery.DeviceAuthorizationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return GrokDeviceAuthStartResult{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return GrokDeviceAuthStartResult{}, fmt.Errorf("请求 xAI Device Code 失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return GrokDeviceAuthStartResult{}, fmt.Errorf("请求 xAI Device Code 返回 HTTP %d", response.StatusCode)
	}
	var device grokDeviceCodeResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, grokOAuthMaxFileSize)).Decode(&device); err != nil {
		return GrokDeviceAuthStartResult{}, fmt.Errorf("解析 xAI Device Code 响应失败: %w", err)
	}
	if strings.TrimSpace(device.DeviceCode) == "" || strings.TrimSpace(device.UserCode) == "" || strings.TrimSpace(device.VerificationURI) == "" {
		return GrokDeviceAuthStartResult{}, fmt.Errorf("xAI Device Code 响应缺少必要字段")
	}
	sessionID, err := newGrokOAuthID("device")
	if err != nil {
		return GrokDeviceAuthStartResult{}, err
	}
	interval := device.Interval
	if interval < 5 {
		interval = 5
	}
	expiresAt := time.Now().UTC().Add(time.Duration(device.ExpiresIn) * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	session := &grokDeviceSession{
		status: GrokDeviceAuthStatus{SessionID: sessionID, Status: "waiting_for_user", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		cancel: cancel,
	}
	s.sessionMu.Lock()
	for _, existing := range s.sessions {
		if existing.status.Status == "waiting_for_user" || existing.status.Status == "polling" {
			s.sessionMu.Unlock()
			cancel()
			return GrokDeviceAuthStartResult{}, fmt.Errorf("已有 Grok Device Code 登录正在进行")
		}
	}
	s.sessions[sessionID] = session
	s.sessionMu.Unlock()
	go s.pollGrokDeviceCode(ctx, client, sessionID, discovery, device.DeviceCode, interval, expiresAt)
	return GrokDeviceAuthStartResult{
		SessionID: sessionID, VerificationURI: device.VerificationURI,
		VerificationURIComplete: device.VerificationURIComplete, UserCode: device.UserCode,
		ExpiresAt: expiresAt.Format(time.RFC3339Nano), PollIntervalSeconds: interval,
	}, nil
}

func (s *GrokBuildService) GetDeviceCodeStatus(sessionID string) (GrokDeviceAuthStatus, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return GrokDeviceAuthStatus{}, fmt.Errorf("Grok Device Code 会话不存在: %s", sessionID)
	}
	return session.status, nil
}

func (s *GrokBuildService) CancelDeviceCode(sessionID string) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("Grok Device Code 会话不存在: %s", sessionID)
	}
	if session.cancel != nil {
		session.cancel()
	}
	session.status.Status = "cancelled"
	session.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return nil
}

func (s *GrokBuildService) pollGrokDeviceCode(ctx context.Context, client *http.Client, sessionID string, discovery grokOIDCDiscovery, deviceCode string, interval int, expiresAt time.Time) {
	update := func(status, message, accountID string) {
		s.sessionMu.Lock()
		defer s.sessionMu.Unlock()
		if session, ok := s.sessions[sessionID]; ok {
			session.status.Status = status
			session.status.Message = message
			session.status.AccountID = accountID
			session.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	pollInterval := time.Duration(interval) * time.Second
	for {
		select {
		case <-ctx.Done():
			update("cancelled", "", "")
			return
		case <-time.After(pollInterval):
		}
		if time.Now().UTC().After(expiresAt) {
			update("expired", "Device Code 已过期", "")
			return
		}
		update("polling", "", "")
		form := url.Values{"grant_type": {grokDeviceGrantType}, "device_code": {deviceCode}, "client_id": {grokXAIClientID}}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.TokenEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			update("failed", err.Error(), "")
			return
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(request)
		if err != nil {
			update("failed", fmt.Sprintf("xAI Device Token 请求失败: %v", err), "")
			return
		}
		var token grokTokenResponse
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, grokOAuthMaxFileSize)).Decode(&token)
		response.Body.Close()
		if decodeErr != nil {
			update("failed", fmt.Sprintf("解析 xAI Device Token 响应失败: %v", decodeErr), "")
			return
		}
		switch token.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			pollInterval += 5 * time.Second
			continue
		case "expired_token":
			update("expired", "Device Code 已过期", "")
			return
		case "access_denied":
			update("denied", "用户拒绝了授权", "")
			return
		case "":
		default:
			update("failed", formatGrokTokenError(response.StatusCode, token), "")
			return
		}
		account, err := s.grokAccountFromDeviceToken(ctx, client, discovery, token)
		if err != nil {
			update("failed", err.Error(), "")
			return
		}
		s.mu.Lock()
		store, loadErr := loadGrokOAuthStore()
		if loadErr == nil {
			if index := grokOAuthAccountIndex(store.Accounts, account.ID); index >= 0 {
				store.Accounts[index] = mergeImportedGrokAccount(store.Accounts[index], account)
			} else {
				store.Accounts = append(store.Accounts, account)
			}
			loadErr = saveGrokOAuthStore(store)
		}
		s.mu.Unlock()
		if loadErr != nil {
			update("failed", fmt.Sprintf("保存 Grok OAuth 账号失败: %v", loadErr), "")
			return
		}
		update("completed", "", account.ID)
		return
	}
}

func (s *GrokBuildService) grokAccountFromDeviceToken(ctx context.Context, client *http.Client, discovery grokOIDCDiscovery, token grokTokenResponse) (grokOAuthAccount, error) {
	if strings.TrimSpace(token.AccessToken) == "" {
		return grokOAuthAccount{}, fmt.Errorf("xAI Device Token 响应缺少 access_token")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery.UserinfoEndpoint, nil)
	if err != nil {
		return grokOAuthAccount{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return grokOAuthAccount{}, fmt.Errorf("xAI userinfo 请求失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return grokOAuthAccount{}, fmt.Errorf("xAI userinfo 返回 HTTP %d", response.StatusCode)
	}
	var user map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, grokOAuthMaxFileSize)).Decode(&user); err != nil {
		return grokOAuthAccount{}, fmt.Errorf("解析 xAI userinfo 失败: %w", err)
	}
	now := time.Now().UTC()
	expiresAt := grokJWTExpiry(token.AccessToken)
	if expiresAt.IsZero() && token.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	entry := map[string]any{
		"key": token.AccessToken, "auth_mode": "oidc", "oidc_issuer": strings.TrimRight(discovery.Issuer, "/"),
		"oidc_client_id": grokXAIClientID, "create_time": now.Format(time.RFC3339Nano),
	}
	if token.RefreshToken != "" {
		entry["refresh_token"] = token.RefreshToken
	}
	if !expiresAt.IsZero() {
		entry["expires_at"] = expiresAt.Format(time.RFC3339Nano)
	}
	for source, target := range map[string]string{"sub": "user_id", "email": "email", "given_name": "first_name", "picture": "profile_image_asset_id"} {
		if value := strings.TrimSpace(stringFromAny(user[source])); value != "" {
			entry[target] = value
		}
	}
	account, ok := grokAccountFromEntry(entry, "device-code", "", grokAuthScopeKey(discovery.Issuer, grokXAIClientID), now.Format(time.RFC3339Nano))
	if !ok {
		return grokOAuthAccount{}, fmt.Errorf("无法构造 Grok OAuth 账号")
	}
	account.TokenEndpoint = discovery.TokenEndpoint
	return account, nil
}

func mergeGrokAccountIntoAuth(current []byte, account grokOAuthAccount) ([]byte, error) {
	root := make(map[string]any)
	if len(strings.TrimSpace(string(current))) > 0 {
		decoder := json.NewDecoder(strings.NewReader(string(current)))
		decoder.UseNumber()
		if err := decoder.Decode(&root); err != nil {
			return nil, fmt.Errorf("Grok auth.json 解析失败: %w", err)
		}
	}
	for key, raw := range root {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		issuer := strings.TrimRight(firstGrokString(entry, "oidc_issuer", "issuer"), "/")
		clientID := firstGrokString(entry, "oidc_client_id", "client_id")
		if strings.EqualFold(issuer, account.Issuer) && clientID == account.ClientID && key != account.ScopeKey {
			delete(root, key)
		}
	}
	selected := cloneGrokJSONMap(account.AuthEntry)
	if existing, ok := root[account.ScopeKey].(map[string]any); ok && sameGrokIdentity(existing, account.AuthEntry) {
		merged := cloneGrokJSONMap(existing)
		for key, value := range selected {
			merged[key] = value
		}
		selected = merged
	}
	root[account.ScopeKey] = selected
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 Grok auth.json 失败: %w", err)
	}
	return append(encoded, '\n'), nil
}

func syncGrokAppliedAccountFromLive(store *grokOAuthStore, accountID string, authData []byte) {
	index := grokOAuthAccountIndex(store.Accounts, accountID)
	if index < 0 || len(strings.TrimSpace(string(authData))) == 0 {
		return
	}
	var root map[string]any
	if json.Unmarshal(authData, &root) != nil {
		return
	}
	account := &store.Accounts[index]
	live, ok := root[account.ScopeKey].(map[string]any)
	if !ok || !sameGrokIdentity(live, account.AuthEntry) {
		return
	}
	for _, key := range []string{"key", "access_token", "refresh_token", "expires_at"} {
		if value, exists := live[key]; exists {
			account.AuthEntry[key] = value
		}
	}
	account.ExpiresAt = formatOptionalGrokTime(grokEntryExpiry(account.AuthEntry))
	account.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

func sameGrokIdentity(left, right map[string]any) bool {
	leftSubject := firstGrokString(left, "user_id", "principal_id", "sub")
	rightSubject := firstGrokString(right, "user_id", "principal_id", "sub")
	if leftSubject != "" && rightSubject != "" {
		return leftSubject == rightSubject
	}
	leftEmail := strings.ToLower(strings.TrimSpace(stringFromAny(left["email"])))
	rightEmail := strings.ToLower(strings.TrimSpace(stringFromAny(right["email"])))
	return leftEmail != "" && leftEmail == rightEmail
}

func grokOAuthAccountIndex(accounts []grokOAuthAccount, accountID string) int {
	for index := range accounts {
		if accounts[index].ID == accountID {
			return index
		}
	}
	return -1
}

func validateGrokXAIEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" {
		return fmt.Errorf("xAI OAuth 地址必须使用 HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "x.ai" && !strings.HasSuffix(host, ".x.ai") {
		return fmt.Errorf("xAI OAuth 地址必须位于 x.ai 域名")
	}
	return nil
}

func getGrokJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, grokOAuthMaxFileSize)).Decode(target)
}

func formatGrokTokenError(status int, token grokTokenResponse) string {
	code := strings.TrimSpace(token.Error)
	description := strings.TrimSpace(token.ErrorDescription)
	if len(description) > 300 {
		description = description[:300]
	}
	if code == "invalid_grant" {
		return fmt.Sprintf("xAI refresh token 已失效或被撤销（HTTP %d, invalid_grant），需要重新进行 Device Code 登录", status)
	}
	if code != "" && description != "" {
		return fmt.Sprintf("xAI Token 请求失败（HTTP %d, %s）: %s", status, code, description)
	}
	if code != "" {
		return fmt.Sprintf("xAI Token 请求失败（HTTP %d, %s）", status, code)
	}
	return fmt.Sprintf("xAI Token 请求失败（HTTP %d）", status)
}

func grokQuotaFresh(quota GrokOAuthQuota) bool {
	updated := parseGrokTime(quota.DataUpdatedAt)
	return !updated.IsZero() && time.Since(updated) < 10*time.Minute
}

func grokBillingRoot(body map[string]any) map[string]any {
	if config, ok := body["config"].(map[string]any); ok {
		return config
	}
	return body
}

func grokPlanType(body map[string]any) string {
	for _, source := range []map[string]any{body, grokBillingRoot(body)} {
		if value := firstGrokString(source, "subscriptionTier", "subscription_tier", "planName", "plan_name"); value != "" {
			return value
		}
		if user, ok := source["user"].(map[string]any); ok {
			if value := firstGrokString(user, "subscriptionTier", "subscription_tier"); value != "" {
				return value
			}
		}
		for _, key := range []string{"subscription", "plan", "membership"} {
			if nested, ok := source[key].(map[string]any); ok {
				if value := firstGrokString(nested, "name", "displayName", "display_name", "code", "tier"); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func grokPlanFromJWT(token string) string {
	value := stringFromAny(decodeGrokJWTClaims(token)["tier"])
	if number, err := strconv.Atoi(value); err == nil {
		return map[int]string{0: "free", 1: "supergrok", 2: "x_basic", 3: "x_premium", 4: "x_premium_plus", 5: "supergrok_heavy", 6: "supergrok_lite"}[number]
	}
	return value
}

func grokNestedTime(root map[string]any, parentKeys, childKeys []string) string {
	for _, parent := range parentKeys {
		if nested, ok := root[parent].(map[string]any); ok {
			if value := grokTimeString(firstGrokValue(nested, childKeys...)); value != "" {
				return value
			}
		}
	}
	return ""
}

func grokTimeString(value any) string {
	parsed := parseGrokTime(stringFromAny(value))
	return formatOptionalGrokTime(parsed)
}

func clampGrokPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func grokJSONNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	case map[string]any:
		return grokJSONNumber(typed["val"])
	default:
		return 0, false
	}
}

func firstGrokValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func firstGrokString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringFromAny(values[key])); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyGrok(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func parseGrokTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		if unix > 1e12 {
			unix /= 1000
		}
		return time.Unix(unix, 0).UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func formatOptionalGrokTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func decodeGrokJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	claims := make(map[string]any)
	if decoder.Decode(&claims) != nil {
		return map[string]any{}
	}
	return claims
}

func grokJWTExpiry(token string) time.Time {
	claims := decodeGrokJWTClaims(token)
	unix, ok := grokJSONNumber(claims["exp"])
	if !ok {
		return time.Time{}
	}
	return time.Unix(int64(unix), 0).UTC()
}

func grokEntryExpiry(entry map[string]any) time.Time {
	expiresAt := parseGrokTime(stringFromAny(entry["expires_at"]))
	if expiresAt.IsZero() {
		expiresAt = grokJWTExpiry(grokAccessToken(entry))
	}
	return expiresAt
}

func grokAccessToken(entry map[string]any) string {
	return firstGrokString(entry, "key", "access_token")
}

func grokAuthScopeKey(issuer, clientID string) string {
	return strings.TrimRight(strings.TrimSpace(issuer), "/") + "::" + strings.TrimSpace(clientID)
}

func cloneGrokJSONMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func newGrokOAuthID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成 Grok OAuth 标识失败: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(buffer), nil
}
