package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// boolToSettingValue 把布尔值转成 app_settings 里存储的文本形式
func boolToSettingValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// settingsRowQuerier 覆盖 *sql.DB 与事务连接，让配置读取在两种场景下共用一份实现
type settingsRowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// loadBlacklistLevelConfigFrom 从 app_settings 读出整份等级拉黑配置。
//
// 以默认配置为基底再覆盖：新增字段在旧数据里缺失时保留默认值而不是零值。
func loadBlacklistLevelConfigFrom(ctx context.Context, q settingsRowQuerier) (*BlacklistLevelConfig, error) {
	config := DefaultBlacklistLevelConfig()

	var stored string
	err := q.QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key = ?`, blacklistLevelConfigSettingKey,
	).Scan(&stored)
	switch {
	case err == sql.ErrNoRows:
		// 配置行还不存在（迁移前的库或全新库）：用默认值
		return config, nil
	case err != nil:
		return nil, fmt.Errorf("读取等级拉黑配置失败: %w", err)
	}
	if stored == "" {
		return config, nil
	}
	if err := json.Unmarshal([]byte(stored), config); err != nil {
		return nil, fmt.Errorf("解析等级拉黑配置失败: %w", err)
	}
	return config, nil
}

// GetBlacklistLevelConfigPath 获取等级拉黑配置文件路径
func GetBlacklistLevelConfigPath() (string, error) {
	configDir, err := ensureAppConfigDir()
	if err != nil {
		return "", fmt.Errorf("创建项目配置目录失败: %w", err)
	}

	return filepath.Join(configDir, "blacklist-config.json"), nil
}

// GetBlacklistLevelConfig 获取等级拉黑配置。
//
// 单一来源是 app_settings 的 blacklist_level_config 行（迁移 v7 收敛）。
// 原先这份配置有两处真相：JSON 文件存全部字段，而 UI 改动的开关、阈值和
// 拉黑时长只写 app_settings 的三个独立键，读取时要"先读 JSON、
// 再用数据库覆盖那几个字段"打补丁维持一致（原注释自称【关键修复】）。
func (ss *SettingsService) GetBlacklistLevelConfig() (*BlacklistLevelConfig, error) {
	ss.cacheMu.Lock()
	if ss.blacklistLevelConfig != nil && time.Now().Before(ss.blacklistLevelConfigUntil) {
		cached := *ss.blacklistLevelConfig
		ss.cacheMu.Unlock()
		return &cached, nil
	}
	ss.cacheMu.Unlock()

	db, err := dbHandle()
	if err != nil {
		return nil, err
	}
	config, err := loadBlacklistLevelConfigFrom(context.Background(), db)
	if err != nil {
		return nil, err
	}

	ss.cacheMu.Lock()
	cached := *config
	ss.blacklistLevelConfig = &cached
	ss.blacklistLevelConfigUntil = time.Now().Add(settingsHotPathCacheTTL)
	ss.cacheMu.Unlock()
	result := cached
	return &result, nil
}

// SaveBlacklistLevelConfig 保存等级拉黑配置。
//
// 只写配置行。收敛前这里写 JSON 文件、而 UI 的开关与阈值写 app_settings
// 独立键，两条写路径天然分叉；现在读写同一行。
//
// 注意不要在这里顺带同步 enable_blacklist：那是拉黑总开关，
// 与本配置里的 EnableLevelBlacklist（等级拉黑开关）是两个概念，
// ShouldUseFixedMode 会分别读取再组合判断。
func (ss *SettingsService) SaveBlacklistLevelConfig(config *BlacklistLevelConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := dbExec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		blacklistLevelConfigSettingKey, string(data),
	); err != nil {
		return fmt.Errorf("写入等级拉黑配置失败: %w", err)
	}
	ss.invalidateBlacklistLevelConfigCache()

	return nil
}

// updateBlacklistLevelConfigFields 就地改配置行里的若干字段。
//
// 用读-改-写而不是 SQL 的 json_set：json_set 遇到非法 JSON 返回 NULL，
// 会把整份配置静默清空。这里在 BEGIN IMMEDIATE 事务内完成，
// 解析失败就报错回滚，配置行缺失时以默认值为基底建行。
func (ss *SettingsService) updateBlacklistLevelConfigFields(
	mutate func(*BlacklistLevelConfig),
) error {
	err := dbExecInImmediateTx(context.Background(), func(tx dbTxExecutor) error {
		config, err := loadBlacklistLevelConfigFrom(context.Background(), tx)
		if err != nil {
			return err
		}
		mutate(config)

		data, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("序列化配置失败: %w", err)
		}
		_, err = tx.ExecContext(context.Background(),
			`INSERT INTO app_settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			blacklistLevelConfigSettingKey, string(data),
		)
		return err
	})
	if err != nil {
		return err
	}
	ss.invalidateBlacklistLevelConfigCache()
	return nil
}

// UpdateBlacklistLevelConfig 更新等级拉黑配置
func (ss *SettingsService) UpdateBlacklistLevelConfig(config *BlacklistLevelConfig) error {
	// 验证配置
	if err := validateBlacklistLevelConfig(config); err != nil {
		return err
	}

	return ss.SaveBlacklistLevelConfig(config)
}

// validateBlacklistLevelConfig 验证等级拉黑配置
func validateBlacklistLevelConfig(config *BlacklistLevelConfig) error {
	if config.FailureThreshold < 1 || config.FailureThreshold > 10 {
		return fmt.Errorf("失败阈值必须在 1-10 之间")
	}

	if config.DedupeWindowSeconds < 1 || config.DedupeWindowSeconds > 300 {
		return fmt.Errorf("去重窗口必须在 1-300 秒之间")
	}

	if config.NormalDegradeIntervalHours < 0.1 || config.NormalDegradeIntervalHours > 24 {
		return fmt.Errorf("正常降级间隔必须在 0.1-24 小时之间")
	}

	if config.ForgivenessHours < 0.5 || config.ForgivenessHours > 72 {
		return fmt.Errorf("宽恕触发时间必须在 0.5-72 小时之间")
	}

	if config.JumpPenaltyWindowHours < 0.1 || config.JumpPenaltyWindowHours > 24 {
		return fmt.Errorf("跳级惩罚窗口必须在 0.1-24 小时之间")
	}

	// 验证等级时长（必须递增）
	if config.L1DurationMinutes < 1 || config.L1DurationMinutes > 10080 {
		return fmt.Errorf("L1 拉黑时长必须在 1-10080 分钟之间")
	}
	if config.L2DurationMinutes <= config.L1DurationMinutes {
		return fmt.Errorf("L2 拉黑时长必须大于 L1")
	}
	if config.L3DurationMinutes <= config.L2DurationMinutes {
		return fmt.Errorf("L3 拉黑时长必须大于 L2")
	}
	if config.L4DurationMinutes <= config.L3DurationMinutes {
		return fmt.Errorf("L4 拉黑时长必须大于 L3")
	}
	if config.L5DurationMinutes <= config.L4DurationMinutes {
		return fmt.Errorf("L5 拉黑时长必须大于 L4")
	}

	if config.FallbackMode != "fixed" && config.FallbackMode != "none" {
		return fmt.Errorf("fallbackMode 只支持 'fixed' 或 'none'")
	}

	if config.FallbackDurationMinutes < 1 || config.FallbackDurationMinutes > 10080 {
		return fmt.Errorf("fallback 拉黑时长必须在 1-10080 分钟之间")
	}

	return nil
}
