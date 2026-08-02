package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type piResolvedModelPricing struct {
	Cost    PiModelCost
	Source  string
	Version string
}

type piModelPricingCache struct {
	key            string
	builtinVersion string
	modelsModTime  time.Time
	modelsSize     int64
	modelsExists   bool
	entries        map[string]piResolvedModelPricing
}

// resolveModelPricing 按 Pi 自身的 provider+model 精确语义解析价格。
// 自定义 models 整体覆盖内置模型，modelOverrides.cost 则字段级合并；未命中时由调用方回退全局定价。
func (s *PiSettingsService) resolveModelPricing(providerID, modelID string) (*PiModelCost, string, string, error) {
	if s == nil {
		return nil, "", "", nil
	}
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return nil, "", "", nil
	}

	builtin, builtinErr := s.BuiltinModelsCatalog(false)
	builtinVersion := strings.TrimSpace(builtin.ModelVersion)
	modelsPath := s.modelsPath()
	info, statErr := os.Stat(modelsPath)
	modelsExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, "", "", fmt.Errorf("读取 Pi models.json 定价失败: %w", statErr)
	}

	s.pricingMu.Lock()
	defer s.pricingMu.Unlock()
	if s.pricingCache != nil &&
		s.pricingCache.builtinVersion == builtinVersion &&
		s.pricingCache.modelsExists == modelsExists &&
		(!modelsExists || (s.pricingCache.modelsSize == info.Size() && s.pricingCache.modelsModTime.Equal(info.ModTime()))) {
		resolved, exists := s.pricingCache.entries[piPricingKey(providerID, modelID)]
		if !exists {
			return nil, "", "", nil
		}
		cost := clonePiModelCost(resolved.Cost)
		return &cost, resolved.Source, resolved.Version, nil
	}

	modelsData := []byte(`{"providers":{}}`)
	if modelsExists {
		var readErr error
		modelsData, readErr = os.ReadFile(modelsPath)
		if readErr != nil {
			return nil, "", "", fmt.Errorf("读取 Pi models.json 定价失败: %w", readErr)
		}
	}
	cleaned := stripJSONComments(modelsData)
	modelsFingerprint := fingerprintPiModelsJSON(cleaned)
	cacheKey := modelsFingerprint + "\x00" + builtinVersion
	if s.pricingCache == nil || s.pricingCache.key != cacheKey {
		entries, err := buildPiModelPricingEntries(cleaned, builtin, builtinErr, modelsFingerprint)
		if err != nil {
			return nil, "", "", err
		}
		cache := &piModelPricingCache{
			key: cacheKey, builtinVersion: builtinVersion, modelsExists: modelsExists, entries: entries,
		}
		if modelsExists {
			cache.modelsModTime = info.ModTime()
			cache.modelsSize = info.Size()
		}
		s.pricingCache = cache
	}
	s.pricingCache.builtinVersion = builtinVersion
	s.pricingCache.modelsExists = modelsExists
	s.pricingCache.modelsModTime = time.Time{}
	s.pricingCache.modelsSize = 0
	if modelsExists {
		s.pricingCache.modelsModTime = info.ModTime()
		s.pricingCache.modelsSize = info.Size()
	}
	resolved, exists := s.pricingCache.entries[piPricingKey(providerID, modelID)]
	if !exists {
		return nil, "", "", nil
	}
	cost := clonePiModelCost(resolved.Cost)
	return &cost, resolved.Source, resolved.Version, nil
}

func buildPiModelPricingEntries(modelsData []byte, builtin PiBuiltinCatalogSnapshot, builtinErr error, modelsFingerprint string) (map[string]piResolvedModelPricing, error) {
	entries := make(map[string]piResolvedModelPricing)
	builtinVersion := strings.TrimSpace(builtin.ModelVersion)
	if builtinVersion == "" {
		builtinVersion = strings.TrimSpace(builtin.PiVersion)
	}
	if builtinVersion == "" {
		builtinVersion = "unavailable"
	}
	if builtinErr == nil {
		for _, provider := range builtin.Providers {
			for _, model := range provider.Models {
				if model.Cost == nil {
					continue
				}
				entries[piPricingKey(provider.ID, model.ID)] = piResolvedModelPricing{
					Cost: clonePiModelCost(*model.Cost), Source: pricingSourcePiBuiltin,
					Version: "pi-builtin:" + builtinVersion,
				}
			}
		}
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(modelsData, &root); err != nil {
		return nil, fmt.Errorf("解析 Pi models.json 定价失败: %w", err)
	}
	providers, err := nestedJSONObject(root, "providers")
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	shortFingerprint := shortPricingVersion(modelsFingerprint)
	for providerID, raw := range providers {
		var provider piModelsProviderFile
		if err := json.Unmarshal(raw, &provider); err != nil {
			return nil, fmt.Errorf("解析 Pi Provider %q 定价失败: %w", providerID, err)
		}
		for modelID, override := range provider.ModelOverrides {
			if override.Cost == nil {
				continue
			}
			key := piPricingKey(providerID, modelID)
			base, exists := entries[key]
			if !exists {
				continue
			}
			base.Cost = applyPiCostOverride(base.Cost, *override.Cost)
			base.Source = pricingSourcePiOverride
			base.Version = "pi-override:" + shortFingerprint + "@" + builtinVersion
			entries[key] = base
		}
		for _, model := range provider.Models {
			modelID := strings.TrimSpace(model.ID)
			if modelID == "" {
				continue
			}
			cost := PiModelCost{}
			if model.Cost != nil {
				cost = clonePiModelCost(*model.Cost)
			}
			entries[piPricingKey(providerID, modelID)] = piResolvedModelPricing{
				Cost: cost, Source: pricingSourcePiCustom,
				Version: "pi-custom:" + shortFingerprint,
			}
		}
	}
	return entries, nil
}

func piPricingKey(providerID, modelID string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)
}

func applyPiCostOverride(base PiModelCost, override PiModelOverrideCost) PiModelCost {
	if override.Input != nil {
		base.Input = *override.Input
	}
	if override.Output != nil {
		base.Output = *override.Output
	}
	if override.CacheRead != nil {
		base.CacheRead = *override.CacheRead
	}
	if override.CacheWrite != nil {
		base.CacheWrite = *override.CacheWrite
	}
	if override.Tiers != nil {
		base.Tiers = append([]PiModelCostTier(nil), override.Tiers...)
	}
	return base
}

func clonePiModelCost(source PiModelCost) PiModelCost {
	cloned := source
	cloned.Input = normalizePiMoneyString(cloned.Input)
	cloned.Output = normalizePiMoneyString(cloned.Output)
	cloned.CacheRead = normalizePiMoneyString(cloned.CacheRead)
	cloned.CacheWrite = normalizePiMoneyString(cloned.CacheWrite)
	cloned.Tiers = append([]PiModelCostTier(nil), source.Tiers...)
	for index := range cloned.Tiers {
		cloned.Tiers[index].Input = normalizePiMoneyString(cloned.Tiers[index].Input)
		cloned.Tiers[index].Output = normalizePiMoneyString(cloned.Tiers[index].Output)
		cloned.Tiers[index].CacheRead = normalizePiMoneyString(cloned.Tiers[index].CacheRead)
		cloned.Tiers[index].CacheWrite = normalizePiMoneyString(cloned.Tiers[index].CacheWrite)
	}
	return cloned
}

func normalizePiMoneyString(value string) string {
	amount, err := parseMoney(value)
	if err != nil {
		return "0"
	}
	return moneyString(amount)
}
