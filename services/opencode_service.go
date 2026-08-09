package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type OpenCodeService struct {
	mu            sync.Mutex
	providerStore *ProviderService
	appSettings   *AppSettingsService
	pricing       *PricingService
	usageState    OpenCodeUsageLoggingState
	stopOnce      sync.Once
	stopCh        chan struct{}
}

func NewOpenCodeService(providerStore *ProviderService, appSettings *AppSettingsService, pricing *PricingService) *OpenCodeService {
	return &OpenCodeService{
		providerStore: providerStore,
		appSettings:   appSettings,
		pricing:       pricing,
		stopCh:        make(chan struct{}),
	}
}

// Start 负责首次同步外部配置，并按用户设置定期读取 OpenCode 已完成会话。
// OpenCode 请求始终直连上游，本服务不会启动或控制任何 OpenCode Relay。
func (s *OpenCodeService) Start() error {
	s.mu.Lock()
	_, syncErr := s.syncLiveProvidersLocked()
	if s.usageLoggingEnabledLocked() {
		_, _ = s.syncUsageLocked()
	}
	s.mu.Unlock()
	go s.runUsageSyncLoop()
	return syncErr
}

func (s *OpenCodeService) Stop() error {
	s.stopOnce.Do(func() { close(s.stopCh) })
	return nil
}

func (s *OpenCodeService) runUsageSyncLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			if s.usageLoggingEnabledLocked() {
				_, _ = s.syncUsageLocked()
			}
			s.mu.Unlock()
		case <-s.stopCh:
			return
		}
	}
}

func (s *OpenCodeService) Snapshot() (OpenCodeConfigSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.syncLiveProvidersLocked(); err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	return s.snapshotLocked()
}

// SetUsageLoggingEnabled 持久化使用记录读取开关。关闭仅停止后续读取，不删除历史记录。
func (s *OpenCodeService) SetUsageLoggingEnabled(enabled bool) (OpenCodeUsageLoggingState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appSettings == nil {
		return OpenCodeUsageLoggingState{}, fmt.Errorf("应用设置服务未初始化")
	}
	settings, err := s.appSettings.GetAppSettings()
	if err != nil {
		return OpenCodeUsageLoggingState{}, err
	}
	settings.OpenCodeUsageLoggingEnabled = enabled
	if _, err := s.appSettings.SaveAppSettings(settings); err != nil {
		return OpenCodeUsageLoggingState{}, err
	}
	if enabled {
		_, _ = s.syncUsageLocked()
	}
	s.usageState.Enabled = enabled
	return s.usageState, nil
}

// SyncUsageNow 供日志页进入和手动刷新时调用；开关关闭时不访问 OpenCode 数据库。
func (s *OpenCodeService) SyncUsageNow() (OpenCodeUsageSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.usageLoggingEnabledLocked() {
		return OpenCodeUsageSyncResult{Enabled: false, Errors: []string{}}, nil
	}
	return s.syncUsageLocked()
}

// ExportProviders 返回当前 OpenCode 页面全部供应商的可导出内容。
// 配置 JSON 按原文保留，便于在另一台设备完整恢复。
func (s *OpenCodeService) ExportProviders() (OpenCodeProviderExportDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.snapshotLocked()
	if err != nil {
		return OpenCodeProviderExportDocument{}, err
	}
	document := OpenCodeProviderExportDocument{
		Version:   openCodeProviderExportVersion,
		Platform:  openCodePlatform,
		Providers: make([]OpenCodeProviderExportEntry, 0, len(snapshot.Providers)),
	}
	for _, provider := range snapshot.Providers {
		document.Providers = append(document.Providers, openCodeProviderExportEntryFromInfo(provider))
	}
	sort.Slice(document.Providers, func(i, j int) bool {
		return document.Providers[i].ProviderKey < document.Providers[j].ProviderKey
	})
	return document, nil
}

// SaveProviderExport 把用户在导出弹窗中看到的内容保存到所选 JSON 文件。
func (s *OpenCodeService) SaveProviderExport(path string, document OpenCodeProviderExportDocument) error {
	target, err := normalizeOpenCodeProviderJSONPath(path)
	if err != nil {
		return err
	}
	normalized, err := normalizeOpenCodeProviderExportDocument(document)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("生成供应商导出文件失败: %w", err)
	}
	if err := atomicWriteFile(target, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("保存供应商导出文件失败: %w", err)
	}
	return nil
}

// ReadProviderImportFile 读取并校验用户选择的供应商 JSON 文件。
// ReadProviderImportText 只读取用户选择的 JSON 文本，不提前解析；前端需要在编辑区展示并标记无效内容。
func (s *OpenCodeService) ReadProviderImportText(path string) (string, error) {
	target, err := normalizeOpenCodeProviderJSONPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("读取供应商导入文件失败: %w", err)
	}
	if info.Size() > 8<<20 {
		return "", fmt.Errorf("供应商导入文件不能超过 8 MB")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("读取供应商导入文件失败: %w", err)
	}
	return strings.TrimPrefix(string(data), "\ufeff"), nil
}

// ImportProviders 按用户逐项选择的结果写入供应商资料。
// 导入不会自动应用到 OpenCode，避免修改用户正在使用的配置。
func (s *OpenCodeService) ImportProviders(request OpenCodeProviderImportRequest) (OpenCodeProviderImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providerStore == nil {
		return OpenCodeProviderImportResult{}, fmt.Errorf("OpenCode 供应商存储服务未初始化")
	}
	document, err := normalizeOpenCodeProviderExportDocument(OpenCodeProviderExportDocument{
		Version: openCodeProviderExportVersion, Platform: openCodePlatform, Providers: request.Providers,
	})
	if err != nil {
		return OpenCodeProviderImportResult{}, err
	}
	decisions, err := openCodeProviderImportDecisions(request.Decisions)
	if err != nil {
		return OpenCodeProviderImportResult{}, err
	}
	existing, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return OpenCodeProviderImportResult{}, err
	}
	existingKeys := make(map[string]struct{}, len(existing))
	for _, provider := range existing {
		if provider.openCode != nil {
			existingKeys[provider.openCode.ProviderKey] = struct{}{}
		}
	}
	usedKeys := make(map[string]struct{}, len(existingKeys))
	for key := range existingKeys {
		usedKeys[key] = struct{}{}
	}

	result := OpenCodeProviderImportResult{}
	for _, entry := range document.Providers {
		action, decided := decisions[entry.ProviderKey]
		_, originalExists := existingKeys[entry.ProviderKey]
		if _, alreadyImported := usedKeys[entry.ProviderKey]; alreadyImported && !originalExists {
			return OpenCodeProviderImportResult{}, fmt.Errorf("导入处理后出现重复的供应商标识: %s", entry.ProviderKey)
		}
		if decided && action == "skip" {
			// 前端也会把只存在于 OpenCode 配置文件中的同名项放进冲突列表。
			// 即使它尚未保存到本项目，也必须尊重用户选择的“保留当前”。
			result.Skipped++
			continue
		}
		if decided && action == "rename" {
			originalKey := entry.ProviderKey
			entry.ProviderKey = nextOpenCodeProviderImportKey(originalKey, usedKeys)
			entry.Name = strings.TrimSpace(entry.Name)
			if entry.Name == "" {
				entry.Name = originalKey
			}
			entry.Name += " (副本)"
			result.Imported++
		} else if originalExists {
			if !decided {
				return OpenCodeProviderImportResult{}, fmt.Errorf("供应商 %q 与现有供应商重复，请选择处理方式", entry.ProviderKey)
			}
			result.Replaced++
		} else {
			result.Imported++
		}
		if _, err := s.saveProviderLocked(entry.toInput()); err != nil {
			if rollbackErr := s.saveOpenCodeProviders(cloneOpenCodeProviders(existing)); rollbackErr != nil {
				return OpenCodeProviderImportResult{}, fmt.Errorf("导入供应商 %q 失败，且无法恢复原有供应商: %v（恢复失败: %w）", entry.ProviderKey, err, rollbackErr)
			}
			return OpenCodeProviderImportResult{}, fmt.Errorf("导入供应商 %q 失败，已恢复原有供应商: %w", entry.ProviderKey, err)
		}
		usedKeys[entry.ProviderKey] = struct{}{}
	}
	return result, nil
}

func (s *OpenCodeService) OpenProviderExportDirectory(path string) error {
	target, err := normalizeOpenCodeProviderJSONPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("导出文件不存在: %w", err)
	}
	if err := OpenInExplorer(filepath.Dir(target)); err != nil {
		return fmt.Errorf("打开导出文件夹失败: %w", err)
	}
	return nil
}

// SetDefaultModel 设置 OpenCode 主模型（顶层 model 字段，格式为 provider/model）。
// 传入空字符串会清空该字段。
func (s *OpenCodeService) SetDefaultModel(model string) (OpenCodeConfigSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.setModelReferenceLocked("model", model); err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	return s.snapshotLocked()
}

// SetSmallModel 设置 OpenCode 小模型（顶层 small_model 字段，格式为 provider/model）。
// 传入空字符串会清空该字段。
func (s *OpenCodeService) SetSmallModel(model string) (OpenCodeConfigSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.setModelReferenceLocked("small_model", model); err != nil {
		return OpenCodeConfigSnapshot{}, err
	}
	return s.snapshotLocked()
}

func (s *OpenCodeService) setModelReferenceLocked(key, reference string) error {
	path, format, original, document, _, state, err := s.readDocumentForWriteLocked()
	if err != nil {
		return err
	}
	reference = strings.TrimSpace(reference)
	if reference == "" {
		delete(document.Raw, key)
	} else {
		if !openCodeModelReferenceExists(document, reference) {
			return fmt.Errorf("OpenCode 模型引用 %q 不存在，请先确认供应商和模型", reference)
		}
		setRawString(document.Raw, key, reference, false)
	}
	if _, _, err := s.persistDocumentLocked(path, format, original, document, state); err != nil {
		return err
	}
	return nil
}

func (s *OpenCodeService) SaveProvider(input OpenCodeProviderInput) (OpenCodeProviderInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenCodeBaselineLocked(); err != nil {
		return OpenCodeProviderInfo{}, err
	}
	provider, err := s.saveProviderLocked(input)
	if err != nil {
		return OpenCodeProviderInfo{}, err
	}
	if input.Applied {
		if err := s.applyProviderLocked(provider.openCode.ProviderKey); err != nil {
			return OpenCodeProviderInfo{}, err
		}
	} else {
		if err := s.restoreProviderLocked(provider.openCode.ProviderKey); err != nil {
			return OpenCodeProviderInfo{}, err
		}
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return OpenCodeProviderInfo{}, err
	}
	path, _, _, err := s.resolveTarget()
	if err != nil {
		return OpenCodeProviderInfo{}, err
	}
	managed, applied := state.Managed[openCodeProviderStorageKey(path, provider.openCode.ProviderKey)]
	if !applied {
		return openCodeProviderInfo(provider, nil), nil
	}
	return openCodeProviderInfo(provider, &managed), nil
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
	if liveExists || managedExists {
		return fmt.Errorf("已应用到 OpenCode 的供应商不能直接修改短名称，请先取消应用")
	}
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
		delete(document.Providers, providerKey)
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
	if existing != nil && strings.TrimSpace(input.ConfigJSON) == "" && strings.TrimSpace(input.APIKey) == "" {
		input.APIKey = existing.APIKey
	}
	baseRaw := make(map[string]json.RawMessage)
	if strings.TrimSpace(input.ConfigJSON) != "" {
		if !strings.HasPrefix(strings.TrimSpace(input.ConfigJSON), "{") {
			return Provider{}, fmt.Errorf("OpenCode Provider %q 配置 JSON 必须是 JSON 对象", key)
		}
		baseRaw, err = providerRawMap(json.RawMessage(input.ConfigJSON))
		if err != nil {
			return Provider{}, fmt.Errorf("OpenCode Provider %q 配置 JSON 必须是 JSON 对象: %w", key, err)
		}
	}
	if existing != nil && existing.openCode != nil {
		if len(baseRaw) == 0 {
			baseRaw, err = providerRawMap(existing.openCode.RawProvider)
			if err != nil {
				return Provider{}, fmt.Errorf("解析 OpenCode Provider %q 原始配置失败: %w", key, err)
			}
		}
	}
	configOptions, err := optionsRawMap(baseRaw)
	if err != nil {
		return Provider{}, fmt.Errorf("解析 OpenCode Provider %q options 失败: %w", key, err)
	}
	if strings.TrimSpace(input.Name) == "" {
		input.Name = rawString(baseRaw["name"])
	}
	if strings.TrimSpace(input.BaseURL) == "" {
		input.BaseURL = rawString(configOptions["baseURL"])
	}
	if strings.TrimSpace(input.APIKey) == "" {
		input.APIKey = rawString(configOptions["apiKey"])
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
		Enabled:          true,
		Level:            1,
		UpstreamProtocol: string(upstreamProtocol),
		SupportedModels:  openCodeSupportedModels(baseRaw),
		openCode: &openCodeProviderPayload{
			ProviderKey: key, NPM: npm, ClientProtocol: clientProtocol,
			RawProvider: rawData,
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

func openCodeProviderExportEntryFromInfo(provider OpenCodeProviderInfo) OpenCodeProviderExportEntry {
	return OpenCodeProviderExportEntry{
		ProviderKey: provider.ProviderKey, Name: provider.Name, NPM: provider.NPM,
		ClientProtocol: provider.ClientProtocol, UpstreamProtocol: provider.UpstreamProtocol,
		BaseURL: provider.BaseURL, ConfigJSON: provider.ConfigJSON,
	}
}

func (entry OpenCodeProviderExportEntry) toInput() OpenCodeProviderInput {
	return OpenCodeProviderInput{
		ProviderKey: entry.ProviderKey, Name: entry.Name, NPM: entry.NPM,
		ClientProtocol: entry.ClientProtocol, UpstreamProtocol: entry.UpstreamProtocol,
		BaseURL: entry.BaseURL, Applied: false, ConfigJSON: entry.ConfigJSON,
		// nil 表示直接保留 ConfigJSON 内完整模型设置。
		Models: nil,
	}
}

func normalizeOpenCodeProviderExportDocument(document OpenCodeProviderExportDocument) (OpenCodeProviderExportDocument, error) {
	if document.Version != openCodeProviderExportVersion {
		return OpenCodeProviderExportDocument{}, fmt.Errorf("不支持的供应商导入文件版本: %d", document.Version)
	}
	if strings.TrimSpace(document.Platform) != openCodePlatform {
		return OpenCodeProviderExportDocument{}, fmt.Errorf("该文件不是 OpenCode 供应商导出文件")
	}
	if document.Providers == nil {
		document.Providers = []OpenCodeProviderExportEntry{}
	}
	seen := make(map[string]struct{}, len(document.Providers))
	for index := range document.Providers {
		entry := &document.Providers[index]
		entry.ProviderKey = strings.TrimSpace(entry.ProviderKey)
		if err := validateOpenCodeProviderKey(entry.ProviderKey); err != nil {
			return OpenCodeProviderExportDocument{}, fmt.Errorf("第 %d 个供应商无效: %w", index+1, err)
		}
		if _, exists := seen[entry.ProviderKey]; exists {
			return OpenCodeProviderExportDocument{}, fmt.Errorf("导入文件中有重复的供应商标识: %s", entry.ProviderKey)
		}
		seen[entry.ProviderKey] = struct{}{}
		formatted, err := formatOpenCodeConfigJSON(json.RawMessage(entry.ConfigJSON))
		if err != nil {
			return OpenCodeProviderExportDocument{}, fmt.Errorf("供应商 %q 的配置 JSON 无效: %w", entry.ProviderKey, err)
		}
		entry.ConfigJSON = formatted
	}
	return document, nil
}

func openCodeProviderImportDecisions(items []OpenCodeProviderImportDecision) (map[string]string, error) {
	decisions := make(map[string]string, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.ProviderKey)
		if err := validateOpenCodeProviderKey(key); err != nil {
			return nil, fmt.Errorf("重复供应商处理项无效: %w", err)
		}
		if _, exists := decisions[key]; exists {
			return nil, fmt.Errorf("供应商 %q 的处理方式重复", key)
		}
		action := strings.ToLower(strings.TrimSpace(item.Action))
		if action != "skip" && action != "replace" && action != "rename" {
			return nil, fmt.Errorf("供应商 %q 的处理方式无效", key)
		}
		decisions[key] = action
	}
	return decisions, nil
}

func nextOpenCodeProviderImportKey(base string, used map[string]struct{}) string {
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s-%d", base, index)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func normalizeOpenCodeProviderJSONPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("未选择供应商 JSON 文件")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析供应商 JSON 文件路径失败: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if !strings.EqualFold(filepath.Ext(absolute), ".json") {
		return "", fmt.Errorf("供应商导入导出只支持 .json 文件")
	}
	return absolute, nil
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
		Warnings:     compactWarnings(warning),
		UsageLogging: s.currentUsageLoggingStateLocked(),
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
			RawProvider: cloneRaw(raw),
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
	raw := json.RawMessage(nil)
	if provider.openCode != nil {
		key = provider.openCode.ProviderKey
		npm = normalizeOpenCodeNPM(provider.openCode.NPM)
		client = provider.openCode.ClientProtocol
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
	managedValue := managed != nil && managed.TargetPath != ""
	ownership := "local"
	if managedValue {
		ownership = "applied"
	}
	configJSON, _ := formatOpenCodeConfigJSON(raw)
	return OpenCodeProviderInfo{
		ID: provider.ID, ProviderKey: key, Name: name, NPM: npm,
		ClientProtocol: client, UpstreamProtocol: string(provider.GetUpstreamProtocol()),
		BaseURL:           provider.APIURL,
		APIKeyConfigured:  strings.TrimSpace(provider.APIKey) != "",
		APIKeyMasked:      maskSecret(provider.APIKey),
		HeadersConfigured: headersConfigured, Timeout: timeout,
		Applied: managedValue,
		Models:  modelViews, Ownership: ownership,
		ConfigJSON: configJSON,
	}
}

func openCodeModelInfo(id string, raw json.RawMessage) OpenCodeModelInfo {
	model, _ := providerRawMap(raw)
	limit, _ := providerRawMap(model["limit"])
	known := map[string]struct{}{"name": {}, "limit": {}, "modalities": {}, "attachment": {}, "reasoning": {}, "tool_call": {}, "temperature": {}, "variants": {}, "options": {}}
	extra := 0
	for key := range model {
		if _, exists := known[key]; !exists {
			extra++
		}
	}
	modalities := parseOpenCodeModelModalities(model["modalities"])
	var variants map[string]any
	if len(model["variants"]) > 0 {
		_ = json.Unmarshal(model["variants"], &variants)
	}
	optionsJSON := ""
	if len(model["options"]) > 0 {
		optionsJSON = string(model["options"])
	}
	return OpenCodeModelInfo{
		ID: id, Name: rawString(model["name"]),
		ContextLimit: rawInt64(limit["context"]), InputLimit: rawInt64(limit["input"]),
		OutputLimit: rawInt64(limit["output"]), Reasoning: rawBool(model["reasoning"]),
		ToolCall: rawBool(model["tool_call"]) || rawBool(model["toolCall"]), Temperature: rawBool(model["temperature"]),
		Attachment:      rawBool(model["attachment"]),
		HasVariants:     len(model["variants"]) > 0,
		ExtraFieldCount: extra,
		Modalities:      modalities,
		Variants:        variants,
		OptionsJSON:     optionsJSON,
	}
}

func parseOpenCodeModelModalities(raw json.RawMessage) OpenCodeModelModalities {
	empty := OpenCodeModelModalities{Input: []string{}, Output: []string{}}
	if len(raw) == 0 {
		return empty
	}
	var modalities OpenCodeModelModalities
	if json.Unmarshal(raw, &modalities) == nil && (modalities.Input != nil || modalities.Output != nil) {
		if modalities.Input == nil {
			modalities.Input = []string{}
		}
		if modalities.Output == nil {
			modalities.Output = []string{}
		}
		return modalities
	}
	var legacy []string
	if json.Unmarshal(raw, &legacy) == nil {
		if legacy == nil {
			legacy = []string{}
		}
		return OpenCodeModelModalities{Input: legacy, Output: []string{}}
	}
	return empty
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

func (s *OpenCodeService) applyProviderLocked(providerKey string) error {
	if err := validateOpenCodeProviderKey(providerKey); err != nil {
		return err
	}
	if s.providerStore == nil {
		return fmt.Errorf("OpenCode Provider 存储服务未初始化")
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
	target := state.Targets[path]
	if target.LastHash != "" && target.LastHash != currentHash {
		return fmt.Errorf("OpenCode 配置已被外部修改: %s", path)
	}
	providers, err := s.providerStore.LoadProviders(openCodePlatform)
	if err != nil {
		return err
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
		return fmt.Errorf("未找到 OpenCode Provider %q", providerKey)
	}
	baseRaw, err := providerRawMap(provider.openCode.RawProvider)
	if err != nil {
		return fmt.Errorf("解析 OpenCode Provider %q 失败: %w", providerKey, err)
	}
	nextRaw, err := json.Marshal(baseRaw)
	if err != nil {
		return fmt.Errorf("序列化 OpenCode Provider %q 失败: %w", providerKey, err)
	}
	document.Providers[providerKey] = nextRaw
	nextDocument, err := marshalOpenCodeDocument(document)
	if err != nil {
		return err
	}
	_, err = writeOpenCodeDocument(path, originalBytes, nextDocument)
	if err != nil {
		return err
	}
	_, _, writtenHash, err := readOpenCodeDocument(path)
	if err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return fmt.Errorf("OpenCode 配置写入后校验失败: %w", err)
	}
	managedKey := openCodeProviderStorageKey(path, providerKey)
	state.Managed[managedKey] = openCodeManagedState{
		TargetPath: path, ProviderKey: providerKey, InjectedHash: sha256Hex(nextRaw), UpdatedAt: openCodeTimeNow(),
	}
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: writtenHash, UpdatedAt: openCodeTimeNow()}
	if err := saveOpenCodeState(state); err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		_ = saveOpenCodeState(oldState)
		return fmt.Errorf("保存 OpenCode 托管状态失败，配置和 Provider 已回滚: %w", err)
	}
	return nil
}

func cloneOpenCodePayload(payload *openCodeProviderPayload) *openCodeProviderPayload {
	if payload == nil {
		return nil
	}
	cloned := *payload
	cloned.RawProvider = cloneRaw(payload.RawProvider)
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

func (s *OpenCodeService) restoreProviderLocked(providerKey string) error {
	if err := validateOpenCodeProviderKey(providerKey); err != nil {
		return err
	}
	if s.providerStore == nil {
		return fmt.Errorf("OpenCode Provider 存储服务未初始化")
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
	oldState := cloneOpenCodeState(state)
	managedKey := openCodeProviderStorageKey(path, providerKey)
	managed, exists := state.Managed[managedKey]
	if !exists {
		return nil
	}
	currentProvider := document.Providers[providerKey]
	if sha256Hex(currentProvider) != managed.InjectedHash {
		return fmt.Errorf("OpenCode Provider %q 已被外部修改，拒绝移除", providerKey)
	}
	delete(document.Providers, providerKey)
	nextDocument, err := marshalOpenCodeDocument(document)
	if err != nil {
		return err
	}
	if _, err := writeOpenCodeDocument(path, originalBytes, nextDocument); err != nil {
		return err
	}
	_, _, writtenHash, err := readOpenCodeDocument(path)
	if err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		return err
	}
	delete(state.Managed, managedKey)
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: writtenHash, UpdatedAt: openCodeTimeNow()}
	if err := saveOpenCodeState(state); err != nil {
		_ = restoreOpenCodeFile(path, originalBytes, originalBytes != nil)
		_ = saveOpenCodeState(oldState)
		return err
	}
	return nil
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
