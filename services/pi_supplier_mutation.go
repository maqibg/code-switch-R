package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	PiSupplierMutationUpsert = "upsert"
	PiSupplierMutationDelete = "delete"
	PiSupplierMutationToggle = "toggle"
)

type PiSupplierMutationRequest struct {
	Action            string         `json:"action"`
	ExpectedRevision  string         `json:"expectedRevision"`
	ProviderID        int64          `json:"providerId,omitempty"`
	Provider          Provider       `json:"provider"`
	NewPlatformModels []PiModelEntry `json:"newPlatformModels,omitempty"`
}

type PiSupplierMutationResult struct {
	Provider Provider `json:"provider"`
	Revision string   `json:"revision"`
}

func (s *PiSettingsService) GetSupplier(providerID int64) (Provider, error) {
	if s.providerService == nil {
		return Provider{}, fmt.Errorf("Pi Provider 服务未初始化")
	}
	providers, err := s.providerService.loadProvidersRaw("pi")
	if err != nil {
		return Provider{}, fmt.Errorf("读取 Pi 上游供应商失败: %w", err)
	}
	index := providerIndexByID(providers, providerID)
	if index < 0 {
		return Provider{}, fmt.Errorf("Pi 上游供应商不存在: id=%d", providerID)
	}
	return providers[index], nil
}

func (s *PiSettingsService) SaveSupplierMutation(input PiSupplierMutationRequest) (PiSupplierMutationResult, error) {
	result := PiSupplierMutationResult{}
	if s.providerService == nil {
		return result, fmt.Errorf("Pi Provider 服务未初始化")
	}
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.requireRuntimeRevision(input.ExpectedRevision); err != nil {
		return result, err
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	providers, err := s.providerService.loadProvidersRaw("pi")
	if err != nil {
		return result, fmt.Errorf("读取 Pi 上游供应商失败: %w", err)
	}
	next := append([]Provider(nil), providers...)
	var changed Provider
	var renameID int64
	switch action {
	case PiSupplierMutationUpsert:
		changed = input.Provider
		changed.Name = strings.TrimSpace(changed.Name)
		changed.PiPlatform = strings.TrimSpace(changed.PiPlatform)
		changed.PiTemplate = ""
		if changed.PiPlatform == "" {
			return result, fmt.Errorf("Pi 上游供应商必须归属一个平台")
		}
		index := providerIndexByID(next, input.ProviderID)
		if input.ProviderID == 0 || index < 0 {
			changed.ID = nextProviderID(next)
			next = append(next, changed)
		} else {
			changed.ID = input.ProviderID
			if providers[index].Name != changed.Name {
				renameID = changed.ID
			}
			next[index] = changed
		}
	case PiSupplierMutationDelete:
		index := providerIndexByID(next, input.ProviderID)
		if index < 0 {
			return result, fmt.Errorf("Pi 上游供应商不存在: id=%d", input.ProviderID)
		}
		changed = next[index]
		next = append(next[:index], next[index+1:]...)
	case PiSupplierMutationToggle:
		index := providerIndexByID(next, input.ProviderID)
		if index < 0 {
			return result, fmt.Errorf("Pi 上游供应商不存在: id=%d", input.ProviderID)
		}
		next[index].Enabled = input.Provider.Enabled
		changed = next[index]
	default:
		return result, fmt.Errorf("不支持的 Pi 供应商变更动作: %s", input.Action)
	}

	allowedRenameID := int64(0)
	if renameID != 0 {
		allowedRenameID = renameID
	}
	if _, err := prepareProviderSave("pi", next, providers, allowedRenameID); err != nil {
		return result, err
	}

	modelsBackup, modelsExisted, modelsWrittenHash, err := s.addPlatformModelsBeforeSupplierSave(changed.PiPlatform, input.NewPlatformModels)
	if err != nil {
		return result, err
	}
	saveProvider := func() error {
		if renameID != 0 {
			return s.providerService.SaveProvidersWithRename("pi", renameID, next)
		}
		return s.providerService.SaveProviders("pi", next)
	}
	if err := saveProvider(); err != nil {
		if modelsWrittenHash != "" {
			rollbackErr := restoreModelsIfUnchanged(s.modelsPath(), modelsBackup, modelsExisted, modelsWrittenHash)
			if rollbackErr != nil {
				return result, fmt.Errorf("保存 Pi 上游供应商失败: %w; models.json 补偿回滚失败: %v", err, rollbackErr)
			}
		}
		return result, fmt.Errorf("保存 Pi 上游供应商失败: %w", err)
	}
	result.Provider = changed
	result.Revision = s.currentRuntimeRevision()
	return result, nil
}

func (s *PiSettingsService) addPlatformModelsBeforeSupplierSave(platformID string, additions []PiModelEntry) ([]byte, bool, string, error) {
	if len(additions) == 0 {
		return nil, false, "", nil
	}
	platformID = strings.TrimSpace(platformID)
	root, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return nil, false, "", err
	}
	raw, exists := providers[platformID]
	if !exists {
		return nil, false, "", fmt.Errorf("Pi 平台不存在: %s", platformID)
	}
	state, err := s.loadPlatformState()
	if err != nil {
		return nil, false, "", err
	}
	if entry, tracked := state.Platforms[platformID]; tracked {
		if !piManagedProviderConnectionMatches(raw, s.platformBaseURL(platformID)) {
			return nil, false, "", fmt.Errorf("Pi 平台 %q 存在托管冲突，请先处理冲突", platformID)
		}
		if entry.InjectedAuthHash != "" {
			authRoot, _, authErr := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
			if authErr != nil {
				return nil, false, "", fmt.Errorf("读取 Pi auth.json 失败: %w", authErr)
			}
			if !piManagedAuthMatches(authRoot, platformID, entry) {
				return nil, false, "", fmt.Errorf("Pi 平台 %q 的 auth.json 存在托管冲突，请先处理冲突", platformID)
			}
		}
	}
	var source piModelsProviderFile
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, false, "", fmt.Errorf("解析 Pi 平台 %q 失败: %w", platformID, err)
	}
	existing := make(map[string]struct{}, len(source.Models))
	for _, model := range source.Models {
		existing[strings.TrimSpace(model.ID)] = struct{}{}
	}
	for index := range additions {
		addition := clonePiModelEntry(additions[index])
		addition.ID = strings.TrimSpace(addition.ID)
		if addition.ID == "" {
			return nil, false, "", fmt.Errorf("新增平台模型 ID 不能为空")
		}
		if _, duplicate := existing[addition.ID]; duplicate {
			continue
		}
		if strings.TrimSpace(addition.Name) == "" {
			addition.Name = addition.ID
		}
		if len(addition.Input) == 0 {
			addition.Input = []string{"text"}
		}
		if messages := validateNativePiModelEntry(fmt.Sprintf("models[%d]", len(source.Models)), addition, source.API); len(messages) > 0 {
			return nil, false, "", fmt.Errorf("新增平台模型 %q 无效: %s", addition.ID, strings.Join(messages, "; "))
		}
		source.Models = append(source.Models, addition)
		existing[addition.ID] = struct{}{}
	}
	sort.SliceStable(source.Models, func(i, j int) bool { return source.Models[i].ID < source.Models[j].ID })
	backup, existed, err := readOptionalFile(s.modelsPath())
	if err != nil {
		return nil, false, "", fmt.Errorf("备份 Pi models.json 失败: %w", err)
	}
	input := providerTemplateFromFile(platformID, fingerprint, source)
	if err := writePiModelsProviderDocument(s.modelsPath(), root, providers, input); err != nil {
		return nil, false, "", err
	}
	written, err := os.ReadFile(s.modelsPath())
	if err != nil {
		rollbackErr := restoreOptionalFile(s.modelsPath(), backup, existed)
		return nil, false, "", fmt.Errorf("读取本次 models.json 写入结果失败: %w; 回滚: %v", err, rollbackErr)
	}
	return backup, existed, rawBytesHash(written), nil
}

func restoreModelsIfUnchanged(path string, backup []byte, existed bool, expectedHash string) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取当前 models.json 失败: %w", err)
	}
	if rawBytesHash(current) != expectedHash {
		return fmt.Errorf("models.json 在保存失败后又被外部修改，拒绝自动回滚")
	}
	return restoreOptionalFile(path, backup, existed)
}

func rawBytesHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func providerIndexByID(providers []Provider, id int64) int {
	for index := range providers {
		if providers[index].ID == id {
			return index
		}
	}
	return -1
}
