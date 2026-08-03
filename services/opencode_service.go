package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type OpenCodeService struct {
	mu            sync.Mutex
	providerStore *ProviderService
	relayAddr     string
}

func NewOpenCodeService(providerStore *ProviderService, relayAddr string) *OpenCodeService {
	return &OpenCodeService{providerStore: providerStore, relayAddr: relayAddr}
}

func (s *OpenCodeService) Start() error { return nil }
func (s *OpenCodeService) Stop() error  { return nil }

func (s *OpenCodeService) Snapshot() (OpenCodeConfigSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *OpenCodeService) GetConfigPathInfo() (OpenCodeConfigInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	return snapshot.Config, err
}

func (s *OpenCodeService) SetConfigPath(input OpenCodePathInput) (OpenCodeConfigInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := strings.TrimSpace(input.Path)
	if value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return OpenCodeConfigInfo{}, fmt.Errorf("解析 OpenCode 配置路径失败: %w", err)
		}
		value = filepath.Clean(absolute)
	}
	if err := saveOpenCodeSettings(value); err != nil {
		return OpenCodeConfigInfo{}, err
	}
	snapshot, err := s.snapshotLocked()
	return snapshot.Config, err
}

func (s *OpenCodeService) SetDefaultModels(input OpenCodeDefaultModelsInput) (OpenCodeConfigSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providerStore == nil {
		return OpenCodeConfigSnapshot{}, fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	path, format, original, document, _, state, err := s.readDocumentForWriteLocked()
	if err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	for label, value := range map[string]string{"model": strings.TrimSpace(input.Model), "small_model": strings.TrimSpace(input.SmallModel)} {
		if value == "" {
			delete(document.Raw, label)
			continue
		}
		if !openCodeModelReferenceExists(document, value) {
			return OpenCodeConfigSnapshot{}, fmt.Errorf("OpenCode %s 必须引用已存在的 provider/model: %s", label, value)
		}
		data, _ := json.Marshal(value)
		document.Raw[label] = data
	}
	if _, _, err := s.persistDocumentLocked(path, format, original, document, state); err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	return s.snapshotLocked()
}

func (s *OpenCodeService) GetGlobalPrompt() (OpenCodePromptInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _, _, err := s.resolveTarget()
	if err != nil {
		return OpenCodePromptInfo{}, err
	}
	promptPath := filepath.Join(filepath.Dir(path), "AGENTS.md")
	data, readErr := os.ReadFile(promptPath)
	if os.IsNotExist(readErr) {
		return OpenCodePromptInfo{Path: promptPath, Hash: sha256Hex(nil)}, nil
	}
	if readErr != nil {
		return OpenCodePromptInfo{}, fmt.Errorf("读取 OpenCode AGENTS.md 失败: %w", readErr)
	}
	return OpenCodePromptInfo{Path: promptPath, Hash: sha256Hex(data), Exists: true, Content: string(data)}, nil
}

func (s *OpenCodeService) SaveGlobalPrompt(content, expectedHash string) (OpenCodePromptInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.getGlobalPromptLocked()
	if err != nil {
		return OpenCodePromptInfo{}, err
	}
	if strings.TrimSpace(expectedHash) == "" || expectedHash != current.Hash {
		return OpenCodePromptInfo{}, fmt.Errorf("OpenCode AGENTS.md 已被外部修改，拒绝覆盖")
	}
	if err := AtomicWriteText(current.Path, content); err != nil {
		return OpenCodePromptInfo{}, fmt.Errorf("写入 OpenCode AGENTS.md 失败: %w", err)
	}
	return s.getGlobalPromptLocked()
}

func (s *OpenCodeService) ListMCPServers() ([]OpenCodeMCPServerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _, _, err := s.resolveTarget()
	if err != nil {
		return nil, err
	}
	_, document, _, err := readOpenCodeDocument(path)
	if err != nil {
		return nil, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return nil, err
	}
	return openCodeMCPInfosForState(document.Raw["mcp"], state, path)
}

func (s *OpenCodeService) SaveMCPServer(input OpenCodeMCPServerInput) ([]OpenCodeMCPServerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(input.Key)
	if err := validateOpenCodeProviderKey(key); err != nil {
		return nil, fmt.Errorf("MCP key 无效: %w", err)
	}
	serverType := strings.TrimSpace(strings.ToLower(input.Type))
	if serverType != "local" && serverType != "remote" {
		return nil, fmt.Errorf("OpenCode MCP type 必须是 local 或 remote")
	}
	path, format, original, document, _, state, err := s.readDocumentForWriteLocked()
	if err != nil {
		return nil, err
	}
	mcp, err := providerRawMap(document.Raw["mcp"])
	if err != nil {
		return nil, fmt.Errorf("解析 OpenCode mcp 失败: %w", err)
	}
	managedKey := openCodeProviderStorageKey(path, key)
	managed, owned := state.MCP[managedKey]
	if _, exists := mcp[key]; exists && !owned {
		return nil, fmt.Errorf("OpenCode MCP %q 尚未由本项目托管，请先 Claim 后再修改", key)
	}
	server, err := providerRawMap(mcp[key])
	if err != nil {
		return nil, err
	}
	setRawString(server, "type", serverType, false)
	if serverType == "remote" {
		setRawString(server, "url", strings.TrimSpace(input.URL), true)
		if input.Headers != nil {
			data, _ := json.Marshal(input.Headers)
			server["headers"] = data
		}
		delete(server, "command")
		delete(server, "environment")
	} else {
		if len(input.Command) == 0 {
			return nil, fmt.Errorf("local MCP 必须提供 command")
		}
		data, _ := json.Marshal(input.Command)
		server["command"] = data
		if input.Environment != nil {
			data, _ := json.Marshal(input.Environment)
			server["environment"] = data
		}
		delete(server, "url")
		delete(server, "headers")
	}
	serverData, _ := json.Marshal(server)
	mcp[key] = serverData
	document.Raw["mcp"], _ = json.Marshal(mcp)
	oldState := cloneOpenCodeState(state)
	_, _, err = s.persistDocumentLocked(path, format, original, document, state)
	if err != nil {
		return nil, err
	}
	state.MCP[managedKey] = openCodeManagedMCPState{
		TargetPath: path, Key: key, OriginalServer: cloneRaw(managed.OriginalServer),
		InjectedServer: cloneRaw(serverData), OriginalHash: sha256Hex(managed.OriginalServer),
		InjectedHash: sha256Hex(serverData), UpdatedAt: openCodeTimeNow(),
	}
	if !owned {
		state.MCP[managedKey] = openCodeManagedMCPState{
			TargetPath: path, Key: key, OriginalServer: nil,
			InjectedServer: cloneRaw(serverData), OriginalHash: sha256Hex(nil),
			InjectedHash: sha256Hex(serverData), UpdatedAt: openCodeTimeNow(),
		}
	}
	if err := saveOpenCodeState(state); err != nil {
		_ = restoreOpenCodeFile(path, original, original != nil)
		_ = saveOpenCodeState(oldState)
		return nil, fmt.Errorf("保存 OpenCode MCP 托管状态失败，配置已回滚: %w", err)
	}
	return openCodeMCPInfosForState(document.Raw["mcp"], state, path)
}

func (s *OpenCodeService) ClaimMCPServer(key string) ([]OpenCodeMCPServerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key = strings.TrimSpace(key)
	if err := validateOpenCodeProviderKey(key); err != nil {
		return nil, err
	}
	path, _, _, document, _, state, err := s.readDocumentForWriteLocked()
	if err != nil {
		return nil, err
	}
	mcp, err := providerRawMap(document.Raw["mcp"])
	if err != nil {
		return nil, err
	}
	server, exists := mcp[key]
	if !exists {
		return nil, fmt.Errorf("未找到 OpenCode MCP %q", key)
	}
	managedKey := openCodeProviderStorageKey(path, key)
	if _, owned := state.MCP[managedKey]; !owned {
		state.MCP[managedKey] = openCodeManagedMCPState{
			TargetPath: path, Key: key, OriginalServer: cloneRaw(server),
			InjectedServer: cloneRaw(server), OriginalHash: sha256Hex(server),
			InjectedHash: sha256Hex(server), UpdatedAt: openCodeTimeNow(),
		}
		if err := saveOpenCodeState(state); err != nil {
			return nil, err
		}
	}
	return openCodeMCPInfosForState(document.Raw["mcp"], state, path)
}

func (s *OpenCodeService) DeleteMCPServer(key string) ([]OpenCodeMCPServerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key = strings.TrimSpace(key)
	if err := validateOpenCodeProviderKey(key); err != nil {
		return nil, err
	}
	path, format, original, document, _, state, err := s.readDocumentForWriteLocked()
	if err != nil {
		return nil, err
	}
	mcp, err := providerRawMap(document.Raw["mcp"])
	if err != nil {
		return nil, err
	}
	managedKey := openCodeProviderStorageKey(path, key)
	managed, owned := state.MCP[managedKey]
	if !owned {
		return nil, fmt.Errorf("OpenCode MCP %q 不属于本项目，拒绝删除", key)
	}
	currentServer, exists := mcp[key]
	if !exists || sha256Hex(currentServer) != managed.InjectedHash {
		return nil, fmt.Errorf("OpenCode MCP %q 已被外部修改，拒绝删除", key)
	}
	if len(managed.OriginalServer) == 0 {
		delete(mcp, key)
	} else {
		mcp[key] = cloneRaw(managed.OriginalServer)
	}
	if len(mcp) == 0 {
		delete(document.Raw, "mcp")
	} else {
		document.Raw["mcp"], _ = json.Marshal(mcp)
	}
	oldState := cloneOpenCodeState(state)
	if _, _, err := s.persistDocumentLocked(path, format, original, document, state); err != nil {
		return nil, err
	}
	delete(state.MCP, managedKey)
	if err := saveOpenCodeState(state); err != nil {
		_ = restoreOpenCodeFile(path, original, original != nil)
		_ = saveOpenCodeState(oldState)
		return nil, fmt.Errorf("保存 OpenCode MCP 删除状态失败，配置已回滚: %w", err)
	}
	return openCodeMCPInfosForState(document.Raw["mcp"], state, path)
}

func (s *OpenCodeService) Diagnostics() (OpenCodeDiagnostics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, source, _, err := s.resolveTarget()
	if err != nil {
		return OpenCodeDiagnostics{}, err
	}
	return OpenCodeDiagnostics{
		ConfigPathEnvSet:    os.Getenv("OPENCODE_CONFIG") != "",
		ConfigDirEnvSet:     os.Getenv("OPENCODE_CONFIG_DIR") != "",
		ConfigPath:          path,
		ConfigSource:        source,
		RelayConfigured:     strings.TrimSpace(RelayToken()) != "",
		RelayAddressEnvSet:  strings.TrimSpace(os.Getenv("CODE_SWITCH_RELAY_ADDR")) != "",
		AnthropicKeyEnvSet:  os.Getenv("ANTHROPIC_API_KEY") != "",
		OpenAIKeyEnvSet:     os.Getenv("OPENAI_API_KEY") != "",
		GeminiKeyEnvSet:     os.Getenv("GEMINI_API_KEY") != "",
		EnvironmentWarnings: openCodeEnvironmentWarnings(),
	}, nil
}

func (s *OpenCodeService) ImportLiveProviders() ([]OpenCodeProviderInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providerStore == nil {
		return nil, fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	path, _, _, err := s.resolveTarget()
	if err != nil {
		return nil, err
	}
	_, document, hash, err := readOpenCodeDocument(path)
	if err != nil {
		return nil, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return nil, err
	}
	if conflicts := openCodeManagedConflicts(document, state, path); len(conflicts) > 0 {
		return nil, fmt.Errorf("OpenCode live 配置存在托管冲突，拒绝导入: %s", strings.Join(conflicts, ", "))
	}
	existing, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]Provider, len(existing))
	for _, provider := range existing {
		if provider.openCode != nil && provider.openCode.ProviderKey != "" {
			byKey[provider.openCode.ProviderKey] = provider
		}
	}
	for key, raw := range document.Providers {
		if _, exists := byKey[key]; exists {
			continue
		}
		provider, err := providerFromOpenCodeRaw(key, raw)
		if err != nil {
			return nil, err
		}
		byKey[key] = provider
	}
	providers := make([]Provider, 0, len(byKey))
	for _, provider := range byKey {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].openCode.ProviderKey < providers[j].openCode.ProviderKey
	})
	if err := s.saveOpenCodeProviders(providers); err != nil {
		return nil, err
	}
	format := openCodeFormatForPath(path)
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: hash, UpdatedAt: openCodeTimeNow()}
	if err := saveOpenCodeState(state); err != nil {
		_ = s.saveOpenCodeProviders(cloneOpenCodeProviders(existing))
		return nil, fmt.Errorf("保存 OpenCode 导入基线失败: %w", err)
	}
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return nil, err
	}
	return snapshot.Providers, nil
}

func (s *OpenCodeService) SaveProvider(input OpenCodeProviderInput) (OpenCodeProviderInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	provider, err := s.saveProviderLocked(input)
	if err != nil {
		return OpenCodeProviderInfo{}, err
	}
	return openCodeProviderInfo(provider, nil), nil
}

func (s *OpenCodeService) RenameProviderKey(oldKey, newKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if err := validateOpenCodeProviderKey(oldKey); err != nil {
		return err
	}
	if err := validateOpenCodeProviderKey(newKey); err != nil {
		return err
	}
	if oldKey == newKey {
		return nil
	}
	if s.providerStore == nil {
		return fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return err
	}
	var renamed Provider
	found := false
	for _, provider := range providers {
		if provider.openCode == nil {
			continue
		}
		if provider.openCode.ProviderKey == newKey {
			return fmt.Errorf("OpenCode Provider key %q 已存在", newKey)
		}
		if provider.openCode.ProviderKey == oldKey {
			renamed = provider
			found = true
		}
	}
	if !found {
		return fmt.Errorf("未找到 OpenCode Provider %q", oldKey)
	}

	path, _, format, err := s.resolveTarget()
	if err != nil {
		return err
	}
	originalBytes, document, currentHash, err := readOpenCodeDocument(path)
	if err != nil {
		return err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return err
	}
	oldState := cloneOpenCodeState(state)
	if target := state.Targets[path]; target.LastHash != "" && target.LastHash != currentHash {
		return fmt.Errorf("OpenCode 配置已被外部修改: %s", path)
	}

	oldRaw, liveExists := document.Providers[oldKey]
	if _, newLiveExists := document.Providers[newKey]; newLiveExists {
		return fmt.Errorf("live OpenCode 配置已存在 Provider key %q", newKey)
	}
	managedKey := openCodeProviderStorageKey(path, oldKey)
	_, managedExists := state.Managed[managedKey]
	if liveExists && !managedExists {
		return fmt.Errorf("OpenCode Provider %q 尚未由本项目托管，改名前请先显式应用一次", oldKey)
	}
	if !liveExists && managedExists {
		return fmt.Errorf("OpenCode Provider %q 的托管状态与 live 配置不一致，请先重新读取配置", oldKey)
	}
	var writtenHash string
	if liveExists {
		document.Providers[newKey] = cloneRaw(oldRaw)
		delete(document.Providers, oldKey)
		renameOpenCodeDocumentModelReferences(document.Raw, oldKey, newKey)
		nextDocument, marshalErr := marshalOpenCodeDocument(document)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := writeOpenCodeDocument(path, originalBytes, nextDocument); writeErr != nil {
			return writeErr
		}
		_, _, writtenHash, err = readOpenCodeDocument(path)
		if err != nil {
			_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
			return fmt.Errorf("OpenCode 配置改名后校验失败: %w", err)
		}
	}

	renamed.Name = newKey
	renamed.openCode = cloneOpenCodePayload(renamed.openCode)
	renamed.openCode.ProviderKey = newKey
	if renamed.openCode.GatewayKey == oldKey {
		renamed.openCode.GatewayKey = newKey
	}
	updated := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider.openCode != nil && provider.openCode.ProviderKey == oldKey {
			updated = append(updated, renamed)
		} else {
			updated = append(updated, provider)
		}
	}
	if err := s.saveOpenCodeProviders(updated); err != nil {
		if liveExists {
			_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		}
		return fmt.Errorf("保存 OpenCode Provider 改名失败，配置已回滚: %w", err)
	}
	if managed, managedExists := state.Managed[managedKey]; managedExists {
		delete(state.Managed, managedKey)
		managed.ProviderKey = newKey
		state.Managed[openCodeProviderStorageKey(path, newKey)] = managed
	}
	if liveExists {
		state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: writtenHash, UpdatedAt: openCodeTimeNow()}
	}
	if err := saveOpenCodeState(state); err != nil {
		_ = s.saveOpenCodeProviders(cloneOpenCodeProviders(providers))
		if liveExists {
			_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		}
		_ = saveOpenCodeState(oldState)
		return fmt.Errorf("保存 OpenCode 改名状态失败，已尝试回滚: %w", err)
	}
	return nil
}

func (s *OpenCodeService) DeleteProvider(providerKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	providerKey = strings.TrimSpace(providerKey)
	if err := validateOpenCodeProviderKey(providerKey); err != nil {
		return err
	}
	if s.providerStore == nil {
		return fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return err
	}
	found := false
	for _, provider := range providers {
		if provider.openCode != nil && provider.openCode.ProviderKey == providerKey {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("未找到 OpenCode Provider %q", providerKey)
	}

	path, _, format, err := s.resolveTarget()
	if err != nil {
		return err
	}
	originalBytes, document, currentHash, err := readOpenCodeDocument(path)
	if err != nil {
		return err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return err
	}
	if target := state.Targets[path]; target.LastHash != "" && target.LastHash != currentHash {
		return fmt.Errorf("OpenCode 配置已被外部修改: %s", path)
	}
	managedKey := openCodeProviderStorageKey(path, providerKey)
	managed, managedExists := state.Managed[managedKey]
	currentProvider, liveExists := document.Providers[providerKey]
	if liveExists && !managedExists {
		return fmt.Errorf("OpenCode Provider %q 尚未由本项目托管，拒绝删除 live 配置", providerKey)
	}
	if managedExists {
		if !liveExists {
			return fmt.Errorf("OpenCode Provider %q 的托管状态与 live 配置不一致，请先重新读取配置", providerKey)
		}
		if sha256Hex(currentProvider) != managed.InjectedHash {
			return fmt.Errorf("OpenCode Provider %q 已被外部修改，拒绝删除", providerKey)
		}
		if len(managed.OriginalProvider) == 0 {
			delete(document.Providers, providerKey)
			removeOpenCodeProviderReferences(&document, providerKey)
		} else {
			document.Providers[providerKey] = cloneRaw(managed.OriginalProvider)
		}
	}

	oldProviders := cloneOpenCodeProviders(providers)
	oldState := cloneOpenCodeState(state)
	fileChanged := false
	var writtenHash = currentHash
	if managedExists {
		nextDocument, marshalErr := marshalOpenCodeDocument(document)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := writeOpenCodeDocument(path, originalBytes, nextDocument); writeErr != nil {
			return writeErr
		}
		fileChanged = true
		_, _, writtenHash, err = readOpenCodeDocument(path)
		if err != nil {
			_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
			return fmt.Errorf("OpenCode Provider 删除后校验失败: %w", err)
		}
		delete(state.Managed, managedKey)
		state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: writtenHash, UpdatedAt: openCodeTimeNow()}
	}
	filtered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider.openCode != nil && provider.openCode.ProviderKey == providerKey {
			continue
		}
		filtered = append(filtered, provider)
	}
	if err := s.saveOpenCodeProviders(filtered); err != nil {
		if fileChanged {
			_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		}
		return err
	}
	if managedExists {
		if err := saveOpenCodeState(state); err != nil {
			_ = s.saveOpenCodeProviders(oldProviders)
			_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
			_ = saveOpenCodeState(oldState)
			return fmt.Errorf("保存 OpenCode Provider 删除状态失败，已尝试回滚: %w", err)
		}
	}
	return nil
}

func (s *OpenCodeService) ApplyProvider(providerKey string) (OpenCodeApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyProviderLocked(strings.TrimSpace(providerKey))
}

func (s *OpenCodeService) RestoreProvider(providerKey string) (OpenCodeApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restoreProviderLocked(strings.TrimSpace(providerKey))
}

func (s *OpenCodeService) saveProviderLocked(input OpenCodeProviderInput) (Provider, error) {
	if s.providerStore == nil {
		return Provider{}, fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	key := strings.TrimSpace(input.ProviderKey)
	if err := validateOpenCodeProviderKey(key); err != nil {
		return Provider{}, err
	}
	npm := normalizeOpenCodeNPM(input.NPM)
	clientProtocol, err := normalizeOpenCodeClientProtocol(npm, input.ClientProtocol)
	if err != nil {
		return Provider{}, err
	}
	upstreamProtocol, err := normalizeOpenCodeUpstreamProtocol(input.UpstreamProtocol)
	if err != nil {
		return Provider{}, err
	}
	mode, err := normalizeOpenCodeMode(input.Mode)
	if err != nil {
		return Provider{}, err
	}
	if _, knownNPM := openCodeClientProtocolForNPM(npm); !knownNPM && mode == "relay" {
		return Provider{}, fmt.Errorf("未知 npm 包 %s 只能使用 direct 模式，不能接入 Relay", npm)
	}
	if input.GatewayKey == "" {
		input.GatewayKey = key
	}
	if err := validateOpenCodeProviderKey(input.GatewayKey); err != nil {
		return Provider{}, fmt.Errorf("gatewayKey 无效: %w", err)
	}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return Provider{}, err
	}
	var existing *Provider
	for index := range providers {
		if providers[index].openCode != nil && providers[index].openCode.ProviderKey == key {
			copy := providers[index]
			existing = &copy
			break
		}
	}
	if existing != nil && strings.TrimSpace(input.APIKey) == "" {
		input.APIKey = existing.APIKey
	}
	baseRaw := make(map[string]json.RawMessage)
	if existing != nil && existing.openCode != nil {
		baseRaw, err = providerRawMap(existing.openCode.RawProvider)
		if err != nil {
			return Provider{}, fmt.Errorf("解析 OpenCode Provider %q 原始配置失败: %w", key, err)
		}
	}
	setRawString(baseRaw, "name", strings.TrimSpace(input.Name), true)
	setRawString(baseRaw, "npm", npm, false)
	options, err := optionsRawMap(baseRaw)
	if err != nil {
		return Provider{}, fmt.Errorf("解析 OpenCode Provider %q options 失败: %w", key, err)
	}
	if err := mergeOpenCodeJSONExtension(options, input.OptionsJSON, "options 扩展字段"); err != nil {
		return Provider{}, fmt.Errorf("OpenCode Provider %q %w", key, err)
	}
	setRawString(options, "baseURL", strings.TrimSpace(input.BaseURL), true)
	if strings.TrimSpace(input.APIKey) != "" {
		setRawString(options, "apiKey", input.APIKey, false)
	} else {
		delete(options, "apiKey")
	}
	if strings.TrimSpace(input.HeadersJSON) != "" {
		var headers map[string]any
		if err := json.Unmarshal([]byte(input.HeadersJSON), &headers); err != nil || headers == nil {
			return Provider{}, fmt.Errorf("OpenCode Provider %q headers_json 必须是 JSON 对象", key)
		}
		data, _ := json.Marshal(headers)
		options["headers"] = data
	}
	if input.Timeout > 0 {
		data, _ := json.Marshal(input.Timeout)
		options["timeout"] = data
	}
	optionsData, _ := json.Marshal(options)
	baseRaw["options"] = optionsData
	if input.Models != nil {
		models, err := openCodeProviderModelMap(baseRaw)
		if err != nil {
			return Provider{}, err
		}
		updatedModels := make(map[string]json.RawMessage, len(input.Models))
		for _, modelInput := range input.Models {
			modelInput.ID = strings.TrimSpace(modelInput.ID)
			if modelInput.ID == "" {
				return Provider{}, fmt.Errorf("OpenCode 模型 ID 不能为空")
			}
			if _, exists := updatedModels[modelInput.ID]; exists {
				return Provider{}, fmt.Errorf("OpenCode Provider %q 存在重复模型 ID: %s", key, modelInput.ID)
			}
			modelRaw, err := buildModelRaw(modelInput, models[modelInput.ID])
			if err != nil {
				return Provider{}, err
			}
			updatedModels[modelInput.ID] = modelRaw
		}
		if len(updatedModels) == 0 {
			delete(baseRaw, "models")
		} else {
			modelsData, _ := json.Marshal(updatedModels)
			baseRaw["models"] = modelsData
		}
	}
	rawData, err := json.Marshal(baseRaw)
	if err != nil {
		return Provider{}, fmt.Errorf("序列化 OpenCode Provider %q 失败: %w", key, err)
	}
	provider := Provider{
		Name:             key,
		APIURL:           strings.TrimSpace(input.BaseURL),
		APIKey:           input.APIKey,
		Enabled:          input.Enabled,
		Level:            input.Level,
		UpstreamProtocol: string(upstreamProtocol),
		SupportedModels:  openCodeSupportedModels(baseRaw),
		openCode: &openCodeProviderPayload{
			ProviderKey: key, NPM: npm, ClientProtocol: clientProtocol,
			Mode: mode, GatewayKey: input.GatewayKey, RawProvider: rawData,
		},
	}
	if existing != nil {
		provider.ID = existing.ID
		provider.ModelMapping = existing.ModelMapping
		provider.AuthScheme = existing.AuthScheme
		provider.AuthHeader = existing.AuthHeader
		provider.Headers = existing.Headers
		provider.ProxyEnabled = existing.ProxyEnabled
		if provider.APIURL == "" {
			provider.APIURL = existing.APIURL
		}
		if provider.APIKey == "" {
			provider.APIKey = existing.APIKey
		}
		if provider.openCode.RawProvider == nil {
			provider.openCode.RawProvider = existing.openCode.RawProvider
		}
	}
	if provider.APIURL == "" && mode == "direct" {
		return Provider{}, fmt.Errorf("OpenCode Provider %q 的 Base URL 不能为空", key)
	}
	if provider.APIKey == "" && mode == "direct" {
		return Provider{}, fmt.Errorf("OpenCode Provider %q 的 API Key 不能为空", key)
	}
	updated := make([]Provider, 0, len(providers)+1)
	replaced := false
	for _, item := range providers {
		if item.openCode != nil && item.openCode.ProviderKey == key {
			updated = append(updated, provider)
			replaced = true
		} else {
			updated = append(updated, item)
		}
	}
	if !replaced {
		updated = append(updated, provider)
	}
	if err := s.saveOpenCodeProviders(updated); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *OpenCodeService) saveOpenCodeProviders(providers []Provider) error {
	if s.providerStore == nil {
		return fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	deleted, err := replaceProvidersInDB(context.Background(), providerScope{platform: openCodePlatform}, providers)
	if err != nil {
		return err
	}
	if len(deleted) > 0 {
		if cleanupErr := cleanupDeletedProviders(openCodePlatform, deleted); cleanupErr != nil {
			logWarn("清理已删除 OpenCode Provider 关联数据失败", "error", cleanupErr)
		}
	}
	return nil
}

func (s *OpenCodeService) resolveTarget() (string, string, string, error) {
	explicit, err := loadOpenCodeSettings()
	if err != nil {
		return "", "", "", err
	}
	return resolveOpenCodeConfigPath(explicit)
}

func (s *OpenCodeService) snapshotLocked() (OpenCodeConfigSnapshot, error) {
	if s.providerStore == nil {
		return OpenCodeConfigSnapshot{}, fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	path, source, format, err := s.resolveTarget()
	if err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	original, document, hash, err := readOpenCodeDocument(path)
	if err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	target := state.Targets[path]
	warning := ""
	if len(original) > 0 && hasJSONCComment(original) {
		warning = "JSONC 注释将在写入时规范化，写入前会创建备份"
	}
	config := openCodeTargetInfo(path, source, format, hash, openCodeFileExists(path), warning, len(document.Providers), document.Raw)
	config.ReadAt = openCodeTimeNow()
	config.Conflict = target.LastHash != "" && target.LastHash != hash
	if config.Conflict {
		config.Warning = "OpenCode 配置已被外部修改，应用前需要重新读取"
	}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	byKey := make(map[string]Provider, len(providers))
	for _, provider := range providers {
		if provider.openCode != nil && provider.openCode.ProviderKey != "" {
			byKey[provider.openCode.ProviderKey] = provider
		}
	}
	keys := make([]string, 0, len(document.Providers)+len(byKey))
	seen := make(map[string]struct{})
	for key := range document.Providers {
		keys = append(keys, key)
		seen[key] = struct{}{}
	}
	for key := range byKey {
		if _, exists := seen[key]; !exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make([]OpenCodeProviderInfo, 0, len(keys))
	for _, key := range keys {
		provider, exists := byKey[key]
		if !exists {
			provider, err = providerFromOpenCodeRaw(key, document.Providers[key])
			if err != nil {
				return OpenCodeConfigSnapshot{}, err
			}
		}
		managed := state.Managed[openCodeProviderStorageKey(path, key)]
		result = append(result, openCodeProviderInfo(provider, &managed))
	}
	return OpenCodeConfigSnapshot{
		Config: config, Providers: result,
		DefaultModel: document.DefaultModel, SmallModel: document.SmallModel,
		Warnings: compactWarnings(warning),
	}, nil
}

func (s *OpenCodeService) readDocumentForWriteLocked() (string, string, []byte, openCodeConfigDocument, string, openCodeStateFile, error) {
	path, _, format, err := s.resolveTarget()
	if err != nil {
		return "", "", nil, openCodeConfigDocument{}, "", openCodeStateFile{}, err
	}
	original, document, hash, err := readOpenCodeDocument(path)
	if err != nil {
		return "", "", nil, openCodeConfigDocument{}, "", openCodeStateFile{}, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return "", "", nil, openCodeConfigDocument{}, "", openCodeStateFile{}, err
	}
	if target := state.Targets[path]; target.LastHash != "" && target.LastHash != hash {
		return "", "", nil, openCodeConfigDocument{}, "", openCodeStateFile{}, fmt.Errorf("OpenCode 配置已被外部修改: %s", path)
	}
	return path, format, original, document, hash, state, nil
}

func (s *OpenCodeService) persistDocumentLocked(path, format string, original []byte, document openCodeConfigDocument, state openCodeStateFile) (string, string, error) {
	data, err := marshalOpenCodeDocument(document)
	if err != nil {
		return "", "", err
	}
	warning, err := writeOpenCodeDocument(path, original, data)
	if err != nil {
		return "", "", err
	}
	_, _, hash, err := readOpenCodeDocument(path)
	if err != nil {
		_ = restoreOpenCodeFile(path, original, original != nil)
		return "", "", fmt.Errorf("OpenCode 配置回读校验失败: %w", err)
	}
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: hash, UpdatedAt: openCodeTimeNow()}
	if err := saveOpenCodeState(state); err != nil {
		_ = restoreOpenCodeFile(path, original, original != nil)
		return "", "", err
	}
	return hash, warning, nil
}

func (s *OpenCodeService) getGlobalPromptLocked() (OpenCodePromptInfo, error) {
	path, _, _, err := s.resolveTarget()
	if err != nil {
		return OpenCodePromptInfo{}, err
	}
	promptPath := filepath.Join(filepath.Dir(path), "AGENTS.md")
	data, readErr := os.ReadFile(promptPath)
	if os.IsNotExist(readErr) {
		return OpenCodePromptInfo{Path: promptPath, Hash: sha256Hex(nil)}, nil
	}
	if readErr != nil {
		return OpenCodePromptInfo{}, fmt.Errorf("读取 OpenCode AGENTS.md 失败: %w", readErr)
	}
	return OpenCodePromptInfo{Path: promptPath, Hash: sha256Hex(data), Exists: true, Content: string(data)}, nil
}

func openCodeModelReferenceExists(document openCodeConfigDocument, reference string) bool {
	reference = strings.TrimSpace(reference)
	separator := strings.IndexByte(reference, '/')
	if separator <= 0 || separator == len(reference)-1 {
		return false
	}
	providerKey := reference[:separator]
	modelID := reference[separator+1:]
	provider, exists := document.Providers[providerKey]
	if !exists {
		return false
	}
	providerMap, err := providerRawMap(provider)
	if err != nil {
		return false
	}
	models, err := openCodeProviderModelMap(providerMap)
	if err != nil {
		return false
	}
	_, exists = models[modelID]
	return exists
}

func openCodeMCPInfos(raw json.RawMessage) ([]OpenCodeMCPServerInfo, error) {
	servers, err := providerRawMap(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 OpenCode MCP map 失败: %w", err)
	}
	keys := make([]string, 0, len(servers))
	for key := range servers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]OpenCodeMCPServerInfo, 0, len(keys))
	for _, key := range keys {
		server, err := providerRawMap(servers[key])
		if err != nil {
			return nil, fmt.Errorf("解析 OpenCode MCP %q 失败: %w", key, err)
		}
		serverType := strings.ToLower(strings.TrimSpace(rawString(server["type"])))
		if serverType == "" {
			if len(server["command"]) > 0 {
				serverType = "local"
			} else {
				serverType = "remote"
			}
		}
		command := make([]string, 0)
		if len(server["command"]) > 0 && json.Unmarshal(server["command"], &command) != nil {
			return nil, fmt.Errorf("OpenCode MCP %q 的 command 必须是数组", key)
		}
		environment := make(map[string]string)
		if len(server["environment"]) > 0 {
			if err := json.Unmarshal(server["environment"], &environment); err != nil {
				return nil, fmt.Errorf("OpenCode MCP %q 的 environment 必须是对象", key)
			}
		}
		headers := make(map[string]string)
		if len(server["headers"]) > 0 {
			if err := json.Unmarshal(server["headers"], &headers); err != nil {
				return nil, fmt.Errorf("OpenCode MCP %q 的 headers 必须是对象", key)
			}
		}
		result = append(result, OpenCodeMCPServerInfo{
			Key: key, Type: serverType, Ownership: "unmanaged", URL: maskMCPURL(rawString(server["url"])), Command: maskMCPCommand(command),
			Environment: maskStringMap(environment), Headers: maskStringMap(headers),
		})
	}
	return result, nil
}

func openCodeMCPInfosForState(raw json.RawMessage, state openCodeStateFile, path string) ([]OpenCodeMCPServerInfo, error) {
	result, err := openCodeMCPInfos(raw)
	if err != nil {
		return nil, err
	}
	for index := range result {
		if _, owned := state.MCP[openCodeProviderStorageKey(path, result[index].Key)]; owned {
			result[index].Ownership = "managed"
		}
	}
	return result, nil
}

func openCodeManagedConflicts(document openCodeConfigDocument, state openCodeStateFile, path string) []string {
	conflicts := make([]string, 0)
	prefix := path + "\x00"
	for storageKey, managed := range state.Managed {
		if !strings.HasPrefix(storageKey, prefix) {
			continue
		}
		key := strings.TrimPrefix(storageKey, prefix)
		if sha256Hex(document.Providers[key]) != managed.InjectedHash {
			conflicts = append(conflicts, "provider:"+key)
		}
	}
	mcp, err := providerRawMap(document.Raw["mcp"])
	if err != nil {
		return append(conflicts, "mcp:parse-error")
	}
	for storageKey, managed := range state.MCP {
		if !strings.HasPrefix(storageKey, prefix) {
			continue
		}
		key := strings.TrimPrefix(storageKey, prefix)
		if sha256Hex(mcp[key]) != managed.InjectedHash {
			conflicts = append(conflicts, "mcp:"+key)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func maskStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = maskSecret(value)
	}
	return result
}

func maskMCPCommand(command []string) []string {
	if len(command) == 0 {
		return nil
	}
	result := append([]string(nil), command...)
	for index, value := range result {
		lower := strings.ToLower(value)
		for _, marker := range []string{"api-key=", "apikey=", "token=", "secret=", "password="} {
			if position := strings.Index(lower, marker); position >= 0 {
				result[index] = value[:position+len(marker)] + "****"
				break
			}
		}
	}
	return result
}

func maskMCPURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			query.Set(key, "****")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func compactWarnings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func providerFromOpenCodeRaw(key string, raw json.RawMessage) (Provider, error) {
	if err := validateOpenCodeProviderKey(key); err != nil {
		return Provider{}, err
	}
	providerMap, err := providerRawMap(raw)
	if err != nil {
		return Provider{}, fmt.Errorf("解析 OpenCode Provider %q 失败: %w", key, err)
	}
	options, err := optionsRawMap(providerMap)
	if err != nil {
		return Provider{}, fmt.Errorf("解析 OpenCode Provider %q options 失败: %w", key, err)
	}
	npm := normalizeOpenCodeNPM(rawString(providerMap["npm"]))
	clientProtocol, _ := normalizeOpenCodeClientProtocol(npm, "")
	upstream := UpstreamProtocolAnthropic
	switch clientProtocol {
	case "openai_chat":
		upstream = UpstreamProtocolOpenAIChat
	case "openai_responses":
		upstream = UpstreamProtocolOpenAIResponses
	case "gemini_native":
		upstream = UpstreamProtocolGoogle
	}
	baseURL := rawString(options["baseURL"])
	if baseURL == "" {
		baseURL = rawString(options["baseUrl"])
	}
	apiKey := rawString(options["apiKey"])
	models, _ := openCodeProviderModelMap(providerMap)
	provider := Provider{
		Name: key, APIURL: baseURL, APIKey: apiKey, Enabled: true, Level: 1,
		UpstreamProtocol: string(upstream), SupportedModels: make(map[string]bool),
		openCode: &openCodeProviderPayload{
			ProviderKey: key, NPM: npm, ClientProtocol: clientProtocol,
			Mode: openCodeDefaultMode, GatewayKey: key, RawProvider: cloneRaw(raw),
		},
	}
	for modelID := range models {
		provider.SupportedModels[modelID] = true
	}
	return provider, nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func openCodeSupportedModels(raw map[string]json.RawMessage) map[string]bool {
	models, _ := openCodeProviderModelMap(raw)
	if len(models) == 0 {
		return nil
	}
	result := make(map[string]bool, len(models))
	for key := range models {
		result[key] = true
	}
	return result
}

func openCodeProviderInfo(provider Provider, managed *openCodeManagedState) OpenCodeProviderInfo {
	key := provider.Name
	npm := openCodeDefaultNPM
	client := openCodeDefaultClient
	mode := openCodeDefaultMode
	gateway := key
	raw := json.RawMessage(nil)
	if provider.openCode != nil {
		key = provider.openCode.ProviderKey
		npm = normalizeOpenCodeNPM(provider.openCode.NPM)
		client = provider.openCode.ClientProtocol
		mode = provider.openCode.Mode
		gateway = provider.openCode.GatewayKey
		raw = provider.openCode.RawProvider
	}
	providerMap, _ := providerRawMap(raw)
	name := rawString(providerMap["name"])
	if name == "" {
		name = key
	}
	models, _ := openCodeProviderModelMap(providerMap)
	options, _ := optionsRawMap(providerMap)
	headersConfigured := len(options["headers"]) > 0
	timeout := rawInt64(options["timeout"])
	modelViews := make([]OpenCodeModelInfo, 0, len(models))
	for id, modelRaw := range models {
		modelViews = append(modelViews, openCodeModelInfo(id, modelRaw))
	}
	sort.Slice(modelViews, func(i, j int) bool { return modelViews[i].ID < modelViews[j].ID })
	relayEnabled := managed != nil && managed.Mode == "relay"
	managedValue := managed != nil && managed.TargetPath != ""
	ownership := "unmanaged"
	if managedValue {
		ownership = "managed-" + managed.Mode
	}
	return OpenCodeProviderInfo{
		ID: provider.ID, ProviderKey: key, Name: name, NPM: npm,
		ClientProtocol: client, UpstreamProtocol: string(provider.GetUpstreamProtocol()),
		Mode: mode, GatewayKey: gateway, BaseURL: provider.APIURL,
		APIKeyConfigured: strings.TrimSpace(provider.APIKey) != "",
		APIKeyMasked:     maskSecret(provider.APIKey), Enabled: provider.Enabled,
		HeadersConfigured: headersConfigured, Timeout: timeout,
		Level: provider.Level, Managed: managedValue, RelayEnabled: relayEnabled,
		Models: modelViews, Ownership: ownership,
	}
}

func openCodeModelInfo(id string, raw json.RawMessage) OpenCodeModelInfo {
	model, _ := providerRawMap(raw)
	limit, _ := providerRawMap(model["limit"])
	known := map[string]struct{}{"name": {}, "limit": {}, "modalities": {}, "attachment": {}, "reasoning": {}, "tool_call": {}, "variants": {}, "options": {}}
	extra := 0
	for key := range model {
		if _, exists := known[key]; !exists {
			extra++
		}
	}
	var modalities []string
	if len(model["modalities"]) > 0 {
		_ = json.Unmarshal(model["modalities"], &modalities)
	}
	var variants map[string]any
	if len(model["variants"]) > 0 {
		_ = json.Unmarshal(model["variants"], &variants)
	}
	return OpenCodeModelInfo{
		ID: id, Name: rawString(model["name"]),
		ContextLimit: rawInt64(limit["context"]), InputLimit: rawInt64(limit["input"]),
		OutputLimit: rawInt64(limit["output"]), Reasoning: rawBool(model["reasoning"]),
		ToolCall:        rawBool(model["tool_call"]) || rawBool(model["toolCall"]),
		Attachment:      rawBool(model["attachment"]),
		HasVariants:     len(model["variants"]) > 0,
		ExtraFieldCount: extra,
		Modalities:      modalities,
		Variants:        variants,
	}
}

func openCodeEnvironmentWarnings() []string {
	warnings := make([]string, 0, 4)
	if os.Getenv("OPENCODE_CONFIG") != "" && os.Getenv("OPENCODE_CONFIG_DIR") != "" {
		warnings = append(warnings, "OPENCODE_CONFIG 与 OPENCODE_CONFIG_DIR 同时设置，优先使用 OPENCODE_CONFIG")
	}
	if os.Getenv("OPENAI_API_KEY") != "" && os.Getenv("ANTHROPIC_API_KEY") != "" {
		warnings = append(warnings, "OPENAI_API_KEY 与 ANTHROPIC_API_KEY 同时设置，OpenCode Provider 会优先使用自身 options.apiKey")
	}
	if os.Getenv("GEMINI_API_KEY") != "" && os.Getenv("OPENAI_API_KEY") != "" {
		warnings = append(warnings, "GEMINI_API_KEY 与 OPENAI_API_KEY 同时设置，请确认 @ai-sdk/google Provider 的认证来源")
	}
	if os.Getenv("CODE_SWITCH_RELAY_ADDR") != "" {
		warnings = append(warnings, "CODE_SWITCH_RELAY_ADDR 会覆盖默认 Relay 地址，请确认 OpenCode relay Provider 的 baseURL")
	}
	return warnings
}

func rawInt64(raw json.RawMessage) int64 {
	var value int64
	if len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
		return value
	}
	var number float64
	if len(raw) > 0 && json.Unmarshal(raw, &number) == nil {
		return int64(number)
	}
	return 0
}

func rawBool(raw json.RawMessage) bool {
	var value bool
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

func saveOpenCodeSettings(path string) error {
	settingsPath, err := openCodeSettingsPath()
	if err != nil {
		return err
	}
	return AtomicWriteJSON(settingsPath, struct {
		ConfigPath string `json:"configPath,omitempty"`
	}{ConfigPath: path})
}

func loadOpenCodeSettings() (string, error) {
	path, err := openCodeSettingsPath()
	if err != nil {
		return "", err
	}
	data, readErr := os.ReadFile(path)
	if os.IsNotExist(readErr) {
		return "", nil
	}
	if readErr != nil {
		return "", fmt.Errorf("读取 OpenCode 设置失败: %w", readErr)
	}
	var settings struct {
		ConfigPath string `json:"configPath"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("解析 OpenCode 设置失败: %w", err)
	}
	return strings.TrimSpace(settings.ConfigPath), nil
}

func openCodeSettingsPath() (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "opencode-settings.json"), nil
}

func (s *OpenCodeService) applyProviderLocked(providerKey string) (OpenCodeApplyResult, error) {
	if err := validateOpenCodeProviderKey(providerKey); err != nil {
		return OpenCodeApplyResult{}, err
	}
	if s.providerStore == nil {
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	path, source, format, err := s.resolveTarget()
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	originalBytes, document, currentHash, err := readOpenCodeDocument(path)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	oldState := cloneOpenCodeState(state)
	target := state.Targets[path]
	if target.LastHash != "" && target.LastHash != currentHash {
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode 配置已被外部修改: %s", path)
	}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	var provider *Provider
	for index := range providers {
		if providers[index].openCode != nil && providers[index].openCode.ProviderKey == providerKey {
			copy := providers[index]
			provider = &copy
			break
		}
	}
	if provider == nil {
		return OpenCodeApplyResult{}, fmt.Errorf("未找到 OpenCode Provider %q", providerKey)
	}
	baseRaw, err := providerRawMap(provider.openCode.RawProvider)
	if err != nil {
		return OpenCodeApplyResult{}, fmt.Errorf("解析 OpenCode Provider %q 失败: %w", providerKey, err)
	}
	mode, err := normalizeOpenCodeMode(provider.openCode.Mode)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	if _, knownNPM := openCodeClientProtocolForNPM(provider.openCode.NPM); !knownNPM && mode == "relay" {
		return OpenCodeApplyResult{}, fmt.Errorf("未知 npm 包 %s 只能使用 direct 模式，不能接入 Relay", provider.openCode.NPM)
	}
	nextRaw, err := openCodeProviderWithMode(baseRaw, provider, mode, s.relayAddr)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	nextProviderMap, err := providerRawMap(nextRaw)
	if err != nil {
		return OpenCodeApplyResult{}, fmt.Errorf("解析 OpenCode Provider %q 写入内容失败: %w", providerKey, err)
	}
	models, err := openCodeProviderModelMap(nextProviderMap)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	if references := openCodeModelReferenceErrors(&document, providerKey, models); len(references) > 0 {
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode Provider %q 的模型引用已失效，请先修正: %s", providerKey, strings.Join(references, ", "))
	}
	originalProvider := cloneRaw(document.Providers[providerKey])
	document.Providers[providerKey] = nextRaw
	nextDocument, err := marshalOpenCodeDocument(document)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	warning, err := writeOpenCodeDocument(path, originalBytes, nextDocument)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	_, _, writtenHash, err := readOpenCodeDocument(path)
	if err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode 配置写入后校验失败: %w", err)
	}
	managedKey := openCodeProviderStorageKey(path, providerKey)
	state.Managed[managedKey] = openCodeManagedState{
		TargetPath: path, ProviderKey: providerKey, Mode: mode,
		OriginalProvider: originalProvider, InjectedProvider: cloneRaw(nextRaw),
		OriginalHash: sha256Hex(originalProvider), InjectedHash: sha256Hex(nextRaw), UpdatedAt: openCodeTimeNow(),
	}
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: writtenHash, UpdatedAt: openCodeTimeNow()}
	oldProviders := cloneOpenCodeProviders(providers)
	for index := range providers {
		if providers[index].openCode == nil || providers[index].openCode.ProviderKey != providerKey {
			continue
		}
		providers[index].openCode.Mode = mode
		providers[index].openCode.OriginalProvider = cloneRaw(originalProvider)
		providers[index].openCode.InjectedProvider = cloneRaw(nextRaw)
		providers[index].openCode.BaselineHash = sha256Hex(originalProvider)
		providers[index].openCode.InjectedHash = sha256Hex(nextRaw)
		providers[index].openCode.ConfigPath = path
	}
	if err := s.saveOpenCodeProviders(providers); err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return OpenCodeApplyResult{}, fmt.Errorf("保存 OpenCode Provider 状态失败，配置已回滚: %w", err)
	}
	if err := saveOpenCodeState(state); err != nil {
		_ = s.saveOpenCodeProviders(oldProviders)
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		_ = saveOpenCodeState(oldState)
		return OpenCodeApplyResult{}, fmt.Errorf("保存 OpenCode 托管状态失败，配置和 Provider 已回滚: %w", err)
	}
	_ = source
	return OpenCodeApplyResult{Path: path, ProviderKey: providerKey, Mode: mode, Hash: writtenHash, Warning: warning}, nil
}

func cloneOpenCodePayload(payload *openCodeProviderPayload) *openCodeProviderPayload {
	if payload == nil {
		return nil
	}
	cloned := *payload
	cloned.RawProvider = cloneRaw(payload.RawProvider)
	cloned.OriginalProvider = cloneRaw(payload.OriginalProvider)
	cloned.InjectedProvider = cloneRaw(payload.InjectedProvider)
	return &cloned
}

func cloneOpenCodeProviders(providers []Provider) []Provider {
	cloned := append([]Provider(nil), providers...)
	for index := range cloned {
		cloned[index].openCode = cloneOpenCodePayload(providers[index].openCode)
	}
	return cloned
}

func renameOpenCodeDocumentModelReferences(raw map[string]json.RawMessage, oldKey, newKey string) {
	for _, key := range []string{"model", "small_model", "agent", "plugin"} {
		if value, exists := raw[key]; exists {
			raw[key] = rewriteOpenCodeModelReferenceJSON(value, oldKey, newKey, key == "model" || key == "small_model")
		}
	}
	if value, exists := raw["disabled_providers"]; exists {
		raw["disabled_providers"] = rewriteOpenCodeProviderKeyJSON(value, oldKey, newKey)
	}
}

func rewriteOpenCodeProviderKeyJSON(raw json.RawMessage, oldKey, newKey string) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	var rewrite func(any) any
	rewrite = func(current any) any {
		switch typed := current.(type) {
		case string:
			if typed == oldKey {
				return newKey
			}
		case []any:
			for index := range typed {
				typed[index] = rewrite(typed[index])
			}
		case map[string]any:
			for key, child := range typed {
				typed[key] = rewrite(child)
			}
		}
		return current
	}
	result, err := json.Marshal(rewrite(value))
	if err != nil {
		return raw
	}
	return result
}

func updateOpenCodeModelReferences(document *openCodeConfigDocument, providerKey string, models map[string]json.RawMessage, removeProvider bool) {
	if document == nil {
		return
	}
	for _, key := range []string{"model", "small_model"} {
		value := rawString(document.Raw[key])
		if openCodeModelReferenceInvalid(value, providerKey, models, removeProvider) {
			delete(document.Raw, key)
		}
	}
	for _, key := range []string{"agent", "plugin"} {
		if value, exists := document.Raw[key]; exists {
			document.Raw[key] = removeInvalidOpenCodeModelReferences(value, providerKey, models, removeProvider)
		}
	}
}

func openCodeModelReferenceErrors(document *openCodeConfigDocument, providerKey string, models map[string]json.RawMessage) []string {
	if document == nil {
		return nil
	}
	result := make([]string, 0)
	for _, key := range []string{"model", "small_model"} {
		value := rawString(document.Raw[key])
		if openCodeModelReferenceInvalid(value, providerKey, models, false) {
			result = append(result, key+"="+value)
		}
	}
	for _, key := range []string{"agent", "plugin"} {
		if value, exists := document.Raw[key]; exists {
			result = append(result, collectInvalidOpenCodeModelReferences(value, providerKey, models, key)...)
		}
	}
	sort.Strings(result)
	return result
}

func collectInvalidOpenCodeModelReferences(raw json.RawMessage, providerKey string, models map[string]json.RawMessage, location string) []string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	result := make([]string, 0)
	var visit func(any, string)
	visit = func(current any, currentLocation string) {
		switch typed := current.(type) {
		case []any:
			for index, child := range typed {
				visit(child, fmt.Sprintf("%s[%d]", currentLocation, index))
			}
		case map[string]any:
			for key, child := range typed {
				childLocation := currentLocation + "." + key
				if key == "model" || key == "small_model" {
					if reference, ok := child.(string); ok && openCodeModelReferenceInvalid(reference, providerKey, models, false) {
						result = append(result, childLocation+"="+reference)
						continue
					}
				}
				visit(child, childLocation)
			}
		}
	}
	visit(value, location)
	return result
}

func removeOpenCodeProviderReferences(document *openCodeConfigDocument, providerKey string) {
	updateOpenCodeModelReferences(document, providerKey, nil, true)
	if value, exists := document.Raw["disabled_providers"]; exists {
		document.Raw["disabled_providers"] = removeOpenCodeProviderKeyJSON(value, providerKey)
	}
}

func removeOpenCodeProviderKeyJSON(raw json.RawMessage, providerKey string) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	var remove func(any) any
	remove = func(current any) any {
		switch typed := current.(type) {
		case []any:
			filtered := make([]any, 0, len(typed))
			for _, child := range typed {
				if key, ok := child.(string); ok && key == providerKey {
					continue
				}
				filtered = append(filtered, remove(child))
			}
			return filtered
		case map[string]any:
			for key, child := range typed {
				if key == providerKey {
					delete(typed, key)
					continue
				}
				typed[key] = remove(child)
			}
		}
		return current
	}
	result, err := json.Marshal(remove(value))
	if err != nil {
		return raw
	}
	return result
}

func removeInvalidOpenCodeModelReferences(raw json.RawMessage, providerKey string, models map[string]json.RawMessage, removeProvider bool) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	var prune func(any) any
	prune = func(current any) any {
		switch typed := current.(type) {
		case []any:
			for index := range typed {
				typed[index] = prune(typed[index])
			}
		case map[string]any:
			for key, child := range typed {
				if key == "model" || key == "small_model" {
					if reference, ok := child.(string); ok && openCodeModelReferenceInvalid(reference, providerKey, models, removeProvider) {
						delete(typed, key)
						continue
					}
				}
				typed[key] = prune(child)
			}
		}
		return current
	}
	result, err := json.Marshal(prune(value))
	if err != nil {
		return raw
	}
	return result
}

func openCodeModelReferenceInvalid(reference, providerKey string, models map[string]json.RawMessage, removeProvider bool) bool {
	prefix := providerKey + "/"
	if !strings.HasPrefix(reference, prefix) {
		return false
	}
	if removeProvider {
		return true
	}
	modelID := strings.TrimPrefix(reference, prefix)
	if modelID == "" {
		return true
	}
	_, exists := models[modelID]
	return !exists
}

func rewriteOpenCodeModelReferenceJSON(raw json.RawMessage, oldKey, newKey string, rewriteValue bool) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	var rewrite func(any, bool) any
	rewrite = func(current any, shouldRewriteString bool) any {
		switch typed := current.(type) {
		case string:
			if shouldRewriteString && strings.HasPrefix(typed, oldKey+"/") {
				return newKey + typed[len(oldKey):]
			}
			return typed
		case []any:
			for index := range typed {
				typed[index] = rewrite(typed[index], false)
			}
		case map[string]any:
			for key, child := range typed {
				typed[key] = rewrite(child, key == "model" || key == "small_model")
			}
		}
		return current
	}
	value = rewrite(value, rewriteValue)
	result, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return result
}

func (s *OpenCodeService) restoreProviderLocked(providerKey string) (OpenCodeApplyResult, error) {
	if err := validateOpenCodeProviderKey(providerKey); err != nil {
		return OpenCodeApplyResult{}, err
	}
	if s.providerStore == nil {
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode Provider 存储服务未初始化")
	}
	path, _, format, err := s.resolveTarget()
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	originalBytes, document, currentHash, err := readOpenCodeDocument(path)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	if target := state.Targets[path]; target.LastHash != "" && target.LastHash != currentHash {
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode 配置已被外部修改: %s", path)
	}
	oldState := cloneOpenCodeState(state)
	managedKey := openCodeProviderStorageKey(path, providerKey)
	managed, exists := state.Managed[managedKey]
	if !exists {
		return OpenCodeApplyResult{Path: path, ProviderKey: providerKey, Mode: "direct", Hash: currentHash}, nil
	}
	currentProvider := document.Providers[providerKey]
	if sha256Hex(currentProvider) != managed.InjectedHash {
		return OpenCodeApplyResult{}, fmt.Errorf("OpenCode Provider %q 已被外部修改，拒绝恢复", providerKey)
	}
	if len(managed.OriginalProvider) == 0 {
		delete(document.Providers, providerKey)
		removeOpenCodeProviderReferences(&document, providerKey)
	} else {
		document.Providers[providerKey] = cloneRaw(managed.OriginalProvider)
	}
	nextDocument, err := marshalOpenCodeDocument(document)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	warning, err := writeOpenCodeDocument(path, originalBytes, nextDocument)
	if err != nil {
		return OpenCodeApplyResult{}, err
	}
	_, _, writtenHash, err := readOpenCodeDocument(path)
	if err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return OpenCodeApplyResult{}, err
	}
	delete(state.Managed, managedKey)
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: writtenHash, UpdatedAt: openCodeTimeNow()}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return OpenCodeApplyResult{}, err
	}
	oldProviders := cloneOpenCodeProviders(providers)
	for index := range providers {
		if providers[index].openCode == nil || providers[index].openCode.ProviderKey != providerKey {
			continue
		}
		providers[index].openCode.Mode = "direct"
		providers[index].openCode.OriginalProvider = nil
		providers[index].openCode.InjectedProvider = nil
		providers[index].openCode.BaselineHash = ""
		providers[index].openCode.InjectedHash = ""
	}
	if err := s.saveOpenCodeProviders(providers); err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return OpenCodeApplyResult{}, err
	}
	if err := saveOpenCodeState(state); err != nil {
		_ = s.saveOpenCodeProviders(oldProviders)
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		_ = saveOpenCodeState(oldState)
		return OpenCodeApplyResult{}, err
	}
	return OpenCodeApplyResult{Path: path, ProviderKey: providerKey, Mode: "direct", Hash: writtenHash, Warning: warning}, nil
}

func openCodeProviderWithMode(raw map[string]json.RawMessage, provider *Provider, mode, relayAddr string) (json.RawMessage, error) {
	options, err := optionsRawMap(raw)
	if err != nil {
		return nil, err
	}
	if mode == "relay" {
		if strings.TrimSpace(RelayToken()) == "" {
			return nil, fmt.Errorf("Relay Token 尚未初始化，不能托管 OpenCode Provider")
		}
		setRawString(options, "baseURL", openCodeRelayRootURL(relayAddr, provider.openCode.GatewayKey), false)
		setRawString(options, "apiKey", RelayToken(), false)
	} else {
		setRawString(options, "baseURL", provider.APIURL, true)
		setRawString(options, "apiKey", provider.APIKey, true)
	}
	optionsData, _ := json.Marshal(options)
	raw["options"] = optionsData
	return json.Marshal(raw)
}

func restoreOpenCodeFile(path string, data []byte, existed bool) error {
	if existed {
		return AtomicWriteBytes(path, data)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
