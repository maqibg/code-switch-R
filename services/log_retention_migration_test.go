package services

import (
	"encoding/json"
	"testing"
)

// 核心契约：升级用户（配置里没有 log_retention_initialized）不能被静默清理历史数据
func TestMigrateLogRetentionPreservesExistingUserBehavior(t *testing.T) {
	raw := []byte(`{"log_retention_days":0,"auto_update":true}`)
	settings := AppSettings{LogRetentionDays: 0, AutoUpdate: true}

	migrated, changed := migrateLogRetentionSettings(settings, raw)

	if !changed {
		t.Fatal("老配置应触发迁移")
	}
	if migrated.LogRetentionDays != 0 {
		t.Errorf("升级用户的保留天数必须保持不变（0=永不清理），不能替用户删数据，实际 %d", migrated.LogRetentionDays)
	}
	if !migrated.LogRetentionInitialized {
		t.Error("迁移后应标记为已确认")
	}
	if migrated.LogRetentionNotice == "" {
		t.Error("应写入一次性提示，让用户知道新默认值的存在")
	}
}

// 用户已显式设置过保留天数：迁移只加标记，不改值、不提示
func TestMigrateLogRetentionKeepsExplicitUserValue(t *testing.T) {
	raw := []byte(`{"log_retention_days":30}`)
	settings := AppSettings{LogRetentionDays: 30}

	migrated, changed := migrateLogRetentionSettings(settings, raw)

	if !changed {
		t.Fatal("缺少标记字段时应触发迁移")
	}
	if migrated.LogRetentionDays != 30 {
		t.Errorf("应保留用户显式设置的 30，实际 %d", migrated.LogRetentionDays)
	}
	if migrated.LogRetentionNotice != "" {
		t.Errorf("用户已显式设置过，不该提示，实际 %q", migrated.LogRetentionNotice)
	}
}

// 已确认过的配置不再迁移，此时 0 就是字面意义的"永不清理"
func TestMigrateLogRetentionSkipsAlreadyInitialized(t *testing.T) {
	raw := []byte(`{"log_retention_days":0,"log_retention_initialized":true}`)
	settings := AppSettings{LogRetentionDays: 0, LogRetentionInitialized: true}

	migrated, changed := migrateLogRetentionSettings(settings, raw)

	if changed {
		t.Error("已确认过的配置不应再次迁移")
	}
	if migrated.LogRetentionNotice != "" {
		t.Error("不应产生提示")
	}
}

// 全新安装走 defaultSettings，应直接是 90 天且无需提示
func TestDefaultSettingsUseNonZeroRetention(t *testing.T) {
	as := &AppSettingsService{}
	settings := as.defaultSettings()

	if settings.LogRetentionDays != defaultLogRetentionDays {
		t.Errorf("全新安装应使用默认保留期 %d，实际 %d", defaultLogRetentionDays, settings.LogRetentionDays)
	}
	if !settings.LogRetentionInitialized {
		t.Error("全新安装应视为已确认，避免误报提示")
	}
	if settings.LogRetentionNotice != "" {
		t.Error("全新安装不应有提示")
	}
}

// hasJSONKey 必须区分"键不存在"与"键值为零值"——这是整个迁移判定的基础
func TestHasJSONKeyDistinguishesAbsentFromZero(t *testing.T) {
	if hasJSONKey([]byte(`{"a":1}`), "log_retention_initialized") {
		t.Error("键不存在时应返回 false")
	}
	if !hasJSONKey([]byte(`{"log_retention_initialized":false}`), "log_retention_initialized") {
		t.Error("键存在但值为 false 时应返回 true")
	}
	if hasJSONKey([]byte(`not json`), "anything") {
		t.Error("非法 JSON 应返回 false")
	}
}

// 提示字段应能被序列化/反序列化，且为空时不写入文件
func TestLogRetentionNoticeOmittedWhenEmpty(t *testing.T) {
	data, err := json.Marshal(AppSettings{LogRetentionDays: 90, LogRetentionInitialized: true})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if hasJSONKey(data, "log_retention_notice") {
		t.Error("提示为空时不应写入 JSON（omitempty）")
	}

	data, err = json.Marshal(AppSettings{LogRetentionNotice: "x"})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !hasJSONKey(data, "log_retention_notice") {
		t.Error("提示非空时应写入 JSON")
	}
}
