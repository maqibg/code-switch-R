package services

import (
	"database/sql"
	"os"
	"testing"
)

// 迁移的合并规则：JSON 文件做基底（有全部字段），
// 三个重叠字段以 app_settings 独立键现值为准——UI 改动只落到那里，
// 那是用户最后一次操作的结果，也是原实现读取时的优先级。
func TestBlacklistLevelConfigMigrationPrefersSettingKeysForOverlap(t *testing.T) {
	db := setupProviderImportEnv(t)
	applyBaselineOnly(t, db)

	setSettingForTest(t, db, "blacklist_level_enabled", "true")
	setSettingForTest(t, db, "blacklist_failure_threshold", "9")
	setSettingForTest(t, db, "blacklist_duration_minutes", "60")

	// JSON 文件里是另一套值
	configPath, err := GetBlacklistLevelConfigPath()
	if err != nil {
		t.Fatalf("获取路径失败: %v", err)
	}
	if err := atomicWriteFile(configPath, []byte(
		`{"enableLevelBlacklist":false,"failureThreshold":2,"fallbackDurationMinutes":5,"forgivenessHours":6}`,
	), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	ss := &SettingsService{}
	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !loaded.EnableLevelBlacklist {
		t.Error("重叠字段 enableLevelBlacklist 应取独立键现值 true")
	}
	if loaded.FailureThreshold != 9 {
		t.Errorf("重叠字段 failureThreshold 应取独立键现值 9，实际 %d", loaded.FailureThreshold)
	}
	if loaded.FallbackDurationMinutes != 60 {
		t.Errorf("重叠字段 fallbackDurationMinutes 应取独立键现值 60，实际 %d",
			loaded.FallbackDurationMinutes)
	}
	// 非重叠字段取 JSON
	if loaded.ForgivenessHours != 6 {
		t.Errorf("非重叠字段应取 JSON 值 6，实际 %v", loaded.ForgivenessHours)
	}

	// 已折叠的独立键必须删掉，否则留下第二处真相
	for _, key := range []string{
		"blacklist_level_enabled", "blacklist_failure_threshold", "blacklist_duration_minutes",
	} {
		if settingExistsForTest(t, db, key) {
			t.Errorf("独立键 %s 已折叠进配置行，应删除", key)
		}
	}

	// 拉黑总开关不属于这份配置，必须留着
	if !settingExistsForTest(t, db, "enable_blacklist") {
		t.Error("enable_blacklist 是拉黑总开关，不应被迁移删除")
	}

	// 原 JSON 文件改名，避免让人误以为编辑它仍生效
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("原配置文件应已改名，stat 返回 %v", err)
	}
	if _, err := os.Stat(configPath + ".migrated"); err != nil {
		t.Errorf("应留下 .migrated 文件: %v", err)
	}
}

// 没有 JSON 文件、也没有独立键改动过的库（全新安装）应得到默认配置
func TestBlacklistLevelConfigMigrationOnFreshDatabase(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	ss := &SettingsService{}
	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	defaults := DefaultBlacklistLevelConfig()
	if *loaded != *defaults {
		t.Errorf("全新库应得到默认配置\n实际: %+v\n期望: %+v", loaded, defaults)
	}
}

// JSON 文件损坏时迁移不应失败：用默认值加独立键现值继续，
// 但已由 logWarn 让问题可见，而不是静默丢配置
func TestBlacklistLevelConfigMigrationSurvivesCorruptFile(t *testing.T) {
	db := setupProviderImportEnv(t)
	applyBaselineOnly(t, db)
	setSettingForTest(t, db, "blacklist_failure_threshold", "6")

	configPath, err := GetBlacklistLevelConfigPath()
	if err != nil {
		t.Fatalf("获取路径失败: %v", err)
	}
	if err := atomicWriteFile(configPath, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}

	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("JSON 损坏不应阻塞迁移: %v", err)
	}

	ss := &SettingsService{}
	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if loaded.FailureThreshold != 6 {
		t.Errorf("应保留独立键现值 6，实际 %d", loaded.FailureThreshold)
	}
	defaults := DefaultBlacklistLevelConfig()
	if loaded.ForgivenessHours != defaults.ForgivenessHours {
		t.Errorf("其余字段应为默认值 %v，实际 %v",
			defaults.ForgivenessHours, loaded.ForgivenessHours)
	}
}

// 已迁移过的库重复跑迁移不应覆盖用户后来的改动
func TestBlacklistLevelConfigMigrationIsIdempotent(t *testing.T) {
	db := setupProviderImportEnv(t)
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}

	ss := &SettingsService{}
	if err := ss.UpdateBlacklistSettings(5, 15); err != nil {
		t.Fatalf("更新配置失败: %v", err)
	}

	// 清掉版本记录，强制重跑这条迁移
	if _, err := db.Exec(`DELETE FROM schema_version WHERE version = 7`); err != nil {
		t.Fatalf("清除版本记录失败: %v", err)
	}
	if err := RunMigrationsOn(db); err != nil {
		t.Fatalf("重跑迁移失败: %v", err)
	}

	ss.invalidateBlacklistLevelConfigCache()
	loaded, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if loaded.FailureThreshold != 5 || loaded.FallbackDurationMinutes != 15 {
		t.Errorf("重跑迁移不应覆盖已有配置，得到 threshold=%d duration=%d",
			loaded.FailureThreshold, loaded.FallbackDurationMinutes)
	}
}

func setSettingForTest(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value,
	); err != nil {
		t.Fatalf("写入设置 %s 失败: %v", key, err)
	}
}

func settingExistsForTest(t *testing.T, db *sql.DB, key string) bool {
	t.Helper()
	var one int
	err := db.QueryRow(`SELECT 1 FROM app_settings WHERE key = ?`, key).Scan(&one)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("查询设置 %s 失败: %v", key, err)
	}
	return true
}
