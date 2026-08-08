package services

import (
	"fmt"
	"sort"
)

// syncLiveProvidersLocked 以 OpenCode 当前配置为已应用供应商的真实状态。
// 外部新增或修改会更新本项目；外部删除只会取消应用，保留本项目卡片。
func (s *OpenCodeService) syncLiveProvidersLocked() ([]Provider, error) {
	if s.providerStore == nil {
		return nil, fmt.Errorf("OpenCode 供应商存储服务未初始化")
	}
	path, _, format, err := s.resolveTarget()
	if err != nil {
		return nil, err
	}
	_, document, currentHash, err := readOpenCodeDocument(path)
	if err != nil {
		return nil, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return nil, err
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

	// 所有 live 项都视为“已应用”。外部内容是这一状态下的真实配置。
	for key, raw := range document.Providers {
		provider, parseErr := providerFromOpenCodeRaw(key, raw)
		if parseErr != nil {
			return nil, parseErr
		}
		if current, exists := byKey[key]; exists {
			provider.ID = current.ID
			provider.ModelMapping = current.ModelMapping
			provider.AuthScheme = current.AuthScheme
			provider.AuthHeader = current.AuthHeader
			provider.Headers = current.Headers
			provider.ProxyEnabled = current.ProxyEnabled
		}
		provider.Enabled = true
		provider.Level = 1
		provider.openCode.RawProvider = cloneRaw(raw)
		byKey[key] = provider
		state.Managed[openCodeProviderStorageKey(path, key)] = openCodeManagedState{
			TargetPath: path, ProviderKey: key, InjectedHash: sha256Hex(raw), UpdatedAt: openCodeTimeNow(),
		}
	}

	// live 中已不存在的供应商只取消应用，不删除本项目保存的资料。
	for storageKey, managed := range state.Managed {
		if managed.TargetPath != path {
			continue
		}
		if _, live := document.Providers[managed.ProviderKey]; live {
			continue
		}
		delete(state.Managed, storageKey)
	}

	providers := make([]Provider, 0, len(byKey))
	for _, provider := range byKey {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].openCode.ProviderKey < providers[j].openCode.ProviderKey
	})
	oldProviders := cloneOpenCodeProviders(existing)
	if err := s.saveOpenCodeProviders(providers); err != nil {
		return nil, err
	}
	state.Targets[path] = openCodeTargetState{Path: path, Format: format, LastHash: currentHash, UpdatedAt: openCodeTimeNow()}
	if err := saveOpenCodeState(state); err != nil {
		_ = s.saveOpenCodeProviders(oldProviders)
		return nil, fmt.Errorf("保存 OpenCode 同步状态失败: %w", err)
	}
	return providers, nil
}

func (s *OpenCodeService) ensureOpenCodeBaselineLocked() error {
	path, _, _, err := s.resolveTarget()
	if err != nil {
		return err
	}
	_, _, currentHash, err := readOpenCodeDocument(path)
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
	return nil
}
