package services

import (
	"testing"

	"github.com/daodao97/xgo/xdb"
)

func setupSettingsTestDB(t *testing.T) *SettingsService {
	t.Helper()
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return &SettingsService{}
}

// 核心契约：单一来源。整份保存与按字段更新走同一行，读回来必须一致。
//
// 收敛前这份配置有两处真相：JSON 文件存全部字段，而 UI 改动的开关、阈值和
// 拉黑时长只写 app_settings 的三个独立键，读取时靠"先读 JSON 再用独立键覆盖"
// 打补丁。反方向就丢：整份保存只写 JSON，存进去的值被下次读取时的旧键覆盖。
func TestBlacklistLevelConfigSingleSourceOfTruth(t *testing.T) {
	ss := setupSettingsTestDB(t)

	config := DefaultBlacklistLevelConfig()
	config.EnableLevelBlacklist = true
	config.FailureThreshold = 7
	config.FallbackDurationMinutes = 15
	config.ForgivenessHours = 5
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !loaded.EnableLevelBlacklist || loaded.FailureThreshold != 7 ||
		loaded.FallbackDurationMinutes != 15 || loaded.ForgivenessHours != 5 {
		t.Fatalf("整份保存后读回不一致: %+v", loaded)
	}

	// 收敛后 GetBlacklistSettings 读的就是配置里那两个字段
	threshold, duration, err := ss.GetBlacklistSettings()
	if err != nil {
		t.Fatalf("读取阈值与时长失败: %v", err)
	}
	if threshold != 7 || duration != 15 {
		t.Errorf("GetBlacklistSettings 应与配置一致，得到 threshold=%d duration=%d", threshold, duration)
	}
}

// 按字段更新阈值与时长后，整份配置读回必须跟着变，其余字段不受影响
func TestUpdateBlacklistSettingsWritesLevelConfig(t *testing.T) {
	ss := setupSettingsTestDB(t)

	config := DefaultBlacklistLevelConfig()
	config.FailureThreshold = 3
	config.FallbackDurationMinutes = 30
	config.ForgivenessHours = 4
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("初始保存失败: %v", err)
	}

	if err := ss.UpdateBlacklistSettings(8, 60); err != nil {
		t.Fatalf("更新阈值与时长失败: %v", err)
	}

	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if loaded.FailureThreshold != 8 {
		t.Errorf("failureThreshold 应为 8，实际 %d", loaded.FailureThreshold)
	}
	if loaded.FallbackDurationMinutes != 60 {
		t.Errorf("fallbackDurationMinutes 应为 60，实际 %d", loaded.FallbackDurationMinutes)
	}
	// 按字段更新不应动到其他字段
	if loaded.ForgivenessHours != 4 {
		t.Errorf("未涉及的字段应保持原值 4，实际 %v", loaded.ForgivenessHours)
	}
}

// 等级拉黑开关的 set→get 必须闭环（原先 Set 写独立键、Get 读配置行）
func TestLevelBlacklistEnabledRoundTrip(t *testing.T) {
	ss := setupSettingsTestDB(t)

	if err := ss.SetLevelBlacklistEnabled(true); err != nil {
		t.Fatalf("设置开关失败: %v", err)
	}
	enabled, err := ss.GetLevelBlacklistEnabled()
	if err != nil {
		t.Fatalf("读取开关失败: %v", err)
	}
	if !enabled {
		t.Error("设为 true 后应读回 true")
	}
	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !loaded.EnableLevelBlacklist {
		t.Error("整份配置里的同名字段也应为 true")
	}

	if err := ss.SetLevelBlacklistEnabled(false); err != nil {
		t.Fatalf("关闭开关失败: %v", err)
	}
	if enabled, err := ss.GetLevelBlacklistEnabled(); err != nil || enabled {
		t.Errorf("设为 false 后应读回 false（err=%v）", err)
	}
}

// 两个开关是不同概念，不能互相影响：
// enable_blacklist 决定是否拉黑，enableLevelBlacklist 决定用等级模式还是固定模式。
// ShouldUseFixedMode 分别读两者再组合判断，同步它们会让"总开关关闭"
// 变成"等级拉黑也被关掉"这种用户没要求的行为。
func TestBlacklistSwitchesAreIndependent(t *testing.T) {
	ss := setupSettingsTestDB(t)

	if err := ss.SetLevelBlacklistEnabled(true); err != nil {
		t.Fatalf("开启等级拉黑失败: %v", err)
	}
	if err := ss.UpdateBlacklistEnabled(false); err != nil {
		t.Fatalf("关闭总开关失败: %v", err)
	}

	levelEnabled, err := ss.GetLevelBlacklistEnabled()
	if err != nil {
		t.Fatalf("读取等级开关失败: %v", err)
	}
	if !levelEnabled {
		t.Error("关闭拉黑总开关不应改动等级拉黑开关")
	}
	if ss.IsBlacklistEnabled() {
		t.Error("总开关应为 false")
	}

	// 反方向：整份保存不应改动总开关
	if err := ss.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("开启总开关失败: %v", err)
	}
	config := DefaultBlacklistLevelConfig()
	config.EnableLevelBlacklist = false
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	if !ss.IsBlacklistEnabled() {
		t.Error("保存等级配置不应关掉拉黑总开关")
	}
}

// 缺失字段应保留默认值而不是零值（旧数据里没有新增字段）
func TestBlacklistLevelConfigKeepsDefaultsForMissingFields(t *testing.T) {
	ss := setupSettingsTestDB(t)
	db, _ := xdb.DB("default")

	// 只写一个字段，模拟旧版本存下来的部分配置
	if _, err := db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, '{"failureThreshold":4}')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		blacklistLevelConfigSettingKey,
	); err != nil {
		t.Fatalf("写入部分配置失败: %v", err)
	}

	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	defaults := DefaultBlacklistLevelConfig()
	if loaded.FailureThreshold != 4 {
		t.Errorf("显式字段应生效，实际 %d", loaded.FailureThreshold)
	}
	if loaded.DedupeWindowSeconds != defaults.DedupeWindowSeconds {
		t.Errorf("缺失字段应保留默认值 %d，实际 %d",
			defaults.DedupeWindowSeconds, loaded.DedupeWindowSeconds)
	}
	if loaded.ForgivenessHours != defaults.ForgivenessHours {
		t.Errorf("缺失字段应保留默认值 %v，实际 %v",
			defaults.ForgivenessHours, loaded.ForgivenessHours)
	}
}

// 配置行内容损坏时必须报错，不能静默退回默认值：
// 静默退回等于把用户配好的等级策略换成另一套，且没有任何提示
func TestBlacklistLevelConfigRejectsCorruptRow(t *testing.T) {
	ss := setupSettingsTestDB(t)
	db, _ := xdb.DB("default")

	if _, err := db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, 'not json')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		blacklistLevelConfigSettingKey,
	); err != nil {
		t.Fatalf("写入损坏配置失败: %v", err)
	}

	if _, err := ss.GetBlacklistLevelConfig(); err == nil {
		t.Error("配置行损坏时应报错")
	}
}
