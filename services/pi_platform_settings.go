package services

import (
	_ "embed"
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

const piPlatformStateVersion = 1

//go:embed pi_default_models.json
var defaultPiModelsJSON []byte

type PiPlatformProxyState struct {
	CreatedAt            string          `json:"createdAt"`
	OriginalProvider     json.RawMessage `json:"originalProvider"`
	InjectedProviderHash string          `json:"injectedProviderHash"`
	OriginalAuthExisted  bool            `json:"originalAuthExisted"`
	OriginalAuth         json.RawMessage `json:"originalAuth,omitempty"`
	InjectedAuthHash     string          `json:"injectedAuthHash,omitempty"`
}

type PiPlatformStateDocument struct {
	Version          int                             `json:"version"`
	ModelsPath       string                          `json:"modelsPath"`
	AuthFileExisted  bool                            `json:"authFileExisted"`
	AuthExistenceSet bool                            `json:"authExistenceSet"`
	Platforms        map[string]PiPlatformProxyState `json:"platforms"`
}

type PiPlatformProxyStatus struct {
	ProviderID string `json:"providerId"`
	Enabled    bool   `json:"enabled"`
	Conflict   bool   `json:"conflict"`
	BaseURL    string `json:"baseUrl"`
}

func (s *PiSettingsService) ensurePiModelsInitialized() (detected bool, initialized bool, err error) {
	info, statErr := os.Stat(s.configDir)
	if errors.Is(statErr, os.ErrNotExist) {
		return false, false, nil
	}
	if statErr != nil {
		return false, false, fmt.Errorf("检查 Pi 配置目录失败: %w", statErr)
	}
	if !info.IsDir() {
		return false, false, fmt.Errorf("Pi 配置路径不是目录: %s", s.configDir)
	}

	data, readErr := os.ReadFile(s.modelsPath())
	if errors.Is(readErr, os.ErrNotExist) || (readErr == nil && len(strings.TrimSpace(string(data))) == 0) {
		if err := writeDefaultPiModelsJSON(s.modelsPath()); err != nil {
			return true, false, err
		}
		return true, true, nil
	}
	if readErr != nil {
		return true, false, fmt.Errorf("读取 Pi models.json 失败: %w", readErr)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(stripJSONComments(data), &root); err != nil {
		return true, false, fmt.Errorf("解析 Pi models.json 失败: %w", err)
	}
	providers, providerErr := nestedJSONObject(root, "providers")
	if errors.Is(providerErr, os.ErrNotExist) || (providerErr == nil && len(providers) == 0) {
		if err := writeDefaultPiModelsJSON(s.modelsPath()); err != nil {
			return true, false, err
		}
		return true, true, nil
	}
	if providerErr != nil {
		return true, false, providerErr
	}
	return true, false, nil
}

func writeDefaultPiModelsJSON(path string) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(defaultPiModelsJSON, &root); err != nil {
		return fmt.Errorf("内置 Pi models.json 模板无效: %w", err)
	}
	if err := atomicWriteFile(path, defaultPiModelsJSON, 0o644); err != nil {
		return fmt.Errorf("初始化 Pi models.json 失败: %w", err)
	}
	return nil
}

func (s *PiSettingsService) PlatformProxyStatus(providerID string) (PiPlatformProxyStatus, error) {
	providerID = strings.TrimSpace(providerID)
	status := PiPlatformProxyStatus{ProviderID: providerID, BaseURL: s.platformBaseURL(providerID)}
	if providerID == "" {
		return status, fmt.Errorf("Pi 平台 ID 不能为空")
	}
	state, err := s.loadPlatformState()
	if err != nil {
		return status, err
	}
	entry, exists := state.Platforms[providerID]
	if !exists {
		return status, nil
	}
	_, providers, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return status, err
	}
	current, exists := providers[providerID]
	if !exists || !piManagedProviderConnectionMatches(current, s.platformBaseURL(providerID)) {
		status.Conflict = true
		return status, nil
	}
	if entry.InjectedAuthHash != "" {
		authRoot, _, authErr := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
		if authErr != nil {
			return status, fmt.Errorf("读取 Pi auth.json 失败: %w", authErr)
		}
		if !piManagedAuthMatches(authRoot, providerID, entry) {
			status.Conflict = true
			return status, nil
		}
	}
	status.Enabled = true
	return status, nil
}

func (s *PiSettingsService) EnablePlatformProxy(providerID string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()

	providerID = strings.TrimSpace(providerID)
	if !PiModelsProviderIDPattern.MatchString(providerID) {
		return fmt.Errorf("Pi 平台 ID 格式无效: %s", providerID)
	}
	detected, _, err := s.ensurePiModelsInitialized()
	if err != nil {
		return err
	}
	if !detected {
		return fmt.Errorf("未检测到 Pi 配置目录: %s", s.configDir)
	}
	root, platformObjects, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	original, exists := platformObjects[providerID]
	if !exists {
		return fmt.Errorf("Pi 平台不存在: %s", providerID)
	}
	var source piModelsProviderFile
	if err := json.Unmarshal(original, &source); err != nil {
		return fmt.Errorf("解析 Pi 平台 %q 失败: %w", providerID, err)
	}
	if _, ok := piManagedAPIs[strings.TrimSpace(source.API)]; !ok {
		return fmt.Errorf("Pi 平台 %q 的 api=%q 暂不支持托管", providerID, source.API)
	}
	for _, model := range source.Models {
		if strings.TrimSpace(model.BaseURL) != "" {
			return fmt.Errorf("Pi 平台 %q 的模型 %q 配置了独立 baseUrl，会绕过平台网关；请移除后再开启托管", providerID, model.ID)
		}
	}
	authRoot, authFileExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi auth.json 失败: %w", err)
	}
	originalAuth, originalAuthExisted := authRoot[providerID]
	authCredential, authCredentialExists, err := piAuthAPIKey(originalAuth, originalAuthExisted)
	if err != nil {
		return fmt.Errorf("读取 Pi 平台 %q 认证失败: %w", providerID, err)
	}

	state, err := s.loadPlatformState()
	if err != nil {
		return err
	}
	if existing, managed := state.Platforms[providerID]; managed {
		if !piManagedProviderConnectionMatches(original, s.platformBaseURL(providerID)) {
			return fmt.Errorf("Pi 平台 %q 已被外部修改，拒绝覆盖", providerID)
		}
		if !piManagedAuthMatches(authRoot, providerID, existing) {
			return fmt.Errorf("Pi 平台 %q 的 auth.json 认证已被外部修改，拒绝覆盖", providerID)
		}
		return nil
	}

	managedRaw, err := buildManagedPiPlatformRaw(original, s.platformBaseURL(providerID))
	if err != nil {
		return err
	}
	managedAuthRaw, err := json.Marshal(PiAuthEntry{Type: "api_key", Key: piGatewayToken})
	if err != nil {
		return fmt.Errorf("构建 Pi 平台认证失败: %w", err)
	}
	statePath, err := s.platformStateFile()
	if err != nil {
		return err
	}
	stateBackup, stateExisted, err := readOptionalFile(statePath)
	if err != nil {
		return err
	}
	providersBefore, imported, err := s.ensureFirstPlatformSupplier(providerID, source, authCredential, authCredentialExists)
	if err != nil {
		return err
	}
	if !state.AuthExistenceSet {
		state.AuthFileExisted = authFileExisted
		state.AuthExistenceSet = true
	}
	connectionSnapshot, snapshotErr := piProviderConnectionSnapshot(original)
	if snapshotErr != nil {
		return fmt.Errorf("保存 Pi 平台 %q 连接快照失败: %w", providerID, snapshotErr)
	}
	state.Platforms[providerID] = PiPlatformProxyState{
		CreatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		OriginalProvider:     connectionSnapshot,
		InjectedProviderHash: canonicalJSONHash(managedRaw),
		OriginalAuthExisted:  originalAuthExisted,
		OriginalAuth:         cloneRawMessage(originalAuth),
		InjectedAuthHash:     canonicalJSONHash(managedAuthRaw),
	}
	state.ModelsPath = s.modelsPath()
	platformObjects[providerID] = managedRaw
	providersRaw, err := json.Marshal(platformObjects)
	if err != nil {
		if imported {
			if rollbackErr := s.providerService.SaveProviders("pi", providersBefore); rollbackErr != nil {
				return fmt.Errorf("序列化 Pi 平台失败: %w; 供应商回滚失败: %v", err, rollbackErr)
			}
		}
		return fmt.Errorf("序列化 Pi 平台失败: %w", err)
	}
	root["providers"] = providersRaw
	authRoot[providerID] = managedAuthRaw
	if err := s.savePlatformState(state); err != nil {
		if imported {
			if rollbackErr := s.providerService.SaveProviders("pi", providersBefore); rollbackErr != nil {
				return fmt.Errorf("%w; 供应商回滚失败: %v", err, rollbackErr)
			}
		}
		return err
	}
	if err := writePiConfigPair(s.modelsPath(), root, true, s.authPath(), authRoot, authFileExisted, true); err != nil {
		stateRollbackErr := restoreOptionalFile(statePath, stateBackup, stateExisted)
		var supplierRollbackErr error
		if imported {
			supplierRollbackErr = s.providerService.SaveProviders("pi", providersBefore)
		}
		if stateRollbackErr != nil || supplierRollbackErr != nil {
			return fmt.Errorf("%w; 托管状态回滚: %v; 供应商回滚: %v", err, stateRollbackErr, supplierRollbackErr)
		}
		return err
	}
	return nil
}

func (s *PiSettingsService) DisablePlatformProxy(providerID string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()

	providerID = strings.TrimSpace(providerID)
	state, err := s.loadPlatformState()
	if err != nil {
		return err
	}
	entry, exists := state.Platforms[providerID]
	if !exists {
		return nil
	}
	root, platforms, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	current, exists := platforms[providerID]
	if !exists || !piManagedProviderConnectionMatches(current, s.platformBaseURL(providerID)) {
		return fmt.Errorf("Pi 平台 %q 已被外部修改，拒绝覆盖；请先处理冲突", providerID)
	}
	authRoot, authFileExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi auth.json 失败: %w", err)
	}
	if entry.InjectedAuthHash != "" {
		if !piManagedAuthMatches(authRoot, providerID, entry) {
			return fmt.Errorf("Pi 平台 %q 的 auth.json 认证已被外部修改，拒绝覆盖；请先处理冲突", providerID)
		}
	}
	modelsBackup, err := os.ReadFile(s.modelsPath())
	if err != nil {
		return fmt.Errorf("备份 Pi models.json 失败: %w", err)
	}
	authBackup, _, err := readOptionalFile(s.authPath())
	if err != nil {
		return fmt.Errorf("备份 Pi auth.json 失败: %w", err)
	}
	platforms[providerID], err = restorePiProviderConnection(current, entry.OriginalProvider)
	if err != nil {
		return fmt.Errorf("恢复 Pi 平台 %q 连接配置失败: %w", providerID, err)
	}
	providersRaw, err := json.Marshal(platforms)
	if err != nil {
		return fmt.Errorf("序列化 Pi 平台失败: %w", err)
	}
	root["providers"] = providersRaw
	if entry.InjectedAuthHash != "" {
		if entry.OriginalAuthExisted {
			authRoot[providerID] = cloneRawMessage(entry.OriginalAuth)
		} else {
			delete(authRoot, providerID)
		}
	}
	delete(state.Platforms, providerID)
	desiredAuthFileExisted := state.AuthFileExisted || len(authRoot) > 0
	if err := writePiConfigPair(s.modelsPath(), root, true, s.authPath(), authRoot, desiredAuthFileExisted, false); err != nil {
		return err
	}
	if err := s.savePlatformState(state); err != nil {
		modelsRollbackErr := atomicWriteFile(s.modelsPath(), modelsBackup, 0o644)
		authRollbackErr := restoreOptionalFile(s.authPath(), authBackup, authFileExisted)
		if modelsRollbackErr != nil || authRollbackErr != nil {
			return fmt.Errorf("保存 Pi 托管状态失败: %w; models.json 回滚: %v; auth.json 回滚: %v", err, modelsRollbackErr, authRollbackErr)
		}
		return err
	}
	return nil
}

func (s *PiSettingsService) ensureFirstPlatformSupplier(providerID string, source piModelsProviderFile, authCredential string, authCredentialExists bool) ([]Provider, bool, error) {
	if s.providerService == nil {
		return nil, false, fmt.Errorf("Pi Provider 服务未初始化")
	}
	providers, err := s.providerService.LoadProviders("pi")
	if err != nil {
		return nil, false, fmt.Errorf("加载 Pi 供应商失败: %w", err)
	}
	for _, provider := range providers {
		if provider.PiPlatformKey() == providerID {
			return providers, false, nil
		}
	}
	if strings.TrimSpace(source.BaseURL) == "" {
		return nil, false, fmt.Errorf("Pi 平台 %q 没有 baseUrl，无法导入第一个供应商", providerID)
	}
	newProvider, err := providerFromPiPlatform(providerID, source, nextProviderID(providers), authCredential, authCredentialExists)
	if err != nil {
		return nil, false, err
	}
	updated := append(append([]Provider(nil), providers...), newProvider)
	if err := s.providerService.SaveProviders("pi", updated); err != nil {
		return nil, false, fmt.Errorf("导入 Pi 平台首个供应商失败: %w", err)
	}
	return providers, true, nil
}

func providerFromPiPlatform(providerID string, source piModelsProviderFile, id int64, authCredential string, authCredentialExists bool) (Provider, error) {
	models := make(map[string]bool, len(source.Models)+len(source.ModelOverrides))
	for _, model := range source.Models {
		if modelID := strings.TrimSpace(model.ID); modelID != "" {
			models[modelID] = true
		}
	}
	for modelID := range source.ModelOverrides {
		if modelID = strings.TrimSpace(modelID); modelID != "" {
			models[modelID] = true
		}
	}
	headers := make(map[string]string)
	authHeaders := make(map[string]string, 2)
	for key, value := range source.Headers {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "authorization":
			authHeaders["Authorization"] = value
		case "x-api-key":
			authHeaders["x-api-key"] = value
		default:
			if _, blocked := blockedUpstreamHeaders[strings.ToLower(strings.TrimSpace(key))]; blocked {
				continue
			}
			headers[key] = value
		}
	}
	if len(headers) == 0 {
		headers = nil
	}
	protocol, authScheme, authHeader := piSupplierDefaults(source.API)
	credential := source.APIKey
	if authCredentialExists {
		credential = escapePiConfigLiteral(authCredential)
	}
	if source.AuthHeader != nil && *source.AuthHeader {
		if _, exists := authHeaders["x-api-key"]; exists {
			return Provider{}, fmt.Errorf("Pi 平台 %q 同时启用了 authHeader 和 x-api-key Header，当前网关无法无损导入两套认证", providerID)
		}
		authScheme, authHeader = "bearer", ""
	} else if len(authHeaders) > 1 {
		return Provider{}, fmt.Errorf("Pi 平台 %q 同时配置了 Authorization 和 x-api-key Header，当前网关无法无损导入两套认证", providerID)
	} else if value, exists := authHeaders["Authorization"]; exists {
		credential, authScheme, authHeader = value, "custom", "Authorization"
	} else if value, exists := authHeaders["x-api-key"]; exists {
		credential, authScheme, authHeader = value, "x-api-key", ""
	} else if strings.TrimSpace(credential) == "" {
		authScheme, authHeader = "none", ""
	}
	return Provider{
		ID: id, Name: providerID + " default", APIURL: strings.TrimSpace(source.BaseURL), APIKey: credential,
		Enabled: true, Level: 1, SupportedModels: models, Headers: headers,
		PiPlatform: providerID, UpstreamProtocol: protocol, AuthScheme: authScheme, AuthHeader: authHeader,
	}, nil
}

func piAuthAPIKey(raw json.RawMessage, exists bool) (string, bool, error) {
	if !exists {
		return "", false, nil
	}
	var entry PiAuthEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", false, fmt.Errorf("认证条目不是有效对象: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(entry.Type), "api_key") {
		return "", false, fmt.Errorf("暂不支持从 auth.json 导入 type=%q 的认证，请先改用 API Key", entry.Type)
	}
	if strings.TrimSpace(entry.Key) == "" {
		return "", false, fmt.Errorf("auth.json 的 API Key 为空")
	}
	return entry.Key, true, nil
}

func piSupplierDefaults(api string) (protocol, authScheme, authHeader string) {
	switch strings.TrimSpace(api) {
	case "anthropic-messages":
		return string(UpstreamProtocolAnthropic), "x-api-key", ""
	case "google-generative-ai":
		return string(UpstreamProtocolGoogle), "custom", "x-goog-api-key"
	case "openai-responses", "openai-codex-responses", "azure-openai-responses":
		return string(UpstreamProtocolOpenAIResponses), "bearer", ""
	default:
		return string(UpstreamProtocolOpenAIChat), "bearer", ""
	}
}

func buildManagedPiPlatformRaw(original json.RawMessage, baseURL string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(original, &fields); err != nil {
		return nil, err
	}
	fields["baseUrl"], _ = json.Marshal(baseURL)
	fields["apiKey"], _ = json.Marshal(piGatewayToken)
	return marshalOrderedPiProvider(fields)
}

func marshalOrderedPiProvider(fields map[string]json.RawMessage) (json.RawMessage, error) {
	order := []string{"baseUrl", "apiKey", "api", "headers", "authHeader", "compat", "models", "modelOverrides", "name"}
	known := make(map[string]struct{}, len(order))
	keys := make([]string, 0, len(fields))
	for _, key := range order {
		known[key] = struct{}{}
		if _, exists := fields[key]; exists {
			keys = append(keys, key)
		}
	}
	extras := make([]string, 0)
	for key := range fields {
		if _, exists := known[key]; !exists {
			extras = append(extras, key)
		}
	}
	sort.Strings(extras)
	keys = append(keys, extras...)

	var builder strings.Builder
	builder.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		builder.Write(encodedKey)
		builder.WriteByte(':')
		value := fields[key]
		if !json.Valid(value) {
			return nil, fmt.Errorf("Pi Provider 字段 %q 不是合法 JSON", key)
		}
		builder.Write(value)
	}
	builder.WriteByte('}')
	return json.RawMessage(builder.String()), nil
}

func writePiModelsRoot(path string, root, platforms map[string]json.RawMessage) error {
	encoded, err := json.Marshal(platforms)
	if err != nil {
		return err
	}
	root["providers"] = encoded
	if err := AtomicWriteJSON(path, root); err != nil {
		return fmt.Errorf("保存 Pi models.json 失败: %w", err)
	}
	return nil
}

func (s *PiSettingsService) loadPlatformState() (PiPlatformStateDocument, error) {
	state := PiPlatformStateDocument{Version: piPlatformStateVersion, ModelsPath: s.modelsPath(), Platforms: map[string]PiPlatformProxyState{}}
	statePath, err := s.platformStateFile()
	if err != nil {
		return state, err
	}
	if err := ReadJSONFile(statePath, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("读取 Pi 平台托管状态失败: %w", err)
	}
	if state.Version != piPlatformStateVersion {
		return state, fmt.Errorf("不支持的 Pi 平台托管状态版本: %d", state.Version)
	}
	if state.Platforms == nil {
		state.Platforms = map[string]PiPlatformProxyState{}
	}
	return state, nil
}

func (s *PiSettingsService) savePlatformState(state PiPlatformStateDocument) error {
	statePath, err := s.platformStateFile()
	if err != nil {
		return err
	}
	if len(state.Platforms) == 0 {
		if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("清理 Pi 平台托管状态失败: %w", err)
		}
		return nil
	}
	state.Version = piPlatformStateVersion
	state.ModelsPath = s.modelsPath()
	if err := AtomicWriteJSON(statePath, state); err != nil {
		return fmt.Errorf("保存 Pi 平台托管状态失败: %w", err)
	}
	return nil
}

func (s *PiSettingsService) platformStateFile() (string, error) {
	if path := strings.TrimSpace(s.platformStatePath); path != "" {
		return path, nil
	}
	if dir := strings.TrimSpace(s.configDir); dir != "" {
		return filepath.Join(dir, ".code-switch-r-platform-state.json"), nil
	}
	return "", fmt.Errorf("Pi 平台托管状态路径不可用")
}

func (s *PiSettingsService) platformBaseURL(providerID string) string {
	return strings.TrimSuffix(s.relayRootURL(), "/") + "/pi/providers/" + url.PathEscape(providerID)
}

func (s *PiSettingsService) relayRootURL() string {
	addr := strings.TrimSpace(s.relayAddr)
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	if addr == "" {
		addr = "127.0.0.1:18100"
	}
	return "http://" + strings.TrimSuffix(addr, "/")
}

func piManagedProviderConnectionMatches(raw json.RawMessage, managedBaseURL string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	var baseURL, apiKey string
	_ = json.Unmarshal(fields["baseUrl"], &baseURL)
	_ = json.Unmarshal(fields["apiKey"], &apiKey)
	return sameURL(baseURL, managedBaseURL) && apiKey == piGatewayToken && piManagedProviderRoutingCompatible(raw)
}

func piManagedProviderRoutingCompatible(raw json.RawMessage) bool {
	var source piModelsProviderFile
	if err := json.Unmarshal(raw, &source); err != nil {
		return false
	}
	if _, supported := piManagedAPIs[strings.TrimSpace(source.API)]; !supported {
		return false
	}
	for _, model := range source.Models {
		if strings.TrimSpace(model.BaseURL) != "" {
			return false
		}
		if api := strings.TrimSpace(model.API); api != "" {
			if _, supported := piManagedAPIs[api]; !supported {
				return false
			}
		}
	}
	return true
}

func piManagedAuthMatches(authRoot map[string]json.RawMessage, providerID string, entry PiPlatformProxyState) bool {
	if entry.InjectedAuthHash == "" {
		return true
	}
	raw, exists := authRoot[providerID]
	return exists && canonicalJSONHash(raw) == entry.InjectedAuthHash
}

func piProviderConnectionSnapshot(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	snapshot := make(map[string]json.RawMessage, 2)
	for _, key := range []string{"baseUrl", "apiKey"} {
		if value, exists := fields[key]; exists {
			snapshot[key] = cloneRawMessage(value)
		}
	}
	return marshalOrderedPiProvider(snapshot)
}

func restorePiProviderConnection(current, original json.RawMessage) (json.RawMessage, error) {
	var currentFields, originalFields map[string]json.RawMessage
	if err := json.Unmarshal(current, &currentFields); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(original, &originalFields); err != nil {
		return nil, err
	}
	for _, key := range []string{"baseUrl", "apiKey"} {
		if value, exists := originalFields[key]; exists {
			currentFields[key] = cloneRawMessage(value)
		} else {
			delete(currentFields, key)
		}
	}
	return marshalOrderedPiProvider(currentFields)
}

func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return data, err == nil, err
}

func restoreOptionalFile(path string, data []byte, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicWriteFile(path, data, 0o644)
}

var piManagedAPIs = map[string]struct{}{
	"openai-completions": {}, "openai-responses": {}, "openai-codex-responses": {},
	"anthropic-messages": {}, "google-generative-ai": {},
}
