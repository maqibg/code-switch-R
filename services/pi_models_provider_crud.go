package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

var piModelsProviderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// PiModelsProviderTemplate is returned only for an explicit edit action. Unlike
// ModelsCatalog, it includes credentials and headers required by the editor.
type PiModelsProviderTemplate struct {
	Fingerprint    string                     `json:"fingerprint"`
	ID             string                     `json:"id"`
	Name           string                     `json:"name,omitempty"`
	BaseURL        string                     `json:"baseUrl,omitempty"`
	APIKey         string                     `json:"apiKey,omitempty"`
	API            string                     `json:"api,omitempty"`
	Headers        map[string]string          `json:"headers,omitempty"`
	AuthHeader     *bool                      `json:"authHeader,omitempty"`
	Compat         map[string]any             `json:"compat,omitempty"`
	Models         []PiModelEntry             `json:"models"`
	ModelOverrides map[string]PiModelOverride `json:"modelOverrides"`
}

// piModelsProviderFile field order is the canonical models.json Provider order.
type piModelsProviderFile struct {
	BaseURL        string                     `json:"baseUrl,omitempty"`
	APIKey         string                     `json:"apiKey,omitempty"`
	API            string                     `json:"api,omitempty"`
	Headers        map[string]string          `json:"headers,omitempty"`
	AuthHeader     *bool                      `json:"authHeader,omitempty"`
	Compat         map[string]any             `json:"compat,omitempty"`
	Models         []PiModelEntry             `json:"models,omitempty"`
	ModelOverrides map[string]PiModelOverride `json:"modelOverrides,omitempty"`
	Name           string                     `json:"name,omitempty"`
}

func (s *PiSettingsService) GetModelsProvider(id string) (PiModelsProviderTemplate, error) {
	_, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return PiModelsProviderTemplate{}, err
	}
	id = strings.TrimSpace(id)
	raw, exists := providers[id]
	if !exists {
		return PiModelsProviderTemplate{}, fmt.Errorf("Pi models.json Provider 不存在: %s", id)
	}
	state, stateErr := s.loadPlatformState()
	if stateErr != nil {
		return PiModelsProviderTemplate{}, stateErr
	}
	if managed, ok := state.Platforms[id]; ok {
		if canonicalJSONHash(raw) != managed.InjectedProviderHash {
			return PiModelsProviderTemplate{}, fmt.Errorf("Pi 平台 %q 托管配置已被外部修改，请先处理冲突", id)
		}
		raw = managed.OriginalProvider
	}
	var source piModelsProviderFile
	if err := json.Unmarshal(raw, &source); err != nil {
		return PiModelsProviderTemplate{}, fmt.Errorf("解析 Pi models.json Provider %q 失败: %w", id, err)
	}
	return providerTemplateFromFile(id, fingerprint, source), nil
}

func (s *PiSettingsService) CreateModelsProvider(input PiModelsProviderTemplate) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	detected, _, err := s.ensurePiModelsInitialized()
	if err != nil {
		return err
	}
	if !detected {
		return fmt.Errorf("未检测到 Pi 配置目录: %s", s.configDir)
	}
	input, err = normalizePiModelsProviderTemplate(input)
	if err != nil {
		return err
	}
	root, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	if err := requirePiModelsFingerprint(input.Fingerprint, fingerprint); err != nil {
		return err
	}
	if _, exists := providers[input.ID]; exists {
		return fmt.Errorf("Pi models.json Provider 已存在: %s", input.ID)
	}
	return writePiModelsProviderDocument(s.modelsPath(), root, providers, input)
}

func (s *PiSettingsService) UpdateModelsProvider(input PiModelsProviderTemplate) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	input, err := normalizePiModelsProviderTemplate(input)
	if err != nil {
		return err
	}
	state, stateErr := s.loadPlatformState()
	if stateErr != nil {
		return stateErr
	}
	if _, managed := state.Platforms[input.ID]; managed {
		return fmt.Errorf("Pi 平台 %q 正在托管，请先关闭该平台托管再编辑", input.ID)
	}
	root, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	if err := requirePiModelsFingerprint(input.Fingerprint, fingerprint); err != nil {
		return err
	}
	if _, exists := providers[input.ID]; !exists {
		return fmt.Errorf("Pi models.json Provider 不存在: %s", input.ID)
	}
	return writePiModelsProviderDocument(s.modelsPath(), root, providers, input)
}

func (s *PiSettingsService) DeleteModelsProvider(id, expectedFingerprint string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("Pi models.json Provider ID 不能为空")
	}
	state, stateErr := s.loadPlatformState()
	if stateErr != nil {
		return stateErr
	}
	if _, managed := state.Platforms[id]; managed {
		return fmt.Errorf("Pi 平台 %q 正在托管，请先关闭该平台托管再删除", id)
	}
	var configuredProviders, remainingProviders []Provider
	if s.providerLoader != nil {
		configuredProviders, stateErr = s.providerLoader()
		if stateErr != nil {
			return fmt.Errorf("检查 Pi 平台供应商失败: %w", stateErr)
		}
		remainingProviders = make([]Provider, 0, len(configuredProviders))
		for _, provider := range configuredProviders {
			if provider.piPlatformKey() != id {
				remainingProviders = append(remainingProviders, provider)
			}
		}
	}
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	root, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	if err := requirePiModelsFingerprint(expectedFingerprint, fingerprint); err != nil {
		return err
	}
	if _, exists := providers[id]; !exists {
		return fmt.Errorf("Pi models.json Provider 不存在: %s", id)
	}
	modelsBackup, err := os.ReadFile(s.modelsPath())
	if err != nil {
		return fmt.Errorf("备份 Pi models.json 失败: %w", err)
	}
	delete(providers, id)
	providersRaw, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	root["providers"] = providersRaw
	if err := AtomicWriteJSON(s.modelsPath(), root); err != nil {
		return fmt.Errorf("删除 Pi models.json Provider %q 失败: %w", id, err)
	}
	if s.providerService != nil && len(remainingProviders) != len(configuredProviders) {
		if err := s.providerService.SaveProviders("pi", remainingProviders); err != nil {
			if rollbackErr := atomicWriteFile(s.modelsPath(), modelsBackup, 0o644); rollbackErr != nil {
				return fmt.Errorf("删除 Pi 平台供应商失败: %w; models.json 回滚失败: %v", err, rollbackErr)
			}
			return fmt.Errorf("删除 Pi 平台供应商失败: %w", err)
		}
	}
	return nil
}

func readPiModelsProviderDocument(path string) (map[string]json.RawMessage, map[string]json.RawMessage, string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, map[string]json.RawMessage{}, "", nil
	}
	if err != nil {
		return nil, nil, "", fmt.Errorf("读取 Pi models.json 失败: %w", err)
	}
	cleaned := stripJSONComments(data)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &root); err != nil {
		return nil, nil, "", fmt.Errorf("解析 Pi models.json 失败: %w", err)
	}
	if root == nil {
		return nil, nil, "", fmt.Errorf("Pi models.json 必须是 JSON 对象")
	}
	providers, err := ensureNestedJSONObject(root, "providers")
	if err != nil {
		return nil, nil, "", err
	}
	return root, providers, fingerprintPiModelsJSON(cleaned), nil
}

func writePiModelsProviderDocument(path string, root, providers map[string]json.RawMessage, input PiModelsProviderTemplate) error {
	knownRaw, err := json.Marshal(piModelsProviderFile{
		BaseURL: input.BaseURL, APIKey: input.APIKey, API: input.API,
		Headers: input.Headers, AuthHeader: input.AuthHeader, Compat: input.Compat,
		Models: input.Models, ModelOverrides: input.ModelOverrides, Name: input.Name,
	})
	if err != nil {
		return fmt.Errorf("序列化 Pi models.json Provider %q 失败: %w", input.ID, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(knownRaw, &fields); err != nil {
		return err
	}
	if current, exists := providers[input.ID]; exists {
		var existing map[string]json.RawMessage
		if err := json.Unmarshal(current, &existing); err != nil {
			return fmt.Errorf("解析 Pi models.json Provider %q 失败: %w", input.ID, err)
		}
		for key, value := range existing {
			if _, known := fields[key]; known {
				continue
			}
			switch key {
			case "baseUrl", "apiKey", "api", "headers", "authHeader", "compat", "models", "modelOverrides", "name":
				continue
			default:
				fields[key] = value
			}
		}
	}
	providerRaw, err := marshalOrderedPiProvider(fields)
	if err != nil {
		return fmt.Errorf("序列化 Pi models.json Provider %q 失败: %w", input.ID, err)
	}
	providers[input.ID] = providerRaw
	providersRaw, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	root["providers"] = providersRaw
	if err := AtomicWriteJSON(path, root); err != nil {
		return fmt.Errorf("保存 Pi models.json Provider %q 失败: %w", input.ID, err)
	}
	return nil
}

func normalizePiModelsProviderTemplate(input PiModelsProviderTemplate) (PiModelsProviderTemplate, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.API = strings.TrimSpace(input.API)
	if input.ID == "" || len(input.ID) > 64 || !piModelsProviderIDPattern.MatchString(input.ID) {
		return PiModelsProviderTemplate{}, fmt.Errorf("Provider ID 只能包含字母、数字、点、下划线和连字符，且不能超过 64 个字符")
	}
	if input.API != "" {
		if _, supported := piSupportedAPIs[input.API]; !supported {
			return PiModelsProviderTemplate{}, fmt.Errorf("Provider API %q 不是当前 Pi 版本支持的 API", input.API)
		}
	}
	if input.BaseURL != "" {
		parsed, err := url.Parse(input.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsAny(input.BaseURL, "\r\n") {
			return PiModelsProviderTemplate{}, fmt.Errorf("Provider baseUrl 必须是有效的 HTTP(S) URL")
		}
	}
	if _, err := canonicalizeHeaderMap(input.Headers); err != nil {
		return PiModelsProviderTemplate{}, fmt.Errorf("Provider headers 无效: %w", err)
	}
	seenModels := make(map[string]struct{}, len(input.Models))
	for index := range input.Models {
		model := &input.Models[index]
		model.ID = strings.TrimSpace(model.ID)
		model.Name = strings.TrimSpace(model.Name)
		model.API = strings.TrimSpace(model.API)
		model.BaseURL = strings.TrimSpace(model.BaseURL)
		if model.ID == "" {
			return PiModelsProviderTemplate{}, fmt.Errorf("models[%d].id 不能为空", index)
		}
		if _, duplicate := seenModels[model.ID]; duplicate {
			return PiModelsProviderTemplate{}, fmt.Errorf("models[%d].id=%q 重复", index, model.ID)
		}
		seenModels[model.ID] = struct{}{}
		if messages := validateNativePiModelEntry(fmt.Sprintf("models[%d]", index), *model, input.API); len(messages) > 0 {
			return PiModelsProviderTemplate{}, fmt.Errorf("模型 %q 无效: %s", model.ID, strings.Join(messages, "; "))
		}
	}
	if input.ModelOverrides == nil {
		input.ModelOverrides = map[string]PiModelOverride{}
	}
	normalizedOverrides := make(map[string]PiModelOverride, len(input.ModelOverrides))
	for modelID, override := range input.ModelOverrides {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			return PiModelsProviderTemplate{}, fmt.Errorf("modelOverrides 的模型 ID 不能为空")
		}
		if _, replaced := seenModels[modelID]; replaced {
			return PiModelsProviderTemplate{}, fmt.Errorf("模型 %q 同时存在于 models 和 modelOverrides；完整模型会使覆盖失效", modelID)
		}
		if messages := validateNativePiModelOverride("modelOverrides."+modelID, override); len(messages) > 0 {
			return PiModelsProviderTemplate{}, fmt.Errorf("模型覆盖 %q 无效: %s", modelID, strings.Join(messages, "; "))
		}
		normalizedOverrides[modelID] = override
	}
	input.ModelOverrides = normalizedOverrides
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	if input.Compat == nil {
		input.Compat = map[string]any{}
	}
	if input.Models == nil {
		input.Models = []PiModelEntry{}
	}
	sort.SliceStable(input.Models, func(i, j int) bool { return input.Models[i].ID < input.Models[j].ID })
	return input, nil
}

func validateNativePiModelEntry(path string, model PiModelEntry, fallbackAPI string) []string {
	api := strings.TrimSpace(model.API)
	if api == "" {
		api = fallbackAPI
	}
	errors := make([]string, 0)
	if _, supported := piSupportedAPIs[api]; !supported {
		errors = append(errors, fmt.Sprintf("%s.api=%q 不是当前 Pi 版本支持的 API", path, api))
	}
	errors = append(errors, validatePiModelBasics(path, model.Input, model.ContextWindow, model.MaxTokens, model.ThinkingLevelMap, model.Headers)...)
	if model.Cost != nil {
		errors = append(errors, validatePiModelCost(path+".cost", *model.Cost)...)
	}
	return errors
}

func validateNativePiModelOverride(path string, override PiModelOverride) []string {
	errors := validatePiModelBasics(path, override.Input, override.ContextWindow, override.MaxTokens, override.ThinkingLevelMap, override.Headers)
	if override.Cost == nil {
		return errors
	}
	for name, value := range map[string]*float64{
		"input": override.Cost.Input, "output": override.Cost.Output,
		"cacheRead": override.Cost.CacheRead, "cacheWrite": override.Cost.CacheWrite,
	} {
		if value != nil && !isFiniteNonNegative(*value) {
			errors = append(errors, fmt.Sprintf("%s.cost.%s 必须是非负有限数值", path, name))
		}
	}
	for index, tier := range override.Cost.Tiers {
		errors = append(errors, validatePiCostTier(fmt.Sprintf("%s.cost.tiers[%d]", path, index), tier)...)
	}
	return errors
}

func providerTemplateFromFile(id, fingerprint string, source piModelsProviderFile) PiModelsProviderTemplate {
	result := PiModelsProviderTemplate{
		Fingerprint: fingerprint, ID: id, Name: source.Name,
		BaseURL: source.BaseURL, APIKey: source.APIKey, API: source.API,
		Headers: source.Headers, AuthHeader: source.AuthHeader, Compat: source.Compat,
		Models: source.Models, ModelOverrides: source.ModelOverrides,
	}
	if result.Headers == nil {
		result.Headers = map[string]string{}
	}
	if result.Compat == nil {
		result.Compat = map[string]any{}
	}
	if result.Models == nil {
		result.Models = []PiModelEntry{}
	}
	if result.ModelOverrides == nil {
		result.ModelOverrides = map[string]PiModelOverride{}
	}
	return result
}

func requirePiModelsFingerprint(expected, actual string) error {
	if strings.TrimSpace(expected) != actual {
		return fmt.Errorf("Pi models.json 已被外部修改，请刷新后重新编辑")
	}
	return nil
}

func fingerprintPiModelsJSON(cleaned []byte) string {
	sum := sha256.Sum256(cleaned)
	return hex.EncodeToString(sum[:])
}
