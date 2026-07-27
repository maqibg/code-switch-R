package services

import (
	"encoding/json"
	"fmt"
)

// defaultLogRetentionDays 全新安装的默认日志保留天数。
//
// 早先默认是 0（永不清理），request_log / relay_attempt 会无限增长，
// 长期运行后数据库膨胀到影响启动和统计性能。
const defaultLogRetentionDays = 90

// logRetentionNoticeText 升级用户看到的一次性提示。
const logRetentionNoticeText = "新版本为日志新增了默认保留期（90 天）。" +
	"为避免删除你已有的历史记录，你的设置仍保持“永不清理”。" +
	"如需自动清理，请在通用设置中设置日志保留天数。"

// migrateLogRetentionSettings 处理日志保留策略的老配置迁移。
//
// 判定依据是原始 JSON 里有没有 log_retention_initialized 字段，而不是它的值：
// LogRetentionDays 的 0 既可能是用户显式选择的"永不清理"，也可能是旧版本
// 根本没写这个字段，反序列化后的零值。只有看原始字节才能区分两者。
//
// 返回 (迁移后的设置, 是否发生变化)。
func migrateLogRetentionSettings(settings AppSettings, rawJSON []byte) (AppSettings, bool) {
	if hasJSONKey(rawJSON, "log_retention_initialized") {
		// 已经明确过，不再干预（此时 0 就是字面意义的"永不清理"）
		return settings, false
	}

	settings.LogRetentionInitialized = true
	if settings.LogRetentionDays <= 0 {
		// 保持用户现状（不清理），只提示新默认值的存在。
		// 绝不替用户把 0 改成 90——那等于未经同意删除历史数据。
		settings.LogRetentionDays = 0
		settings.LogRetentionNotice = logRetentionNoticeText
	}
	return settings, true
}

// hasJSONKey 判断顶层 JSON 对象里是否存在指定键（区分"键不存在"和"键值为零值"）
func hasJSONKey(rawJSON []byte, key string) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(rawJSON, &probe); err != nil {
		return false
	}
	_, exists := probe[key]
	return exists
}

// AcknowledgeLogRetentionNotice 清除日志保留策略的一次性提示。
// 前端展示过提示后调用。
func (as *AppSettingsService) AcknowledgeLogRetentionNotice() (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	settings, err := as.loadLocked()
	if err != nil {
		return settings, err
	}
	if settings.LogRetentionNotice == "" {
		return settings, nil
	}
	settings.LogRetentionNotice = ""
	if err := as.saveLocked(settings); err != nil {
		return settings, fmt.Errorf("清除日志保留提示失败: %w", err)
	}
	return settings, nil
}
