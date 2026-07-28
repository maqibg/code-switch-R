package services

import (
	"context"
	"fmt"
	"strings"
)

// A1 第 5 步：Gemini provider 并入 provider 表。
//
// 为什么要并:GeminiProvider 是与 Provider 平行的另一套类型（ID 是 string，
// 存在独立的 gemini-providers.json，有自己的一整套 CRUD）。这直接导致
//   - 日志与黑名单拿不到 provider_id，只能按名字关联
//   - 转发循环必须为 Gemini 单开一套（roundRobinOrderGemini 等）
//   - provider_delete / provider_rename 里散落 gemini 特判
//
// 怎么并:存储统一进 provider 表（platform='gemini'），
// 但对外仍暴露 GeminiProvider 与它的 string ID——原 string ID 存进
// config_json 的 gemini.legacyId，因此 Wails 绑定与前端零改动。

// geminiScope Gemini provider 在 provider 表中的范围
var geminiScope = providerScope{platform: "gemini"}

// toProvider 把 GeminiProvider 转成统一的 Provider。
//
// numericID 为 0 表示新增（由 SQLite 分配主键）；
// 已存在的记录必须传入其 int64 主键，否则会被当成新行插入。
func (g GeminiProvider) toProvider(numericID int64) Provider {
	return Provider{
		ID:           numericID,
		Name:         g.Name,
		APIURL:       g.BaseURL,
		APIKey:       g.APIKey,
		Enabled:      g.Enabled,
		ProxyEnabled: g.ProxyEnabled,
		Level:        g.Level,
		gemini: &geminiConfigPayload{
			LegacyID:            g.ID,
			WebsiteURL:          g.WebsiteURL,
			APIKeyURL:           g.APIKeyURL,
			Model:               g.Model,
			Description:         g.Description,
			Category:            g.Category,
			PartnerPromotionKey: g.PartnerPromotionKey,
			EnvConfig:           g.EnvConfig,
			SettingsConfig:      g.SettingsConfig,
		},
	}
}

// toGeminiProvider 把 Provider 还原成 GeminiProvider。
//
// legacyID 缺失时用 int64 主键兜底生成一个稳定 ID：
// 这只会发生在数据被外部工具改过的情况下，兜底保证 UI 不会拿到空 ID。
func (p Provider) toGeminiProvider() GeminiProvider {
	result := GeminiProvider{
		Name:         p.Name,
		BaseURL:      p.APIURL,
		APIKey:       p.APIKey,
		Enabled:      p.Enabled,
		ProxyEnabled: p.ProxyEnabled,
		Level:        p.Level,
		numericID:    p.ID,
	}
	if p.gemini != nil {
		result.ID = p.gemini.LegacyID
		result.WebsiteURL = p.gemini.WebsiteURL
		result.APIKeyURL = p.gemini.APIKeyURL
		result.Model = p.gemini.Model
		result.Description = p.gemini.Description
		result.Category = p.gemini.Category
		result.PartnerPromotionKey = p.gemini.PartnerPromotionKey
		result.EnvConfig = p.gemini.EnvConfig
		result.SettingsConfig = p.gemini.SettingsConfig
	}
	if strings.TrimSpace(result.ID) == "" {
		result.ID = fmt.Sprintf("gemini-%d", p.ID)
	}
	return result
}

// loadGeminiProvidersFromDB 从 provider 表读出 Gemini provider 列表
func loadGeminiProvidersFromDB() ([]GeminiProvider, error) {
	providers, err := loadProvidersFromDB(context.Background(), geminiScope)
	if err != nil {
		return nil, err
	}
	result := make([]GeminiProvider, 0, len(providers))
	for _, provider := range providers {
		result = append(result, provider.toGeminiProvider())
	}
	return result, nil
}

// saveGeminiProvidersToDB 整体替换 provider 表里的 Gemini provider。
//
// 需要先读一遍现有记录建立 legacyID → int64 主键的映射：
// 传入的 GeminiProvider 只带 string ID，若不映射回主键，
// 每次保存都会把现有行删掉再插入新行，provider_id 随之变化，
// 历史日志的关联就断了。
func saveGeminiProvidersToDB(providers []GeminiProvider) error {
	ctx := context.Background()

	existing, err := loadProvidersFromDB(ctx, geminiScope)
	if err != nil {
		return err
	}
	idByLegacy := make(map[string]int64, len(existing))
	for _, provider := range existing {
		if provider.gemini != nil && provider.gemini.LegacyID != "" {
			idByLegacy[provider.gemini.LegacyID] = provider.ID
		}
	}

	converted := make([]Provider, 0, len(providers))
	for _, g := range providers {
		converted = append(converted, g.toProvider(idByLegacy[g.ID]))
	}

	deleted, err := replaceProvidersInDB(ctx, geminiScope, converted)
	if err != nil {
		return err
	}
	if len(deleted) > 0 {
		// 清理被删除 Gemini provider 的关联数据
		if err := cleanupDeletedProviders("gemini", deleted); err != nil {
			logError("清理已删除 Gemini provider 的关联数据失败", "error", err)
		}
	}
	return nil
}

