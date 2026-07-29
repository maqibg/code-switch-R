package services

import (
	"encoding/json"
	"fmt"
	"os"
)

// blacklistLevelConfigSettingKey app_settings 里承载整份等级拉黑配置的键
const blacklistLevelConfigSettingKey = "blacklist_level_config"

// migrateBlacklistLevelConfig 把等级拉黑配置收敛到 app_settings 单一来源。
//
// 原状况是同一份配置有两处真相：
//   - JSON 文件（SaveBlacklistLevelConfig 只写这里，存全部字段）
//   - app_settings 的三个独立键：blacklist_level_enabled、
//     blacklist_failure_threshold、blacklist_duration_minutes
//     （UI 通过 SetLevelBlacklistEnabled、UpdateBlacklistSettings 只写这里）
//
// 读取时靠"先读 JSON、再用那几个独立键覆盖对应字段"打补丁维持一致，
// 代码注释里自称【关键修复】。反方向就丢：整份保存只写 JSON 文件，
// 存进去的开关与阈值会被下次读取时的旧独立键覆盖掉。
//
// 合并规则：以 JSON 文件为基底（它有全部字段），
// 三个重叠字段以独立键现值为准——因为 UI 改动只落到那里，
// 那才是用户最后一次操作的结果，也是原实现读取时的优先级。
//
// 不动 enable_blacklist：它是拉黑总开关，与本配置里的
// EnableLevelBlacklist（等级拉黑开关）是两个概念，仍留在独立键上。
func migrateBlacklistLevelConfig(tx sqlExecutor) error {
	// 已经迁移过就跳过
	var existing string
	err := tx.QueryRow(
		`SELECT value FROM app_settings WHERE key = ?`, blacklistLevelConfigSettingKey,
	).Scan(&existing)
	if err == nil && existing != "" {
		return nil
	}

	config := DefaultBlacklistLevelConfig()

	// 基底：JSON 文件
	configPath, pathErr := GetBlacklistLevelConfigPath()
	if pathErr == nil {
		if data, readErr := os.ReadFile(configPath); readErr == nil && len(data) > 0 {
			if err := json.Unmarshal(data, config); err != nil {
				// 解析失败不阻塞迁移：用默认值加独立键现值继续，
				// 但要让问题可见而不是静默丢配置
				logWarn("解析等级拉黑配置文件失败，将只使用数据库现值",
					"path", configPath, "error", err)
			}
		}
	}

	// 覆盖：三个重叠字段以独立键现值为准
	if enabled, ok := readBoolSetting(tx, "blacklist_level_enabled"); ok {
		config.EnableLevelBlacklist = enabled
	}
	if threshold, ok := readIntSetting(tx, "blacklist_failure_threshold"); ok && threshold > 0 {
		config.FailureThreshold = threshold
	}
	if duration, ok := readIntSetting(tx, "blacklist_duration_minutes"); ok && duration > 0 {
		config.FallbackDurationMinutes = duration
	}

	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化等级拉黑配置失败: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		blacklistLevelConfigSettingKey, string(payload),
	); err != nil {
		return fmt.Errorf("写入等级拉黑配置失败: %w", err)
	}

	// 删除已折叠的独立键，避免留下第二处真相
	if _, err := tx.Exec(
		`DELETE FROM app_settings WHERE key IN
		 ('blacklist_level_enabled', 'blacklist_failure_threshold', 'blacklist_duration_minutes')`,
	); err != nil {
		return fmt.Errorf("删除已折叠的拉黑配置键失败: %w", err)
	}

	// 原 JSON 文件改名，避免让人误以为编辑它仍生效
	if pathErr == nil {
		if _, statErr := os.Stat(configPath); statErr == nil {
			if err := os.Rename(configPath, configPath+".migrated"); err != nil {
				logWarn("标记已迁移的等级拉黑配置文件失败", "path", configPath, "error", err)
			}
		}
	}
	logInfo("等级拉黑配置已收敛到 app_settings")
	return nil
}

// readBoolSetting 从 app_settings 读一个布尔值
func readBoolSetting(tx sqlExecutor, key string) (bool, bool) {
	var value string
	if err := tx.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value); err != nil {
		return false, false
	}
	return value == "true", true
}

// readIntSetting 从 app_settings 读一个整数值
func readIntSetting(tx sqlExecutor, key string) (int, bool) {
	var value int
	if err := tx.QueryRow(`SELECT CAST(value AS INTEGER) FROM app_settings WHERE key = ?`, key).Scan(&value); err != nil {
		return 0, false
	}
	return value, true
}
