package services

import (
	"testing"
)

// 去重窗口：窗口内的重复失败只算一次。
//
// 目的是防止客户端自身的快速重试被当成多次独立失败，把 provider 误拉黑。
// 首次失败也必须落下窗口起始时间——这条容易在改动插入路径时丢掉。
func TestBlacklistDedupeWindowIgnoresRepeatWithinWindow(t *testing.T) {
	ss := setupSettingsTestDB(t)
	notifications := NewNotificationService(NewAppSettingsService(nil))
	bs := NewBlacklistService(ss, notifications)

	if err := ss.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("打开拉黑总开关失败: %v", err)
	}
	config := DefaultBlacklistLevelConfig()
	config.EnableLevelBlacklist = true
	config.FailureThreshold = 3
	config.DedupeWindowSeconds = 300 // 足够长，测试期间不会走出窗口
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	target := BlacklistTargetFor("claude", Provider{ID: 1, Name: "Retrying"})

	// 连续三次失败，但都在去重窗口内：只有第一次应被计数
	for i := 0; i < 3; i++ {
		if err := bs.recordFailureFor(target); err != nil {
			t.Fatalf("第 %d 次记录失败失败: %v", i+1, err)
		}
	}

	if got := failureCountFor(t, "claude", "Retrying"); got != 1 {
		t.Errorf("去重窗口内的重复失败只应计一次，实际 failure_count=%d", got)
	}
	if blocked, _ := bs.isBlacklistedFor(target); blocked {
		t.Error("去重后未达阈值，不应拉黑")
	}
}

// 去重窗口为 0 时每次失败都计数（failover 测试依赖这个配置）
func TestBlacklistZeroDedupeWindowCountsEveryFailure(t *testing.T) {
	ss := setupSettingsTestDB(t)
	notifications := NewNotificationService(NewAppSettingsService(nil))
	bs := NewBlacklistService(ss, notifications)

	if err := ss.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("打开拉黑总开关失败: %v", err)
	}
	config := DefaultBlacklistLevelConfig()
	config.EnableLevelBlacklist = true
	config.FailureThreshold = 3
	config.DedupeWindowSeconds = 0
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	target := BlacklistTargetFor("claude", Provider{ID: 1, Name: "Bad"})
	for i := 0; i < 2; i++ {
		if err := bs.recordFailureFor(target); err != nil {
			t.Fatalf("第 %d 次记录失败失败: %v", i+1, err)
		}
	}

	if got := failureCountFor(t, "claude", "Bad"); got != 2 {
		t.Errorf("窗口为 0 时每次都应计数，实际 failure_count=%d", got)
	}
}
