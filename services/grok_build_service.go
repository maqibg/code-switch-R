package services

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GrokBuildService struct {
	relayAddr       string
	appSettings     *AppSettingsService
	providerService *ProviderService
	mu              sync.Mutex
	sessionMu       sync.Mutex
	sessions        map[string]*grokDeviceSession
	httpClient      *http.Client
}

func NewGrokBuildService(relayAddr string, appSettings *AppSettingsService, providerService *ProviderService) *GrokBuildService {
	return &GrokBuildService{
		relayAddr:       strings.TrimSpace(relayAddr),
		appSettings:     appSettings,
		providerService: providerService,
		sessions:        make(map[string]*grokDeviceSession),
	}
}

func (s *GrokBuildService) Start() error { return nil }
func (s *GrokBuildService) Stop() error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	for _, session := range s.sessions {
		if session.cancel != nil {
			session.cancel()
		}
	}
	return nil
}

func (s *GrokBuildService) relayBaseURL() string {
	address := strings.TrimRight(strings.TrimSpace(s.relayAddr), "/")
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return address + "/grok/v1"
}

func (s *GrokBuildService) newHTTPClient(timeout time.Duration) (*http.Client, error) {
	if s.httpClient != nil {
		return s.httpClient, nil
	}
	if s.appSettings == nil {
		return &http.Client{Timeout: timeout}, nil
	}
	proxy, err := s.appSettings.GetGlobalProxyConfig()
	if err != nil {
		return nil, fmt.Errorf("读取全局代理设置失败: %w", err)
	}
	client, err := NewHTTPClientWithProxy(timeout, nil, proxy)
	if err != nil {
		return nil, fmt.Errorf("创建 Grok OAuth HTTP 客户端失败: %w", err)
	}
	return client, nil
}

func (s *GrokBuildService) GetStatus() (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getStatusLocked()
}

func (s *GrokBuildService) getStatusLocked() (GrokRuntimeStatus, error) {
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	paths, err := resolveGrokConfigPaths(state)
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	status := GrokRuntimeStatus{
		Mode:             state.Mode,
		AppliedAccountID: state.AppliedAccountID,
		ConfigPath:       paths.ConfigPath,
		AuthPath:         paths.AuthPath,
		CustomDirectory:  paths.CustomDirectory,
		Managed:          state.Mode != GrokModeUnmanaged,
	}
	if !status.Managed {
		return status, nil
	}
	if state.TargetConfigPath == "" || filepath.Clean(state.TargetConfigPath) != filepath.Clean(paths.ConfigPath) {
		status.Conflict = true
		status.ConflictMessage = fmt.Sprintf("Grok 配置路径已从 %s 变为 %s", state.TargetConfigPath, paths.ConfigPath)
		return status, nil
	}
	current, _, readErr := readOptionalGrokFile(paths.ConfigPath)
	if readErr != nil {
		return GrokRuntimeStatus{}, fmt.Errorf("读取 Grok config.toml 失败: %w", readErr)
	}
	if conflictErr := grokConfigConflict(current, state); conflictErr != nil {
		status.Conflict = true
		status.ConflictMessage = conflictErr.Error()
	}
	return status, nil
}

func (s *GrokBuildService) SetCustomDirectory(directory string) (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if state.Mode != GrokModeUnmanaged {
		return GrokRuntimeStatus{}, fmt.Errorf("请先停用或放弃当前 Grok 接管，再修改配置目录")
	}
	directory = strings.TrimSpace(directory)
	if directory != "" {
		directory, err = filepath.Abs(directory)
		if err != nil {
			return GrokRuntimeStatus{}, fmt.Errorf("解析 Grok 自定义目录失败: %w", err)
		}
	}
	state.CustomDirectory = directory
	if err := saveGrokRuntimeState(state); err != nil {
		return GrokRuntimeStatus{}, fmt.Errorf("保存 Grok 自定义目录失败: %w", err)
	}
	return s.getStatusLocked()
}

func (s *GrokBuildService) EnableRelay() (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireEligibleGrokRelayProvider(); err != nil {
		return GrokRuntimeStatus{}, err
	}
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if _, err := applyGrokConfigMode(state, GrokModeRelay, s.relayBaseURL()); err != nil {
		return GrokRuntimeStatus{}, err
	}
	return s.getStatusLocked()
}

func (s *GrokBuildService) requireEligibleGrokRelayProvider() error {
	if s.providerService == nil {
		return fmt.Errorf("Grok Provider 服务未初始化")
	}
	providers, err := s.providerService.LoadProviders("grok")
	if err != nil {
		return fmt.Errorf("读取 Grok Provider 失败: %w", err)
	}
	if !hasEligibleGrokRelayProvider(providers) {
		return fmt.Errorf("请先启用至少一个有效的 Grok Provider")
	}
	return nil
}

func hasEligibleGrokRelayProvider(providers []Provider) bool {
	for _, provider := range providers {
		if strings.TrimSpace(provider.Name) == "" || !ProviderEligibleForRelay(provider, "grok") {
			continue
		}
		if len(provider.ValidateConfiguration()) != 0 || len(validateGrokProviderConfiguration(provider)) != 0 {
			continue
		}
		return true
	}
	return false
}

func (s *GrokBuildService) DisableManagement() (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if state.Mode == GrokModeUnmanaged {
		return s.getStatusLocked()
	}
	if _, err := applyGrokConfigMode(state, GrokModeUnmanaged, ""); err != nil {
		return GrokRuntimeStatus{}, err
	}
	return s.getStatusLocked()
}

func (s *GrokBuildService) ReapplyCurrentMode() (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if state.Mode == GrokModeUnmanaged {
		return GrokRuntimeStatus{}, fmt.Errorf("Grok 当前未接管")
	}
	paths, err := resolveGrokConfigPaths(state)
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if state.TargetConfigPath == "" || filepath.Clean(state.TargetConfigPath) != filepath.Clean(paths.ConfigPath) {
		return GrokRuntimeStatus{}, fmt.Errorf("接管目标路径已变化；请先放弃旧路径接管，再选择新目录")
	}
	current, _, err := readOptionalGrokFile(paths.ConfigPath)
	if err != nil {
		return GrokRuntimeStatus{}, fmt.Errorf("读取 Grok config.toml 失败: %w", err)
	}
	state.InjectedFingerprint, err = grokManagedFingerprint(current)
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	if state.Mode == GrokModeOAuth {
		if strings.TrimSpace(state.AppliedAccountID) == "" {
			return GrokRuntimeStatus{}, fmt.Errorf("OAuth 模式缺少已应用账号")
		}
		if err := s.applyOAuthAccountLocked(state.AppliedAccountID, state); err != nil {
			return GrokRuntimeStatus{}, err
		}
	} else if _, err := applyGrokConfigMode(state, GrokModeRelay, s.relayBaseURL()); err != nil {
		return GrokRuntimeStatus{}, err
	}
	return s.getStatusLocked()
}

func (s *GrokBuildService) AbandonManagement() (GrokRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil {
		return GrokRuntimeStatus{}, err
	}
	clearGrokManagedState(&state)
	if err := saveGrokRuntimeState(state); err != nil {
		return GrokRuntimeStatus{}, fmt.Errorf("放弃 Grok 接管失败: %w", err)
	}
	return s.getStatusLocked()
}

func clearGrokManagedState(state *grokRuntimeState) {
	state.Mode = GrokModeUnmanaged
	state.AppliedAccountID = ""
	state.TargetConfigPath = ""
	state.TargetAuthPath = ""
	state.OriginalModelsSectionExisted = false
	state.OriginalDefaultExisted = false
	state.OriginalDefaultLine = ""
	state.InjectedFingerprint = ""
	state.CreatedAt = ""
}

func restoreOptionalGrokFile(path string, data []byte, existed bool) error {
	if existed {
		return AtomicWriteBytes(path, data)
	}
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
