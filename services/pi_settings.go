package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	piGatewayProviderKey = "code-switch-r"
	piGatewayToken       = "code-switch-r-proxy"
	piProxyStateVersion  = 1
)

// PiGatewayProvider field order is the canonical order used for generated
// models.json content and the live editor preview.
type PiGatewayProvider struct {
	BaseURL        string                     `json:"baseUrl"`
	APIKey         string                     `json:"apiKey"`
	API            string                     `json:"api"`
	Headers        map[string]string          `json:"headers,omitempty"`
	AuthHeader     *bool                      `json:"authHeader,omitempty"`
	Compat         map[string]any             `json:"compat,omitempty"`
	Models         []PiModelEntry             `json:"models,omitempty"`
	ModelOverrides map[string]PiModelOverride `json:"modelOverrides,omitempty"`
}

type PiAuthEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type PiProxyState struct {
	Version                 int             `json:"version"`
	CreatedAt               string          `json:"createdAt"`
	ModelsPath              string          `json:"modelsPath"`
	AuthPath                string          `json:"authPath"`
	ModelsFileExisted       bool            `json:"modelsFileExisted"`
	AuthFileExisted         bool            `json:"authFileExisted"`
	ProvidersObjectExisted  bool            `json:"providersObjectExisted"`
	OriginalProviderExisted bool            `json:"originalProviderExisted"`
	OriginalProvider        json.RawMessage `json:"originalProvider,omitempty"`
	OriginalAuthExisted     bool            `json:"originalAuthExisted"`
	OriginalAuth            json.RawMessage `json:"originalAuth,omitempty"`
	InjectedProviderHash    string          `json:"injectedProviderHash"`
	InjectedAuthHash        string          `json:"injectedAuthHash"`
}

type PiSettingsService struct {
	modelsMu          sync.Mutex
	builtinCatalogMu  sync.Mutex
	pricingMu         sync.Mutex
	pricingCache      *piModelPricingCache
	builtinCatalog    *PiBuiltinCatalogSnapshot
	builtinCatalogErr error
	builtinRetryAt    time.Time
	builtinLoader     func() (PiBuiltinCatalogSnapshot, error)
	relayAddr         string
	configDir         string
	statePath         string
	platformStatePath string
	uiStatePath       string
	providerService   *ProviderService
	providerLoader    func() ([]Provider, error)
}

func NewPiSettingsService(relayAddr string, providerService *ProviderService) *PiSettingsService {
	home, _ := os.UserHomeDir()
	statePath, _ := GetProxyStatePath("pi")
	platformStatePath, _ := GetProxyStatePath("pi-platforms")
	uiStatePath := ""
	if appConfigDir, err := getAppConfigDir(); err == nil {
		uiStatePath = filepath.Join(appConfigDir, "pi-ui.json")
	}
	service := &PiSettingsService{
		relayAddr:         relayAddr,
		configDir:         filepath.Join(home, ".pi", "agent"),
		statePath:         statePath,
		platformStatePath: platformStatePath,
		uiStatePath:       uiStatePath,
		providerService:   providerService,
	}
	if providerService != nil {
		service.providerLoader = func() ([]Provider, error) { return providerService.LoadProviders("pi") }
		// 装配 pi 平台保存后的网关同步回调。
		// 不装配的话，任何绕过 PiSettingsService 直接调 ProviderService.SaveProviders("pi", ...)
		// 的入口（例如 DeepLinkService 导入）都不会同步 ~/.pi/agent/models.json，
		// 导致应用内配置与 Pi CLI 侧静默漂移。
		//
		// 回调只使用传入的 provider 快照，不会反向调用 ProviderService，
		// 因此在 saveProvidersLocked 持锁期间调用是安全的。
		providerService.setPiGatewaySync(service.syncGatewayIfEnabledWithProviders)
	}
	if state, err := service.loadUIState(); err == nil {
		setPiDebugLogging(state.DebugLogging)
	}
	return service
}

func (s *PiSettingsService) ProxyStatus() (ClaudeProxyStatus, error) {
	status := ClaudeProxyStatus{BaseURL: s.baseURL()}
	modelsRoot, _, err := readJSONObject(s.modelsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}
	providers, err := nestedJSONObject(modelsRoot, "providers")
	if err != nil {
		return status, nil
	}
	raw, exists := providers[piGatewayProviderKey]
	if !exists {
		return status, nil
	}
	var gateway PiGatewayProvider
	if json.Unmarshal(raw, &gateway) != nil {
		return status, nil
	}
	status.Enabled = sameURL(gateway.BaseURL, s.baseURL()) && gateway.APIKey == piGatewayToken
	return status, nil
}

func (s *PiSettingsService) EnableProxy() error {
	if s.providerLoader == nil {
		return fmt.Errorf("Pi Provider 服务未初始化")
	}
	if strings.TrimSpace(s.statePath) == "" {
		return fmt.Errorf("Pi 代理状态路径不可用")
	}
	if _, err := os.Stat(s.statePath); err == nil {
		return s.SyncGateway()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	modelsRoot, modelsExisted, err := readJSONObjectOrDefault(s.modelsPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi models.json 失败: %w", err)
	}
	authRoot, authExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi auth.json 失败: %w", err)
	}
	_, providersObjectExisted := modelsRoot["providers"]
	providers, err := ensureNestedJSONObject(modelsRoot, "providers")
	if err != nil {
		return err
	}
	gatewayRaw, err := s.buildGatewayRaw()
	if err != nil {
		return err
	}
	authRaw, _ := json.Marshal(PiAuthEntry{Type: "api_key", Key: piGatewayToken})
	state := PiProxyState{
		Version:                piProxyStateVersion,
		CreatedAt:              time.Now().UTC().Format(time.RFC3339Nano),
		ModelsPath:             s.modelsPath(),
		AuthPath:               s.authPath(),
		ModelsFileExisted:      modelsExisted,
		AuthFileExisted:        authExisted,
		ProvidersObjectExisted: providersObjectExisted,
		InjectedProviderHash:   canonicalJSONHash(gatewayRaw),
		InjectedAuthHash:       canonicalJSONHash(authRaw),
	}
	if original, exists := providers[piGatewayProviderKey]; exists {
		state.OriginalProviderExisted = true
		state.OriginalProvider = cloneRawMessage(original)
	}
	if original, exists := authRoot[piGatewayProviderKey]; exists {
		state.OriginalAuthExisted = true
		state.OriginalAuth = cloneRawMessage(original)
	}
	if err := AtomicWriteJSON(s.statePath, state); err != nil {
		return fmt.Errorf("保存 Pi 代理状态失败: %w", err)
	}

	providers[piGatewayProviderKey] = gatewayRaw
	modelsRoot["providers"], _ = json.Marshal(providers)
	authRoot[piGatewayProviderKey] = authRaw
	if err := writePiConfigPair(s.modelsPath(), modelsRoot, modelsExisted, s.authPath(), authRoot, authExisted, true); err != nil {
		_ = os.Remove(s.statePath)
		return err
	}
	return nil
}

func (s *PiSettingsService) SyncGateway() error {
	if s.providerLoader == nil {
		return fmt.Errorf("Pi Provider 服务未初始化")
	}
	providers, err := s.providerLoader()
	if err != nil {
		return fmt.Errorf("加载 Pi Provider 失败: %w", err)
	}
	return s.syncGatewayWithProviders(providers)
}

func (s *PiSettingsService) syncGatewayWithProviders(providerSnapshot []Provider) error {
	var state PiProxyState
	if err := ReadJSONFile(s.statePath, &state); err != nil {
		return fmt.Errorf("Pi 代理尚未启用或状态不可用: %w", err)
	}
	modelsRoot, _, err := readJSONObject(s.modelsPath())
	if err != nil {
		return fmt.Errorf("读取 Pi models.json 失败: %w", err)
	}
	providers, err := ensureNestedJSONObject(modelsRoot, "providers")
	if err != nil {
		return err
	}
	current, exists := providers[piGatewayProviderKey]
	if exists && state.InjectedProviderHash != "" && canonicalJSONHash(current) != state.InjectedProviderHash {
		return fmt.Errorf("Pi gateway 配置已被外部修改，请先处理冲突再同步")
	}
	gatewayRaw, err := s.buildGatewayRawForProviders(providerSnapshot)
	if err != nil {
		return err
	}
	originalModels, err := os.ReadFile(s.modelsPath())
	if err != nil {
		return fmt.Errorf("读取 Pi models.json 备份失败: %w", err)
	}
	providers[piGatewayProviderKey] = gatewayRaw
	modelsRoot["providers"], _ = json.Marshal(providers)
	if err := AtomicWriteJSON(s.modelsPath(), modelsRoot); err != nil {
		return err
	}
	state.InjectedProviderHash = canonicalJSONHash(gatewayRaw)
	if err := AtomicWriteJSON(s.statePath, state); err != nil {
		if rollbackErr := atomicWriteFile(s.modelsPath(), originalModels, 0o644); rollbackErr != nil {
			return fmt.Errorf("更新 Pi 代理状态失败: %w; models.json 回滚失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("更新 Pi 代理状态失败: %w", err)
	}
	return nil
}

func (s *PiSettingsService) SyncGatewayIfEnabled() error {
	if s.providerLoader == nil {
		return fmt.Errorf("Pi Provider 服务未初始化")
	}
	providers, err := s.providerLoader()
	if err != nil {
		return fmt.Errorf("加载 Pi Provider 失败: %w", err)
	}
	return s.syncGatewayIfEnabledWithProviders(providers)
}

func (s *PiSettingsService) syncGatewayIfEnabledWithProviders(providerSnapshot []Provider) error {
	if _, err := os.Stat(s.statePath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return s.syncGatewayWithProviders(providerSnapshot)
}

func (s *PiSettingsService) DisableProxy() error {
	var state PiProxyState
	if err := ReadJSONFile(s.statePath, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.fallbackDisable()
		}
		return fmt.Errorf("读取 Pi 代理状态失败: %w", err)
	}
	modelsRoot, _, err := readJSONObjectOrDefault(s.modelsPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi models.json 失败: %w", err)
	}
	authRoot, _, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi auth.json 失败: %w", err)
	}
	providers, err := ensureNestedJSONObject(modelsRoot, "providers")
	if err != nil {
		return err
	}
	if current, exists := providers[piGatewayProviderKey]; exists && canonicalJSONHash(current) != state.InjectedProviderHash {
		return fmt.Errorf("Pi gateway 配置已被外部修改，拒绝覆盖；请恢复 code-switch-r entry 后重试")
	}
	if current, exists := authRoot[piGatewayProviderKey]; exists && canonicalJSONHash(current) != state.InjectedAuthHash {
		return fmt.Errorf("Pi auth 配置已被外部修改，拒绝覆盖；请恢复 code-switch-r entry 后重试")
	}
	if state.OriginalProviderExisted {
		providers[piGatewayProviderKey] = cloneRawMessage(state.OriginalProvider)
	} else {
		delete(providers, piGatewayProviderKey)
	}
	if state.OriginalAuthExisted {
		authRoot[piGatewayProviderKey] = cloneRawMessage(state.OriginalAuth)
	} else {
		delete(authRoot, piGatewayProviderKey)
	}
	if len(providers) == 0 && !state.ProvidersObjectExisted {
		delete(modelsRoot, "providers")
	} else {
		modelsRoot["providers"], _ = json.Marshal(providers)
	}
	if err := writePiConfigPair(s.modelsPath(), modelsRoot, state.ModelsFileExisted, s.authPath(), authRoot, state.AuthFileExisted, false); err != nil {
		return err
	}
	return os.Remove(s.statePath)
}

func (s *PiSettingsService) fallbackDisable() error {
	modelsRoot, modelsExisted, err := readJSONObjectOrDefault(s.modelsPath(), map[string]json.RawMessage{})
	if err != nil {
		return err
	}
	authRoot, authExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return err
	}
	providers, err := ensureNestedJSONObject(modelsRoot, "providers")
	if err != nil {
		return err
	}
	if raw, exists := providers[piGatewayProviderKey]; exists {
		var gateway PiGatewayProvider
		if json.Unmarshal(raw, &gateway) == nil && sameURL(gateway.BaseURL, s.baseURL()) && gateway.APIKey == piGatewayToken {
			delete(providers, piGatewayProviderKey)
		}
	}
	if raw, exists := authRoot[piGatewayProviderKey]; exists {
		var auth PiAuthEntry
		if json.Unmarshal(raw, &auth) == nil && auth.Key == piGatewayToken {
			delete(authRoot, piGatewayProviderKey)
		}
	}
	modelsRoot["providers"], _ = json.Marshal(providers)
	return writePiConfigPair(s.modelsPath(), modelsRoot, modelsExisted, s.authPath(), authRoot, authExisted, false)
}

func (s *PiSettingsService) buildGatewayRaw() (json.RawMessage, error) {
	providers, err := s.providerLoader()
	if err != nil {
		return nil, fmt.Errorf("加载 Pi Provider 失败: %w", err)
	}
	return s.buildGatewayRawForProviders(providers)
}

func (s *PiSettingsService) buildGatewayRawForProviders(providers []Provider) (json.RawMessage, error) {
	if err := ValidatePiGatewayProviders(providers); err != nil {
		return nil, err
	}
	gateway, err := BuildPiGatewayProvider(providers, s.baseURL())
	if err != nil {
		return nil, err
	}
	return json.Marshal(gateway)
}

func BuildPiGatewayProvider(providers []Provider, baseURL string) (PiGatewayProvider, error) {
	models := make([]PiModelEntry, 0)
	seen := make(map[string]struct{})
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" || strings.Contains(name, "/") {
			return PiGatewayProvider{}, fmt.Errorf("Pi Provider 名称不能为空且不能包含 '/': %q", provider.Name)
		}
		modelNames := make(map[string]struct{})
		for model, enabled := range provider.SupportedModels {
			if enabled && strings.TrimSpace(model) != "" && !strings.Contains(model, "*") {
				modelNames[model] = struct{}{}
			}
		}
		for model := range provider.ModelMapping {
			if strings.TrimSpace(model) != "" && !strings.Contains(model, "*") {
				modelNames[model] = struct{}{}
			}
		}
		configuredModels := make(map[string]PiModelEntry, len(provider.PiModels))
		for _, configured := range provider.PiModels {
			modelID := strings.TrimSpace(configured.ID)
			if modelID == "" || strings.Contains(modelID, "*") {
				continue
			}
			configured.ID = modelID
			configuredModels[modelID] = configured
			modelNames[modelID] = struct{}{}
		}
		clientAPI := piAPIForProtocol(ResolveProviderUpstreamProtocol("pi", provider, provider.GetEffectiveEndpoint("/v1/chat/completions")))
		for model := range modelNames {
			id := name + "/" + model
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			entry := clonePiModelEntry(configuredModels[model])
			entry.ID = id
			if strings.TrimSpace(entry.Name) == "" {
				entry.Name = id
			}
			if strings.TrimSpace(entry.API) == "" {
				entry.API = clientAPI
			}
			if len(entry.Input) == 0 {
				entry.Input = []string{"text"}
			}
			if entry.ContextWindow == nil {
				entry.ContextWindow = intPointer(defaultPiContextWindow)
			}
			if entry.MaxTokens == nil {
				entry.MaxTokens = intPointer(defaultPiMaxTokens)
			}
			if entry.Reasoning == nil {
				entry.Reasoning = boolPointer(looksLikeReasoningModel(model))
			}
			if override, exists := provider.PiModelOverrides[model]; exists {
				entry = applyPiModelOverride(entry, override)
			}
			models = append(models, entry)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return PiGatewayProvider{
		BaseURL: baseURL, APIKey: piGatewayToken, API: "openai-completions",
		AuthHeader: boolPointer(false), Models: models,
	}, nil
}

func ValidatePiGatewayProviders(providers []Provider) error {
	errors := make([]string, 0)
	for _, provider := range providers {
		for _, message := range validatePiProviderConfiguration(provider) {
			errors = append(errors, fmt.Sprintf("[%s] %s", provider.Name, message))
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("Pi 网关配置无效：\n  - %s", strings.Join(errors, "\n  - "))
	}
	return nil
}

func clonePiModelEntry(source PiModelEntry) PiModelEntry {
	cloned := clonePiModelEntries([]PiModelEntry{source})
	if len(cloned) == 0 {
		return PiModelEntry{}
	}
	return cloned[0]
}

func applyPiModelOverride(model PiModelEntry, override PiModelOverride) PiModelEntry {
	if strings.TrimSpace(override.Name) != "" {
		model.Name = override.Name
	}
	if override.Reasoning != nil {
		model.Reasoning = boolPointer(*override.Reasoning)
	}
	if override.ThinkingLevelMap != nil {
		if model.ThinkingLevelMap == nil {
			model.ThinkingLevelMap = make(map[string]*string, len(override.ThinkingLevelMap))
		}
		for level, value := range override.ThinkingLevelMap {
			if value == nil {
				model.ThinkingLevelMap[level] = nil
				continue
			}
			mapped := *value
			model.ThinkingLevelMap[level] = &mapped
		}
	}
	if override.Input != nil {
		model.Input = append([]string(nil), override.Input...)
	}
	if override.ContextWindow != nil {
		model.ContextWindow = intPointer(*override.ContextWindow)
	}
	if override.MaxTokens != nil {
		model.MaxTokens = intPointer(*override.MaxTokens)
	}
	if override.Cost != nil {
		cost := PiModelCost{}
		if model.Cost != nil {
			cost = *model.Cost
			cost.Tiers = append([]PiModelCostTier(nil), model.Cost.Tiers...)
		}
		if override.Cost.Input != nil {
			cost.Input = *override.Cost.Input
		}
		if override.Cost.Output != nil {
			cost.Output = *override.Cost.Output
		}
		if override.Cost.CacheRead != nil {
			cost.CacheRead = *override.Cost.CacheRead
		}
		if override.Cost.CacheWrite != nil {
			cost.CacheWrite = *override.Cost.CacheWrite
		}
		if override.Cost.Tiers != nil {
			cost.Tiers = append([]PiModelCostTier(nil), override.Cost.Tiers...)
		}
		model.Cost = &cost
	}
	if override.Headers != nil {
		if model.Headers == nil {
			model.Headers = make(map[string]string, len(override.Headers))
		}
		for key, value := range override.Headers {
			model.Headers[key] = value
		}
	}
	if override.Compat != nil {
		if model.Compat == nil {
			model.Compat = make(map[string]any, len(override.Compat))
		}
		for key, value := range override.Compat {
			model.Compat[key] = value
		}
	}
	return model
}

func piQualifiedModelIDs(provider Provider) []string {
	name := strings.TrimSpace(provider.Name)
	ids := make(map[string]struct{})
	for model, enabled := range provider.SupportedModels {
		if enabled && strings.TrimSpace(model) != "" && !strings.Contains(model, "*") {
			ids[name+"/"+strings.TrimSpace(model)] = struct{}{}
		}
	}
	for model := range provider.ModelMapping {
		if strings.TrimSpace(model) != "" && !strings.Contains(model, "*") {
			ids[name+"/"+strings.TrimSpace(model)] = struct{}{}
		}
	}
	for _, model := range provider.PiModels {
		if strings.TrimSpace(model.ID) != "" && !strings.Contains(model.ID, "*") {
			ids[name+"/"+strings.TrimSpace(model.ID)] = struct{}{}
		}
	}
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func diagnosticPathFromMessage(message string) string {
	if index := strings.IndexByte(message, ' '); index > 0 {
		return message[:index]
	}
	return "config"
}

func diagnosticModelLocation(provider Provider, path string) (string, string) {
	if strings.HasPrefix(path, "piModels[") {
		var index int
		if _, err := fmt.Sscanf(path, "piModels[%d]", &index); err == nil && index >= 0 && index < len(provider.PiModels) {
			field := "id"
			if closing := strings.Index(path, "]."); closing >= 0 {
				field = strings.Split(path[closing+2:], ".")[0]
			}
			return strings.TrimSpace(provider.Name) + "/" + strings.TrimSpace(provider.PiModels[index].ID), field
		}
	}
	if strings.HasPrefix(path, "piModelOverrides.") {
		remainder := strings.TrimPrefix(path, "piModelOverrides.")
		modelIDs := make([]string, 0, len(provider.PiModelOverrides))
		for modelID := range provider.PiModelOverrides {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Slice(modelIDs, func(i, j int) bool { return len(modelIDs[i]) > len(modelIDs[j]) })
		for _, modelID := range modelIDs {
			if remainder != modelID && !strings.HasPrefix(remainder, modelID+".") {
				continue
			}
			field := "modelOverrides"
			if suffix := strings.TrimPrefix(remainder, modelID); strings.HasPrefix(suffix, ".") {
				field = strings.Split(strings.TrimPrefix(suffix, "."), ".")[0]
			}
			return strings.TrimSpace(provider.Name) + "/" + modelID, field
		}
	}
	return "", strings.Split(path, ".")[0]
}

func piAPIForProtocol(protocol UpstreamProtocolType) string {
	switch protocol {
	case UpstreamProtocolAnthropic:
		return "anthropic-messages"
	case UpstreamProtocolOpenAIResponses:
		return "openai-responses"
	case UpstreamProtocolGoogle:
		return "google-generative-ai"
	default:
		return "openai-completions"
	}
}

func looksLikeReasoningModel(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "reason") || strings.Contains(lower, "thinking") ||
		strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") || strings.Contains(lower, "gpt-5")
}

func (s *PiSettingsService) modelsPath() string { return filepath.Join(s.configDir, "models.json") }
func (s *PiSettingsService) authPath() string   { return filepath.Join(s.configDir, "auth.json") }

func (s *PiSettingsService) baseURL() string {
	addr := strings.TrimSpace(s.relayAddr)
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	if addr == "" {
		addr = "127.0.0.1:18100"
	}
	return "http://" + strings.TrimSuffix(addr, "/") + "/pi/v1"
}

func readJSONObject(path string) (map[string]json.RawMessage, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(stripJSONComments(data), &root); err != nil {
		return nil, true, err
	}
	if root == nil {
		return nil, true, fmt.Errorf("%s 必须是 JSON 对象", path)
	}
	return root, true, nil
}

func stripJSONComments(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		current := data[i]
		if inString {
			result = append(result, current)
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			result = append(result, current)
			continue
		}
		if current == '/' && i+1 < len(data) && data[i+1] == '/' {
			i += 2
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
			if i < len(data) {
				result = append(result, data[i])
			}
			continue
		}
		if current == '/' && i+1 < len(data) && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				if data[i] == '\n' || data[i] == '\r' {
					result = append(result, data[i])
				}
				i++
			}
			i++
			continue
		}
		result = append(result, current)
	}
	return result
}

func readJSONObjectOrDefault(path string, fallback map[string]json.RawMessage) (map[string]json.RawMessage, bool, error) {
	root, existed, err := readJSONObject(path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, false, nil
	}
	return root, existed, err
}

func nestedJSONObject(root map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	raw, exists := root[key]
	if !exists {
		return nil, os.ErrNotExist
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("%s 必须是 JSON 对象", key)
	}
	return value, nil
}

func ensureNestedJSONObject(root map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	value, err := nestedJSONObject(root, key)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	return value, err
}

func canonicalJSONHash(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	canonical, _ := json.Marshal(value)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func sameURL(left, right string) bool {
	return strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(left), "/"), strings.TrimSuffix(strings.TrimSpace(right), "/"))
}

func writePiConfigPair(modelsPath string, models map[string]json.RawMessage, modelsExisted bool, authPath string, auth map[string]json.RawMessage, authExisted bool, enabling bool) error {
	firstPath, firstValue, firstExisted := modelsPath, models, modelsExisted
	secondPath, secondValue, secondExisted := authPath, auth, authExisted
	if enabling {
		firstPath, firstValue, firstExisted = authPath, auth, authExisted
		secondPath, secondValue, secondExisted = modelsPath, models, modelsExisted
	}
	originalFirst, readErr := os.ReadFile(firstPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := AtomicWriteJSON(firstPath, firstValue); err != nil {
		return err
	}
	if err := AtomicWriteJSON(secondPath, secondValue); err != nil {
		if firstExisted {
			_ = AtomicWriteBytes(firstPath, originalFirst)
		} else {
			_ = os.Remove(firstPath)
		}
		return fmt.Errorf("更新 %s 失败并已回滚 %s: %w", secondPath, firstPath, err)
	}
	if !firstExisted && len(firstValue) == 0 {
		if err := os.Remove(firstPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("恢复文件不存在状态失败 %s: %w", firstPath, err)
		}
	}
	if !secondExisted && len(secondValue) == 0 {
		if err := os.Remove(secondPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("恢复文件不存在状态失败 %s: %w", secondPath, err)
		}
	}
	return nil
}
