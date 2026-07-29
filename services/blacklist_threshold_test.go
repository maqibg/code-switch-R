package services

import (
	"testing"
)

// 阈值为 1 时首次失败就必须拉黑。
//
// 原实现的两条记账路径都在"首次失败"分支插入 failure_count=1 后直接 return，
// 不比较阈值。于是 UI 上允许的阈值 1 完全失效：每次请求重试一次就切换 provider，
// 坏 provider 永远进不了黑名单、一直被重试。
func TestBlacklistThresholdOneBlocksOnFirstFailure(t *testing.T) {
	for _, mode := range []struct {
		name      string
		levelMode bool
	}{
		{name: "固定模式", levelMode: false},
		{name: "等级模式", levelMode: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			ss := setupSettingsTestDB(t)
			notifications := NewNotificationService(NewAppSettingsService(nil))
			bs := NewBlacklistService(ss, notifications)

			if err := ss.UpdateBlacklistEnabled(true); err != nil {
				t.Fatalf("打开拉黑总开关失败: %v", err)
			}
			config := DefaultBlacklistLevelConfig()
			config.EnableLevelBlacklist = mode.levelMode
			config.FailureThreshold = 1
			config.DedupeWindowSeconds = 0
			if err := ss.SaveBlacklistLevelConfig(config); err != nil {
				t.Fatalf("保存配置失败: %v", err)
			}

			target := BlacklistTargetFor("claude", Provider{ID: 1, Name: "Bad"})
			if err := bs.recordFailureFor(target); err != nil {
				t.Fatalf("记录失败失败: %v", err)
			}

			if blocked, _ := bs.isBlacklistedFor(target); !blocked {
				t.Error("阈值为 1 时首次失败应立即拉黑")
			}
		})
	}
}

// 阈值为 2 时首次失败只计数不拉黑，第二次才拉黑
func TestBlacklistThresholdTwoNeedsTwoFailures(t *testing.T) {
	for _, mode := range []struct {
		name      string
		levelMode bool
	}{
		{name: "固定模式", levelMode: false},
		{name: "等级模式", levelMode: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			ss := setupSettingsTestDB(t)
			notifications := NewNotificationService(NewAppSettingsService(nil))
			bs := NewBlacklistService(ss, notifications)

			if err := ss.UpdateBlacklistEnabled(true); err != nil {
				t.Fatalf("打开拉黑总开关失败: %v", err)
			}
			config := DefaultBlacklistLevelConfig()
			config.EnableLevelBlacklist = mode.levelMode
			config.FailureThreshold = 2
			config.DedupeWindowSeconds = 0
			if err := ss.SaveBlacklistLevelConfig(config); err != nil {
				t.Fatalf("保存配置失败: %v", err)
			}

			target := BlacklistTargetFor("claude", Provider{ID: 1, Name: "Bad"})

			if err := bs.recordFailureFor(target); err != nil {
				t.Fatalf("记录第一次失败失败: %v", err)
			}
			if blocked, _ := bs.isBlacklistedFor(target); blocked {
				t.Fatal("阈值为 2 时首次失败不应拉黑")
			}
			if got := failureCountFor(t, "claude", "Bad"); got != 1 {
				t.Errorf("首次失败后计数应为 1，实际 %d", got)
			}

			if err := bs.recordFailureFor(target); err != nil {
				t.Fatalf("记录第二次失败失败: %v", err)
			}
			if blocked, _ := bs.isBlacklistedFor(target); !blocked {
				t.Error("第二次失败应达到阈值并拉黑")
			}
		})
	}
}

// 成功清零计数：清零后再失败一次，阈值 2 时不应拉黑
func TestBlacklistSuccessResetsFailureCount(t *testing.T) {
	ss := setupSettingsTestDB(t)
	notifications := NewNotificationService(NewAppSettingsService(nil))
	bs := NewBlacklistService(ss, notifications)

	if err := ss.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("打开拉黑总开关失败: %v", err)
	}
	config := DefaultBlacklistLevelConfig()
	config.FailureThreshold = 2
	config.DedupeWindowSeconds = 0
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	target := BlacklistTargetFor("claude", Provider{ID: 1, Name: "Flaky"})
	if err := bs.recordFailureFor(target); err != nil {
		t.Fatalf("记录失败失败: %v", err)
	}
	if err := bs.recordSuccessFor(target); err != nil {
		t.Fatalf("记录成功失败: %v", err)
	}
	if got := failureCountFor(t, "claude", "Flaky"); got != 0 {
		t.Errorf("成功后计数应清零，实际 %d", got)
	}

	if err := bs.recordFailureFor(target); err != nil {
		t.Fatalf("记录失败失败: %v", err)
	}
	if blocked, _ := bs.isBlacklistedFor(target); blocked {
		t.Error("清零后单次失败不应达到阈值 2")
	}
}
