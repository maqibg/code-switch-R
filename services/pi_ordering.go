package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const piUIStateVersion = 1

type PiUIState struct {
	Version       int      `json:"version"`
	PlatformOrder []string `json:"platformOrder"`
	DebugLogging  bool     `json:"debugLogging"`
}

func (s *PiSettingsService) SavePlatformOrder(providerIDs []string, expectedRevision string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.requireRuntimeRevision(expectedRevision); err != nil {
		return err
	}
	_, providers, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	if err := validatePlatformOrder(providerIDs, providers); err != nil {
		return err
	}
	state, err := s.loadUIState()
	if err != nil {
		return err
	}
	state.Version = piUIStateVersion
	state.PlatformOrder = append([]string(nil), providerIDs...)
	if err := AtomicWriteJSON(s.uiStateFile(), state); err != nil {
		return fmt.Errorf("保存 Pi 平台排序失败: %w", err)
	}
	return nil
}

func (s *PiSettingsService) SaveSupplierOrder(platformID string, providerIDs []int64, expectedRevision string) error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.requireRuntimeRevision(expectedRevision); err != nil {
		return err
	}
	if s.providerService == nil {
		return fmt.Errorf("Pi Provider 服务未初始化")
	}
	platformID = strings.TrimSpace(platformID)
	providers, err := s.providerService.loadProvidersRaw("pi")
	if err != nil {
		return fmt.Errorf("读取 Pi 上游供应商失败: %w", err)
	}
	positions := make([]int, 0)
	current := make(map[int64]Provider)
	for index, provider := range providers {
		if provider.PiPlatformKey() != platformID {
			continue
		}
		positions = append(positions, index)
		current[provider.ID] = provider
	}
	if len(providerIDs) != len(positions) {
		return fmt.Errorf("Pi 平台 %q 的供应商排序集合已变化，请刷新后重试", platformID)
	}
	seen := make(map[int64]struct{}, len(providerIDs))
	lastLevel := 0
	for index, providerID := range providerIDs {
		provider, exists := current[providerID]
		if !exists {
			return fmt.Errorf("供应商 id=%d 不属于 Pi 平台 %q", providerID, platformID)
		}
		if _, duplicate := seen[providerID]; duplicate {
			return fmt.Errorf("供应商排序包含重复 id=%d", providerID)
		}
		seen[providerID] = struct{}{}
		level := provider.Level
		if level <= 0 {
			level = 1
		}
		if lastLevel > level {
			return fmt.Errorf("供应商只能在同一 Level 内拖拽排序；跨 Level 优先级请编辑 Level")
		}
		lastLevel = level
		providers[positions[index]] = provider
	}
	if err := s.providerService.SaveProviders("pi", providers); err != nil {
		return fmt.Errorf("保存 Pi 供应商排序失败: %w", err)
	}
	return nil
}

func (s *PiSettingsService) loadUIState() (PiUIState, error) {
	state := PiUIState{Version: piUIStateVersion, PlatformOrder: []string{}}
	if err := ReadJSONFile(s.uiStateFile(), &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("读取 Pi 页面状态失败: %w", err)
	}
	if state.Version != piUIStateVersion {
		return state, fmt.Errorf("不支持的 Pi 页面状态版本: %d", state.Version)
	}
	return state, nil
}

func (s *PiSettingsService) uiStateFile() string {
	if path := strings.TrimSpace(s.uiStatePath); path != "" {
		return path
	}
	return filepath.Join(s.configDir, ".code-switch-r-ui.json")
}

func validatePlatformOrder(order []string, providers map[string]json.RawMessage) error {
	if len(order) != len(providers) {
		return fmt.Errorf("Pi 平台排序集合已变化，请刷新后重试")
	}
	seen := make(map[string]struct{}, len(order))
	for _, providerID := range order {
		providerID = strings.TrimSpace(providerID)
		if _, exists := providers[providerID]; !exists {
			return fmt.Errorf("Pi 平台排序包含未知 Provider: %s", providerID)
		}
		if _, duplicate := seen[providerID]; duplicate {
			return fmt.Errorf("Pi 平台排序包含重复 Provider: %s", providerID)
		}
		seen[providerID] = struct{}{}
	}
	return nil
}

func applyPiPlatformOrder(platforms []PiRuntimePlatform, order []string) []PiRuntimePlatform {
	if len(platforms) < 2 || len(order) == 0 {
		return platforms
	}
	byID := make(map[string]PiRuntimePlatform, len(platforms))
	for _, platform := range platforms {
		byID[platform.ProviderID] = platform
	}
	result := make([]PiRuntimePlatform, 0, len(platforms))
	for _, providerID := range order {
		if platform, exists := byID[providerID]; exists {
			result = append(result, platform)
			delete(byID, providerID)
		}
	}
	for _, platform := range platforms {
		if remaining, exists := byID[platform.ProviderID]; exists {
			result = append(result, remaining)
			delete(byID, platform.ProviderID)
		}
	}
	return result
}
