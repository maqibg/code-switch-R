package services

import (
	"encoding/json"
	"fmt"
	"strings"
)

type piRenameFileBackup struct {
	path    string
	data    []byte
	existed bool
}

type piRenameJSONWrite struct {
	path  string
	value any
}

// RenameModelsProvider changes the models.json Provider key and every
// code-switch-R/Pi reference that uses that key as platform identity.
func (s *PiSettingsService) RenameModelsProvider(originalID string, input PiModelsProviderTemplate) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()

	originalID = strings.TrimSpace(originalID)
	if originalID == "" || len(originalID) > 64 || !PiModelsProviderIDPattern.MatchString(originalID) {
		return fmt.Errorf("原 Provider key 格式无效")
	}
	var err error
	input, err = normalizePiModelsProviderTemplate(input)
	if err != nil {
		return err
	}
	if input.ID == originalID {
		return fmt.Errorf("新旧 Provider key 相同，无需重命名")
	}

	root, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	if err := requirePiModelsFingerprint(input.Fingerprint, fingerprint); err != nil {
		return err
	}
	current, exists := providers[originalID]
	if !exists {
		return fmt.Errorf("Pi models.json Provider 不存在: %s", originalID)
	}
	if _, exists := providers[input.ID]; exists {
		return fmt.Errorf("Pi models.json Provider 已存在: %s", input.ID)
	}

	state, err := s.loadPlatformState()
	if err != nil {
		return err
	}
	managed, isManaged := state.Platforms[originalID]
	if _, collision := state.Platforms[input.ID]; collision {
		return fmt.Errorf("Pi 托管状态已占用 Provider key: %s", input.ID)
	}

	authRoot, _, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi auth.json 失败: %w", err)
	}
	if _, collision := authRoot[input.ID]; collision {
		return fmt.Errorf("Pi auth.json 已存在 Provider key: %s", input.ID)
	}
	if isManaged {
		if !piManagedProviderConnectionMatches(current, s.platformBaseURL(originalID)) {
			return fmt.Errorf("Pi 平台 %q 托管配置已被外部修改，请先处理冲突", originalID)
		}
		if err := validateManagedPiProviderTemplate(input.ID, input); err != nil {
			return err
		}
		if managed.InjectedAuthHash != "" && !piManagedAuthMatches(authRoot, originalID, managed) {
			return fmt.Errorf("Pi 平台 %q 的 auth.json 认证已被外部修改，请先处理冲突", originalID)
		}
		input.BaseURL = s.platformBaseURL(input.ID)
		input.APIKey = relayTokenForConfig()
	}

	renamedRaw, err := buildPiModelsProviderRaw(input, current)
	if err != nil {
		return fmt.Errorf("构建重命名后的 Pi 平台失败: %w", err)
	}
	delete(providers, originalID)
	providers[input.ID] = renamedRaw
	providersRaw, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	root["providers"] = providersRaw

	authChanged := false
	if originalAuth, exists := authRoot[originalID]; exists {
		delete(authRoot, originalID)
		authRoot[input.ID] = cloneRawMessage(originalAuth)
		authChanged = true
	}
	stateChanged := false
	if isManaged {
		delete(state.Platforms, originalID)
		managed.InjectedProviderHash = canonicalJSONHash(renamedRaw)
		state.Platforms[input.ID] = managed
		stateChanged = true
	}

	settingsRoot, _, err := readJSONObjectOrDefault(s.settingsPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi settings.json 失败: %w", err)
	}
	settingsChanged, err := renamePiSettingsProviderReferences(settingsRoot, originalID, input.ID)
	if err != nil {
		return err
	}

	uiState, err := s.loadUIState()
	if err != nil {
		return err
	}
	uiChanged := renamePiPlatformOrderReference(&uiState, originalID, input.ID)

	suppliers, suppliersChanged, err := s.renamePiSupplierPlatformReferences(originalID, input.ID)
	if err != nil {
		return err
	}

	writes := []piRenameJSONWrite{
		{path: s.modelsPath(), value: root},
	}
	if authChanged {
		writes = append(writes, piRenameJSONWrite{path: s.authPath(), value: authRoot})
	}
	if stateChanged {
		statePath, statePathErr := s.platformStateFile()
		if statePathErr != nil {
			return statePathErr
		}
		writes = append(writes, piRenameJSONWrite{path: statePath, value: state})
	}
	if settingsChanged {
		writes = append(writes, piRenameJSONWrite{path: s.settingsPath(), value: settingsRoot})
	}
	if uiChanged {
		writes = append(writes, piRenameJSONWrite{path: s.uiStateFile(), value: uiState})
	}

	backups := make([]piRenameFileBackup, 0, len(writes))
	for _, write := range writes {
		backup, existed, readErr := readOptionalFile(write.path)
		if readErr != nil {
			return fmt.Errorf("备份 Pi 平台重命名文件失败 %s: %w", write.path, readErr)
		}
		backups = append(backups, piRenameFileBackup{path: write.path, data: backup, existed: existed})
	}
	rollback := func(primary error) error {
		var rollbackErrors []string
		for index := len(backups) - 1; index >= 0; index-- {
			backup := backups[index]
			if rollbackErr := restoreOptionalFile(backup.path, backup.data, backup.existed); rollbackErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: %v", backup.path, rollbackErr))
			}
		}
		if len(rollbackErrors) > 0 {
			return fmt.Errorf("%w; 回滚失败: %s", primary, strings.Join(rollbackErrors, "; "))
		}
		return primary
	}
	for _, write := range writes {
		if err := AtomicWriteJSON(write.path, write.value); err != nil {
			return rollback(fmt.Errorf("重命名 Pi 平台写入 %s 失败: %w", write.path, err))
		}
	}
	if suppliersChanged {
		if err := s.providerService.SaveProviders("pi", suppliers); err != nil {
			return rollback(fmt.Errorf("迁移 Pi 平台供应商归属失败: %w", err))
		}
	}
	return nil
}

func validateManagedPiProviderTemplate(platformID string, input PiModelsProviderTemplate) error {
	if _, supported := piManagedAPIs[input.API]; !supported {
		return fmt.Errorf("Pi 平台 %q 的 api=%q 暂不支持托管", platformID, input.API)
	}
	for _, model := range input.Models {
		if strings.TrimSpace(model.BaseURL) != "" {
			return fmt.Errorf("Pi 平台 %q 的模型 %q 配置了独立 baseUrl，会绕过平台网关", platformID, model.ID)
		}
		if api := strings.TrimSpace(model.API); api != "" {
			if _, supported := piManagedAPIs[api]; !supported {
				return fmt.Errorf("Pi 平台 %q 的模型 %q 使用了不受网关支持的 api=%q", platformID, model.ID, api)
			}
		}
	}
	return nil
}

func renamePiSettingsProviderReferences(root map[string]json.RawMessage, originalID, newID string) (bool, error) {
	changed := false
	if raw, exists := root["defaultProvider"]; exists {
		var providerID string
		if err := json.Unmarshal(raw, &providerID); err != nil {
			return false, fmt.Errorf("Pi settings.json 的 defaultProvider 必须是字符串")
		}
		if providerID == originalID {
			root["defaultProvider"], _ = json.Marshal(newID)
			changed = true
		}
	}
	if raw, exists := root["enabledModels"]; exists {
		var enabledModels []string
		if err := json.Unmarshal(raw, &enabledModels); err != nil {
			return false, fmt.Errorf("Pi settings.json 的 enabledModels 必须是字符串数组")
		}
		enabledModelsChanged := false
		for index, model := range enabledModels {
			if strings.HasPrefix(model, originalID+"/") {
				enabledModels[index] = newID + strings.TrimPrefix(model, originalID)
				enabledModelsChanged = true
			}
		}
		if enabledModelsChanged {
			root["enabledModels"], _ = json.Marshal(enabledModels)
			changed = true
		}
	}
	return changed, nil
}

func renamePiPlatformOrderReference(state *PiUIState, originalID, newID string) bool {
	changed := false
	for index, providerID := range state.PlatformOrder {
		if providerID == originalID {
			state.PlatformOrder[index] = newID
			changed = true
		}
	}
	if changed {
		state.PlatformOrder = normalizePlatformOrder(state.PlatformOrder, nil)
	}
	return changed
}

func (s *PiSettingsService) renamePiSupplierPlatformReferences(originalID, newID string) ([]Provider, bool, error) {
	if s.providerService == nil {
		return nil, false, nil
	}
	providers, err := s.providerService.LoadProviders("pi")
	if err != nil {
		return nil, false, fmt.Errorf("读取 Pi 上游供应商失败: %w", err)
	}
	for _, provider := range providers {
		if provider.PiPlatformKey() == newID {
			return nil, false, fmt.Errorf("Pi 上游供应商已占用目标平台: %s", newID)
		}
	}
	changed := false
	for index := range providers {
		if providers[index].PiPlatformKey() != originalID {
			continue
		}
		providers[index].PiPlatform = newID
		providers[index].PiTemplate = ""
		changed = true
	}
	return providers, changed, nil
}
