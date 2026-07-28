package services

import (
	"encoding/json"
	"fmt"
	"os"
)

// migrateGeminiProviders 把 gemini-providers.json 导入 provider 表。
//
// 与其他平台不同，Gemini 的 provider 原本存在独立文件里且 ID 是 string。
// 这里为每条记录分配 int64 主键（由 SQLite 自增），原 string ID 存进
// config_json 的 gemini.legacyId，因此对外 API 形态不变。
func migrateGeminiProviders(tx sqlExecutor) error {
	// 已有数据说明导入过了
	var existing int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM provider WHERE platform = 'gemini'`,
	).Scan(&existing); err != nil {
		return fmt.Errorf("统计 Gemini provider 失败: %w", err)
	}
	if existing > 0 {
		return nil
	}

	path := getGeminiProvidersPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var providers []GeminiProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", path, err)
	}

	for order, g := range providers {
		converted := g.toProvider(0) // 0 = 由 SQLite 分配主键
		configJSON, err := marshalProviderConfig(converted)
		if err != nil {
			return fmt.Errorf("序列化 Gemini provider %q 配置失败: %w", g.Name, err)
		}
		if _, err := tx.Exec(`
			INSERT INTO provider
				(platform, source_id, name, api_url, api_key, enabled, level, sort_order, config_json)
			VALUES ('gemini', '', ?, ?, ?, ?, ?, ?, ?)
		`, converted.Name, converted.APIURL, converted.APIKey,
			boolToInt(converted.Enabled), converted.Level, order, configJSON,
		); err != nil {
			return fmt.Errorf("导入 Gemini provider %q 失败: %w", g.Name, err)
		}
	}

	if len(providers) > 0 {
		logInfo("已把 Gemini provider 导入数据库", "count", len(providers))
		// 与其他平台一致：导入后把原文件改名，避免让人误以为编辑它仍生效
		if err := os.Rename(path, path+".migrated"); err != nil {
			logWarn("标记已导入的 Gemini 配置文件失败", "path", path, "error", err)
		}
	}
	return nil
}
