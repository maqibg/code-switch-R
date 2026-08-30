package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var piBuiltinProviderIDs = []string{
	"anthropic", "openai", "google", "openrouter", "github-copilot", "codex", "claude", "mistral",
}

type PiRuntimeFileStatus struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	ModifiedAt  string `json:"modifiedAt,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Error       string `json:"error,omitempty"`
	ErrorLine   int    `json:"errorLine,omitempty"`
	ErrorColumn int    `json:"errorColumn,omitempty"`
}

type PiRuntimeAuthStatus struct {
	ProviderID string `json:"providerId"`
	Type       string `json:"type"`
	Configured bool   `json:"configured"`
}

type PiRuntimeSettings struct {
	DefaultProvider string   `json:"defaultProvider,omitempty"`
	DefaultModel    string   `json:"defaultModel,omitempty"`
	EnabledModels   []string `json:"enabledModels"`
}

type PiRuntimePlatform struct {
	ProviderID         string                 `json:"providerId"`
	Name               string                 `json:"name,omitempty"`
	API                string                 `json:"api,omitempty"`
	BaseURLConfigured  bool                   `json:"baseUrlConfigured"`
	APIKeyConfigured   bool                   `json:"apiKeyConfigured"`
	CredentialSource   string                 `json:"credentialSource,omitempty"`
	GatewayURL         string                 `json:"gatewayUrl,omitempty"`
	HeaderCount        int                    `json:"headerCount"`
	Builtin            bool                   `json:"builtin"`
	Managed            bool                   `json:"managed"`
	Conflict           bool                   `json:"conflict"`
	Manageable         bool                   `json:"manageable"`
	ManagementBlockers []string               `json:"managementBlockers"`
	Models             []PiModelsCatalogModel `json:"models"`
}

func piPlatformManagementBlockers(platform PiModelsCatalogTemplate) []string {
	blockers := make([]string, 0, 3)
	if _, supported := piManagedAPIs[strings.TrimSpace(platform.API)]; !supported {
		blockers = append(blockers, fmt.Sprintf("api=%q 暂不支持托管", platform.API))
	}
	if strings.TrimSpace(platform.BaseURL) == "" {
		blockers = append(blockers, "平台没有可导入的 baseUrl")
	}
	for _, model := range platform.Models {
		if strings.TrimSpace(model.BaseURL) != "" {
			blockers = append(blockers, fmt.Sprintf("模型 %q 配置了独立 baseUrl", model.ID))
		}
		if api := strings.TrimSpace(model.API); api != "" {
			if _, supported := piManagedAPIs[api]; !supported {
				blockers = append(blockers, fmt.Sprintf("模型 %q 使用了不受网关支持的 api=%q", model.ID, api))
			}
		}
	}
	return blockers
}

type PiRuntimeSupplier struct {
	ID                  int64    `json:"id"`
	Name                string   `json:"name"`
	PlatformID          string   `json:"platformId"`
	Enabled             bool     `json:"enabled"`
	Level               int      `json:"level"`
	ModelCount          int      `json:"modelCount"`
	URLConfigured       bool     `json:"urlConfigured"`
	KeyConfigured       bool     `json:"keyConfigured"`
	URLHost             string   `json:"urlHost,omitempty"`
	Protocol            string   `json:"protocol,omitempty"`
	IdentityTemplateID  string   `json:"identityTemplateId,omitempty"`
	IdentityName        string   `json:"identityName,omitempty"`
	IdentityMode        string   `json:"identityMode,omitempty"`
	TargetCLI           string   `json:"targetCli,omitempty"`
	UserAgent           string   `json:"userAgent,omitempty"`
	IdentityHeaderNames []string `json:"identityHeaderNames"`
	MetadataMode        string   `json:"metadataMode,omitempty"`
	AuthScheme          string   `json:"authScheme,omitempty"`
	ModelIdentityCount  int      `json:"modelIdentityCount"`
}

type PiRuntimeSnapshot struct {
	ConfigDir            string                `json:"configDir"`
	Detected             bool                  `json:"detected"`
	Initialized          bool                  `json:"initialized"`
	Revision             string                `json:"revision"`
	LegacyGatewayTracked bool                  `json:"legacyGatewayTracked"`
	ModelsFile           PiRuntimeFileStatus   `json:"modelsFile"`
	AuthFile             PiRuntimeFileStatus   `json:"authFile"`
	SettingsFile         PiRuntimeFileStatus   `json:"settingsFile"`
	Settings             PiRuntimeSettings     `json:"settings"`
	Auth                 []PiRuntimeAuthStatus `json:"auth"`
	BuiltinProviders     []string              `json:"builtinProviders"`
	Platforms            []PiRuntimePlatform   `json:"platforms"`
	Suppliers            []PiRuntimeSupplier   `json:"suppliers"`
	DebugLogging         bool                  `json:"debugLogging"`
}

func (s *PiSettingsService) RuntimeSnapshot() (PiRuntimeSnapshot, error) {
	snapshot := PiRuntimeSnapshot{
		ConfigDir: s.configDir,
		Settings:  PiRuntimeSettings{EnabledModels: []string{}},
		Auth:      []PiRuntimeAuthStatus{}, Platforms: []PiRuntimePlatform{}, Suppliers: []PiRuntimeSupplier{},
		BuiltinProviders: append([]string(nil), piBuiltinProviderIDs...),
		DebugLogging:     piDebugLoggingEnabled(),
	}
	info, err := os.Stat(s.configDir)
	if errors.Is(err, os.ErrNotExist) {
		snapshot.ModelsFile = inspectPiRuntimeFile(s.modelsPath())
		snapshot.AuthFile = inspectPiRuntimeFile(s.authPath())
		snapshot.SettingsFile = inspectPiRuntimeFile(s.settingsPath())
		snapshot.Revision = s.currentRuntimeRevision()
		return snapshot, nil
	}
	if err != nil {
		return snapshot, fmt.Errorf("检查 Pi 配置目录失败: %w", err)
	}
	if !info.IsDir() {
		return snapshot, fmt.Errorf("Pi 配置路径不是目录: %s", s.configDir)
	}
	snapshot.Detected = true
	snapshot.ModelsFile = inspectPiRuntimeFile(s.modelsPath())
	snapshot.AuthFile = inspectPiRuntimeFile(s.authPath())
	snapshot.SettingsFile = inspectPiRuntimeFile(s.settingsPath())
	snapshot.LegacyGatewayTracked = fileExists(s.statePath)

	catalog, _ := s.ModelsCatalog()
	if catalog.Error != "" && snapshot.ModelsFile.Error == "" {
		snapshot.ModelsFile.Error = catalog.Error
		snapshot.ModelsFile.ErrorLine = catalog.ErrorLine
		snapshot.ModelsFile.ErrorColumn = catalog.ErrorColumn
	}
	snapshot.Initialized = catalog.Exists && len(catalog.Templates) > 0
	builtin := make(map[string]struct{}, len(piBuiltinProviderIDs))
	for _, id := range piBuiltinProviderIDs {
		builtin[id] = struct{}{}
	}
	for _, platform := range catalog.Templates {
		_, isBuiltin := builtin[platform.ProviderID]
		runtimePlatform := PiRuntimePlatform{
			ProviderID: platform.ProviderID, Name: platform.Name, API: platform.API,
			BaseURLConfigured: strings.TrimSpace(platform.BaseURL) != "", Builtin: isBuiltin,
			Managed: platform.Managed, Conflict: platform.Conflict, Models: platform.Models,
			ManagementBlockers: piPlatformManagementBlockers(platform),
			GatewayURL:         s.platformBaseURL(platform.ProviderID),
		}
		runtimePlatform.Manageable = len(runtimePlatform.ManagementBlockers) == 0
		snapshot.Platforms = append(snapshot.Platforms, runtimePlatform)
	}
	uiState, uiStateErr := s.loadUIState()
	if uiStateErr != nil {
		return snapshot, uiStateErr
	}
	snapshot.DebugLogging = uiState.DebugLogging
	setPiDebugLogging(uiState.DebugLogging)
	snapshot.Platforms = applyPiPlatformOrder(snapshot.Platforms, uiState.PlatformOrder)

	modelsRoot, _, modelsErr := readJSONObject(s.modelsPath())
	if modelsErr == nil {
		if providers, providerErr := nestedJSONObject(modelsRoot, "providers"); providerErr == nil {
			for index := range snapshot.Platforms {
				var source piModelsProviderFile
				if json.Unmarshal(providers[snapshot.Platforms[index].ProviderID], &source) == nil {
					snapshot.Platforms[index].APIKeyConfigured = strings.TrimSpace(source.APIKey) != ""
					if snapshot.Platforms[index].APIKeyConfigured {
						snapshot.Platforms[index].CredentialSource = "models.json"
					}
					snapshot.Platforms[index].HeaderCount = len(source.Headers)
				}
			}
		}
	}
	snapshot.Auth = readPiRuntimeAuth(s.authPath(), &snapshot.AuthFile)
	for index := range snapshot.Platforms {
		for _, auth := range snapshot.Auth {
			if auth.ProviderID == snapshot.Platforms[index].ProviderID && auth.Configured {
				snapshot.Platforms[index].CredentialSource = "auth.json"
				break
			}
		}
	}
	snapshot.Settings = readPiRuntimeSettings(s.settingsPath(), &snapshot.SettingsFile)
	if s.providerService != nil || s.providerLoader != nil {
		var providers []Provider
		var providerErr error
		if s.providerService != nil {
			providers, providerErr = s.providerService.loadProvidersRaw("pi")
		} else {
			providers, providerErr = s.providerLoader()
		}
		if providerErr != nil {
			return snapshot, fmt.Errorf("读取 Pi 上游供应商失败: %w", providerErr)
		}
		for _, provider := range providers {
			models := make(map[string]struct{})
			for _, model := range provider.PiModels {
				if id := strings.TrimSpace(model.ID); id != "" {
					models[id] = struct{}{}
				}
			}
			for id := range provider.ModelMapping {
				models[id] = struct{}{}
			}
			for id := range provider.PiModelOverrides {
				if id = strings.TrimSpace(id); id != "" {
					models[id] = struct{}{}
				}
			}
			identity := providerRequestIdentityForModel(provider, "")
			identityName := identity.Name
			if identityName == "" && identity.TargetCLI != "" && identity.TargetCLI != "inherit" {
				identityName = identity.TargetCLI
			}
			identityHeaders := make(map[string]string, len(identity.Headers)+2)
			_ = applyUserAgentIdentity(identityHeaders, identity)
			for key, value := range identity.Headers {
				if validateAdditionalHeader(key, value) == nil {
					setHeader(identityHeaders, key, value)
				}
			}
			identityHeaderNames := make([]string, 0, len(identityHeaders))
			for key := range identityHeaders {
				identityHeaderNames = append(identityHeaderNames, key)
			}
			sort.Strings(identityHeaderNames)
			authScheme, _ := provider.effectiveAuthScheme("pi")
			snapshot.Suppliers = append(snapshot.Suppliers, PiRuntimeSupplier{
				ID: provider.ID, Name: provider.Name, PlatformID: provider.PiPlatformKey(), Enabled: provider.Enabled,
				Level: provider.Level, ModelCount: len(models), URLConfigured: strings.TrimSpace(provider.APIURL) != "",
				KeyConfigured: strings.TrimSpace(provider.APIKey) != "",
				URLHost:       piSupplierURLHost(provider.APIURL), Protocol: provider.UpstreamProtocol,
				IdentityTemplateID: identity.TemplateID, IdentityName: identityName, IdentityMode: identity.Mode,
				TargetCLI: identity.TargetCLI, UserAgent: headerValue(identityHeaders, "User-Agent"),
				IdentityHeaderNames: identityHeaderNames, MetadataMode: identity.MetadataMode, AuthScheme: authScheme,
				ModelIdentityCount: len(provider.ModelRequestIdentities),
			})
		}
		platformOrder := make(map[string]int, len(snapshot.Platforms))
		for index, platform := range snapshot.Platforms {
			platformOrder[platform.ProviderID] = index
		}
		sort.SliceStable(snapshot.Suppliers, func(i, j int) bool {
			left, right := snapshot.Suppliers[i], snapshot.Suppliers[j]
			if platformOrder[left.PlatformID] != platformOrder[right.PlatformID] {
				return platformOrder[left.PlatformID] < platformOrder[right.PlatformID]
			}
			leftLevel, rightLevel := left.Level, right.Level
			if leftLevel <= 0 {
				leftLevel = 1
			}
			if rightLevel <= 0 {
				rightLevel = 1
			}
			return leftLevel < rightLevel
		})
	}
	snapshot.Revision = s.currentRuntimeRevision()
	return snapshot, nil
}

func piSupplierURLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (s *PiSettingsService) InitializeDefaultModels(expectedRevision string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.requireRuntimeRevision(expectedRevision); err != nil {
		return err
	}
	info, err := os.Stat(s.configDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("未检测到 Pi 配置目录: %s", s.configDir)
		}
		return fmt.Errorf("检查 Pi 配置目录失败: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("Pi 配置路径不是目录: %s", s.configDir)
	}
	data, readErr := os.ReadFile(s.modelsPath())
	if readErr == nil && strings.TrimSpace(string(data)) != "" {
		var root map[string]json.RawMessage
		if err := json.Unmarshal(stripJSONComments(data), &root); err != nil {
			return fmt.Errorf("现有 models.json 无法解析，拒绝覆盖: %w", err)
		}
		providers, providerErr := nestedJSONObject(root, "providers")
		if providerErr != nil && !errors.Is(providerErr, os.ErrNotExist) {
			return providerErr
		}
		if len(providers) > 0 {
			return fmt.Errorf("现有 models.json 已包含 Provider，拒绝用默认模板覆盖")
		}
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("读取 Pi models.json 失败: %w", readErr)
	}
	return writeDefaultPiModelsJSON(s.modelsPath())
}

func (s *PiSettingsService) MigrateLegacyGateway(expectedRevision string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.requireRuntimeRevision(expectedRevision); err != nil {
		return err
	}
	if !fileExists(s.statePath) {
		return nil
	}
	return s.DisableProxy()
}

func (s *PiSettingsService) settingsPath() string { return filepath.Join(s.configDir, "settings.json") }

func (s *PiSettingsService) requireRuntimeRevision(expected string) error {
	if strings.TrimSpace(expected) == "" || expected != s.currentRuntimeRevision() {
		return fmt.Errorf("Pi Runtime 配置已被外部修改，请刷新后重试")
	}
	return nil
}

func (s *PiSettingsService) currentRuntimeRevision() string {
	paths := []string{s.modelsPath(), s.authPath(), s.settingsPath(), s.platformStatePath, s.statePath}
	if appDir, err := getAppConfigDir(); err == nil {
		paths = append(paths, filepath.Join(appDir, "pi.json"))
	}
	hash := sha256.New()
	for _, path := range paths {
		hash.Write([]byte(path))
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			hash.Write([]byte("\x00missing"))
			continue
		}
		if err != nil {
			hash.Write([]byte("\x00error:" + err.Error()))
			continue
		}
		hash.Write([]byte("\x00present:"))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func inspectPiRuntimeFile(path string) PiRuntimeFileStatus {
	status := PiRuntimeFileStatus{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return status
	}
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	cleaned := stripJSONComments(data)
	status.Fingerprint = fingerprintPiModelsJSON(cleaned)
	if info, statErr := os.Stat(path); statErr == nil {
		status.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	var value any
	if err := json.Unmarshal(cleaned, &value); err != nil {
		status.Error = err.Error()
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			status.ErrorLine, status.ErrorColumn = jsonOffsetLineColumn(cleaned, syntaxError.Offset)
		}
	}
	return status
}

func readPiRuntimeAuth(path string, status *PiRuntimeFileStatus) []PiRuntimeAuthStatus {
	root, _, err := readJSONObject(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && status.Error == "" {
			status.Error = err.Error()
		}
		return []PiRuntimeAuthStatus{}
	}
	ids := make([]string, 0, len(root))
	for id := range root {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]PiRuntimeAuthStatus, 0, len(ids))
	for _, id := range ids {
		entryType := "unknown"
		var fields map[string]json.RawMessage
		if json.Unmarshal(root[id], &fields) == nil {
			_ = json.Unmarshal(fields["type"], &entryType)
		}
		result = append(result, PiRuntimeAuthStatus{ProviderID: id, Type: entryType, Configured: true})
	}
	return result
}

func readPiRuntimeSettings(path string, status *PiRuntimeFileStatus) PiRuntimeSettings {
	result := PiRuntimeSettings{EnabledModels: []string{}}
	root, _, err := readJSONObject(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && status.Error == "" {
			status.Error = err.Error()
		}
		return result
	}
	_ = json.Unmarshal(root["defaultProvider"], &result.DefaultProvider)
	_ = json.Unmarshal(root["defaultModel"], &result.DefaultModel)
	_ = json.Unmarshal(root["enabledModels"], &result.EnabledModels)
	if result.EnabledModels == nil {
		result.EnabledModels = []string{}
	}
	return result
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
