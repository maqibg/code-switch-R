package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	PiConflictKeepExternalStop    = "keep_external_stop"
	PiConflictRestoreOriginalStop = "restore_original_stop"
	PiConflictRebaselineManaged   = "rebaseline_managed"
)

type PiPlatformChangePlan struct {
	ProviderID  string   `json:"providerId"`
	CurrentMode string   `json:"currentMode"`
	TargetMode  string   `json:"targetMode"`
	Revision    string   `json:"revision"`
	Changes     []string `json:"changes"`
	Blockers    []string `json:"blockers"`
}

type PiPlatformConflictDetail struct {
	ProviderID      string `json:"providerId"`
	Revision        string `json:"revision"`
	Tracked         bool   `json:"tracked"`
	ProviderExists  bool   `json:"providerExists"`
	ProviderChanged bool   `json:"providerChanged"`
	AuthExists      bool   `json:"authExists"`
	AuthChanged     bool   `json:"authChanged"`
	CanKeepExternal bool   `json:"canKeepExternal"`
	CanRestore      bool   `json:"canRestore"`
	CanRebaseline   bool   `json:"canRebaseline"`
}

func (s *PiSettingsService) PreviewPlatformModeChange(providerID, targetMode string) (PiPlatformChangePlan, error) {
	providerID = strings.TrimSpace(providerID)
	targetMode = strings.ToLower(strings.TrimSpace(targetMode))
	plan := PiPlatformChangePlan{
		ProviderID: providerID, TargetMode: targetMode, Revision: s.currentRuntimeRevision(),
		Changes: []string{}, Blockers: []string{}, CurrentMode: "direct",
	}
	if providerID == "" || !PiModelsProviderIDPattern.MatchString(providerID) {
		return plan, fmt.Errorf("Pi 平台 ID 格式无效: %s", providerID)
	}
	if targetMode != "managed" && targetMode != "direct" {
		return plan, fmt.Errorf("Pi 平台目标模式只允许 managed 或 direct")
	}
	detail, err := s.GetPlatformConflict(providerID)
	if err != nil {
		return plan, err
	}
	if detail.Tracked && !detail.ProviderChanged && !detail.AuthChanged {
		plan.CurrentMode = "managed"
	}
	if detail.Tracked && (detail.ProviderChanged || detail.AuthChanged) {
		plan.CurrentMode = "conflict"
		plan.Blockers = append(plan.Blockers, "平台存在外部修改冲突，请先选择冲突解决方式")
		return plan, nil
	}
	if plan.CurrentMode == targetMode {
		return plan, nil
	}
	if targetMode == "direct" {
		plan.Changes = append(plan.Changes, "恢复平台原始 baseUrl、apiKey 和 auth.json 认证", "停止通过 code-switch-R Pi 网关转发")
		return plan, nil
	}
	_, providers, _, readErr := readPiModelsProviderDocument(s.modelsPath())
	if readErr != nil {
		return plan, readErr
	}
	raw, exists := providers[providerID]
	if !exists {
		plan.Blockers = append(plan.Blockers, "models.json 中不存在该平台")
		return plan, nil
	}
	var source piModelsProviderFile
	if err := json.Unmarshal(raw, &source); err != nil {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("平台配置无法解析: %v", err))
		return plan, nil
	}
	if _, supported := piManagedAPIs[strings.TrimSpace(source.API)]; !supported {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("api=%q 暂不支持托管", source.API))
	}
	for _, model := range source.Models {
		if strings.TrimSpace(model.BaseURL) != "" {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("模型 %q 配置了会绕过平台网关的 baseUrl", model.ID))
		}
		if api := strings.TrimSpace(model.API); api != "" {
			if _, supported := piManagedAPIs[api]; !supported {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("模型 %q 使用了不受网关支持的 api=%q", model.ID, api))
			}
		}
	}
	if strings.TrimSpace(source.BaseURL) == "" {
		plan.Blockers = append(plan.Blockers, "平台没有可导入为上游供应商的 baseUrl")
	}
	plan.Changes = append(plan.Changes,
		"备份该平台在 models.json 和 auth.json 中的原始配置",
		"将平台 baseUrl 指向 code-switch-R 平台网关",
		"没有上游供应商时导入当前平台连接配置",
	)
	return plan, nil
}

func (s *PiSettingsService) ApplyPlatformModeChange(plan PiPlatformChangePlan) error {
	if err := s.requireRuntimeRevision(plan.Revision); err != nil {
		return err
	}
	current, err := s.PreviewPlatformModeChange(plan.ProviderID, plan.TargetMode)
	if err != nil {
		return err
	}
	if current.Revision != plan.Revision {
		return fmt.Errorf("Pi Runtime 配置已变化，请重新预检")
	}
	if len(current.Blockers) > 0 {
		return fmt.Errorf("无法切换 Pi 平台模式: %s", strings.Join(current.Blockers, "; "))
	}
	if current.CurrentMode == current.TargetMode {
		return nil
	}
	if current.TargetMode == "managed" {
		return s.EnablePlatformProxy(current.ProviderID)
	}
	return s.DisablePlatformProxy(current.ProviderID)
}

func (s *PiSettingsService) GetPlatformConflict(providerID string) (PiPlatformConflictDetail, error) {
	providerID = strings.TrimSpace(providerID)
	detail := PiPlatformConflictDetail{ProviderID: providerID, Revision: s.currentRuntimeRevision()}
	if providerID == "" {
		return detail, fmt.Errorf("Pi 平台 ID 不能为空")
	}
	state, err := s.loadPlatformState()
	if err != nil {
		return detail, err
	}
	entry, tracked := state.Platforms[providerID]
	detail.Tracked = tracked
	if !tracked {
		return detail, nil
	}
	_, providers, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return detail, err
	}
	current, exists := providers[providerID]
	detail.ProviderExists = exists
	detail.ProviderChanged = !exists || !piManagedProviderConnectionMatches(current, s.platformBaseURL(providerID))
	authRoot, _, authErr := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if authErr != nil {
		return detail, fmt.Errorf("读取 Pi auth.json 失败: %w", authErr)
	}
	_, detail.AuthExists = authRoot[providerID]
	if entry.InjectedAuthHash != "" {
		detail.AuthChanged = !piManagedAuthMatches(authRoot, providerID, entry)
	}
	detail.CanKeepExternal = detail.ProviderExists
	detail.CanRestore = len(entry.OriginalProvider) > 0
	detail.CanRebaseline = detail.ProviderExists && piManagedProviderRoutingCompatible(current)
	return detail, nil
}

func (s *PiSettingsService) ResolvePlatformConflict(providerID, action, expectedRevision string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.requireRuntimeRevision(expectedRevision); err != nil {
		return err
	}
	providerID = strings.TrimSpace(providerID)
	action = strings.TrimSpace(action)
	state, err := s.loadPlatformState()
	if err != nil {
		return err
	}
	entry, tracked := state.Platforms[providerID]
	if !tracked {
		return fmt.Errorf("Pi 平台 %q 没有待处理的托管状态", providerID)
	}
	root, platforms, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	authRoot, authFileExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return fmt.Errorf("读取 Pi auth.json 失败: %w", err)
	}
	modelsBackup, modelsExisted, err := readOptionalFile(s.modelsPath())
	if err != nil {
		return err
	}
	authBackup, _, err := readOptionalFile(s.authPath())
	if err != nil {
		return err
	}
	statePath, err := s.platformStateFile()
	if err != nil {
		return err
	}
	stateBackup, stateExisted, err := readOptionalFile(statePath)
	if err != nil {
		return err
	}

	switch action {
	case PiConflictKeepExternalStop:
		current, exists := platforms[providerID]
		if !exists {
			return fmt.Errorf("外部配置已删除平台 %q，无法保留为直连平台", providerID)
		}
		platforms[providerID] = rebaselinePiProvider(current, entry.OriginalProvider, s.platformBaseURL(providerID))
		if entry.InjectedAuthHash != "" {
			currentAuth, currentAuthExists := authRoot[providerID]
			if currentAuthExists && canonicalJSONHash(currentAuth) == entry.InjectedAuthHash {
				if entry.OriginalAuthExisted {
					authRoot[providerID] = cloneRawMessage(entry.OriginalAuth)
				} else {
					delete(authRoot, providerID)
				}
			}
		}
		delete(state.Platforms, providerID)
	case PiConflictRestoreOriginalStop:
		if len(entry.OriginalProvider) == 0 {
			return fmt.Errorf("Pi 平台 %q 缺少可恢复的原始配置", providerID)
		}
		current, exists := platforms[providerID]
		if !exists {
			return fmt.Errorf("外部配置已删除平台 %q，无法恢复原始连接", providerID)
		}
		restored, restoreErr := restorePiProviderConnection(current, entry.OriginalProvider)
		if restoreErr != nil {
			return fmt.Errorf("恢复 Pi 平台 %q 连接配置失败: %w", providerID, restoreErr)
		}
		platforms[providerID] = restored
		if entry.InjectedAuthHash != "" {
			if entry.OriginalAuthExisted {
				authRoot[providerID] = cloneRawMessage(entry.OriginalAuth)
			} else {
				delete(authRoot, providerID)
			}
		}
		delete(state.Platforms, providerID)
	case PiConflictRebaselineManaged:
		current, exists := platforms[providerID]
		if !exists {
			return fmt.Errorf("外部配置已删除平台 %q，无法重新建立托管基线", providerID)
		}
		if !piManagedProviderRoutingCompatible(current) {
			return fmt.Errorf("Pi 平台 %q 当前包含不受网关支持的 API 或独立模型 baseUrl，无法重新建立托管基线", providerID)
		}
		baselineRaw := rebaselinePiProvider(current, entry.OriginalProvider, s.platformBaseURL(providerID))
		managedRaw, buildErr := buildManagedPiPlatformRaw(baselineRaw, s.platformBaseURL(providerID))
		if buildErr != nil {
			return buildErr
		}
		currentAuth, currentAuthExists := authRoot[providerID]
		baselineAuth, baselineAuthExists := currentAuth, currentAuthExists
		if currentAuthExists && canonicalJSONHash(currentAuth) == entry.InjectedAuthHash {
			baselineAuth, baselineAuthExists = entry.OriginalAuth, entry.OriginalAuthExisted
		}
		managedAuthRaw, marshalErr := json.Marshal(PiAuthEntry{Type: "api_key", Key: relayTokenForConfig()})
		if marshalErr != nil {
			return marshalErr
		}
		connectionSnapshot, snapshotErr := piProviderConnectionSnapshot(baselineRaw)
		if snapshotErr != nil {
			return snapshotErr
		}
		entry = PiPlatformProxyState{
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), OriginalProvider: connectionSnapshot,
			InjectedProviderHash: canonicalJSONHash(managedRaw), OriginalAuthExisted: baselineAuthExists,
			OriginalAuth: cloneRawMessage(baselineAuth), InjectedAuthHash: canonicalJSONHash(managedAuthRaw),
		}
		platforms[providerID] = managedRaw
		authRoot[providerID] = managedAuthRaw
		state.Platforms[providerID] = entry
	default:
		return fmt.Errorf("不支持的 Pi 冲突解决动作: %s", action)
	}

	providersRaw, err := json.Marshal(platforms)
	if err != nil {
		return err
	}
	root["providers"] = providersRaw
	desiredAuthExisted := state.AuthFileExisted || len(authRoot) > 0
	if err := writePiConfigPair(s.modelsPath(), root, modelsExisted, s.authPath(), authRoot, desiredAuthExisted, action == PiConflictRebaselineManaged); err != nil {
		return err
	}
	if err := s.savePlatformState(state); err != nil {
		modelsRollbackErr := restoreOptionalFile(s.modelsPath(), modelsBackup, modelsExisted)
		authRollbackErr := restoreOptionalFile(s.authPath(), authBackup, authFileExisted)
		stateRollbackErr := restoreOptionalFile(statePath, stateBackup, stateExisted)
		if modelsRollbackErr != nil || authRollbackErr != nil || stateRollbackErr != nil {
			return fmt.Errorf("保存 Pi 冲突解决状态失败: %w; models.json 回滚: %v; auth.json 回滚: %v; 状态回滚: %v", err, modelsRollbackErr, authRollbackErr, stateRollbackErr)
		}
		return err
	}
	return nil
}

func rebaselinePiProvider(current, previousOriginal json.RawMessage, managedBaseURL string) json.RawMessage {
	var currentFields, originalFields map[string]json.RawMessage
	if json.Unmarshal(current, &currentFields) != nil || json.Unmarshal(previousOriginal, &originalFields) != nil {
		return cloneRawMessage(current)
	}
	var currentBaseURL, currentAPIKey string
	_ = json.Unmarshal(currentFields["baseUrl"], &currentBaseURL)
	_ = json.Unmarshal(currentFields["apiKey"], &currentAPIKey)
	if sameURL(currentBaseURL, managedBaseURL) {
		if value, exists := originalFields["baseUrl"]; exists {
			currentFields["baseUrl"] = value
		} else {
			delete(currentFields, "baseUrl")
		}
	}
	if relayManagedTokenMatches(currentAPIKey) {
		if value, exists := originalFields["apiKey"]; exists {
			currentFields["apiKey"] = value
		} else {
			delete(currentFields, "apiKey")
		}
	}
	result, err := marshalOrderedPiProvider(currentFields)
	if err != nil {
		return cloneRawMessage(current)
	}
	return result
}
