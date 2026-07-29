package services

import (
	"testing"
	"time"
)

// P1 缓存的核心契约：写路径必须让缓存失效，读到的状态始终与库一致。

func newBlacklistCacheTestService(t *testing.T, threshold int) *BlacklistService {
	t.Helper()
	ss := setupSettingsTestDB(t)
	notifications := NewNotificationService(NewAppSettingsService(nil))
	bs := NewBlacklistService(ss, notifications)

	if err := ss.UpdateBlacklistEnabled(true); err != nil {
		t.Fatalf("打开拉黑总开关失败: %v", err)
	}
	config := DefaultBlacklistLevelConfig()
	config.EnableLevelBlacklist = false
	config.FallbackMode = "fixed"
	config.FailureThreshold = threshold
	config.DedupeWindowSeconds = 0
	if err := ss.SaveBlacklistLevelConfig(config); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	return bs
}

// 缓存了"未拉黑"之后记失败到阈值，必须立即读到已拉黑（RecordFailure 失效缓存）
func TestBlacklistCacheInvalidatedByFailure(t *testing.T) {
	bs := newBlacklistCacheTestService(t, 1)
	target := BlacklistTargetFor("claude", Provider{ID: 11, Name: "CacheProbe"})

	if blacklisted, _ := bs.isBlacklistedFor(target); blacklisted {
		t.Fatal("初始不应拉黑")
	}
	// 此时缓存里是"未拉黑"
	if err := bs.recordFailureFor(target); err != nil {
		t.Fatalf("记录失败失败: %v", err)
	}
	blacklisted, until := bs.isBlacklistedFor(target)
	if !blacklisted {
		t.Fatal("失败达到阈值后应立即读到已拉黑（缓存未失效）")
	}
	if until == nil || !until.After(time.Now()) {
		t.Fatalf("拉黑截止时间应在未来: %v", until)
	}
	// 命中缓存的第二次读也必须一致
	if blacklisted, _ := bs.isBlacklistedFor(target); !blacklisted {
		t.Fatal("缓存命中的读取结果与首读不一致")
	}
}

// 手动解禁后必须立即读到未拉黑（ManualUnblockAndReset 失效缓存）
func TestBlacklistCacheInvalidatedByManualUnblock(t *testing.T) {
	bs := newBlacklistCacheTestService(t, 1)
	target := BlacklistTargetFor("claude", Provider{ID: 12, Name: "CacheUnblock"})

	if err := bs.recordFailureFor(target); err != nil {
		t.Fatalf("记录失败失败: %v", err)
	}
	if blacklisted, _ := bs.isBlacklistedFor(target); !blacklisted {
		t.Fatal("应已拉黑")
	}
	// 此时缓存里是"已拉黑"
	if err := bs.ManualUnblockAndReset("claude", "CacheUnblock"); err != nil {
		t.Fatalf("手动解禁失败: %v", err)
	}
	if blacklisted, _ := bs.isBlacklistedFor(target); blacklisted {
		t.Fatal("手动解禁后应立即读到未拉黑（缓存未失效）")
	}
}

// 成功记账清零后必须立即读到未拉黑（RecordSuccess 失效缓存）
func TestBlacklistCacheInvalidatedBySuccess(t *testing.T) {
	bs := newBlacklistCacheTestService(t, 2)
	target := BlacklistTargetFor("claude", Provider{ID: 13, Name: "CacheSuccess"})

	if err := bs.recordFailureFor(target); err != nil {
		t.Fatalf("记录失败失败: %v", err)
	}
	if blacklisted, _ := bs.isBlacklistedFor(target); blacklisted {
		t.Fatal("未达阈值不应拉黑")
	}
	if err := bs.recordSuccessFor(target); err != nil {
		t.Fatalf("记录成功失败: %v", err)
	}
	// 成功清零后再失败一次仍未达阈值
	if err := bs.recordFailureFor(target); err != nil {
		t.Fatalf("记录失败失败: %v", err)
	}
	if blacklisted, _ := bs.isBlacklistedFor(target); blacklisted {
		t.Fatal("成功清零后单次失败不应拉黑（缓存或计数未失效）")
	}
}

// 缓存键必须区分不同定位方式：按 ID 与按名字的目标互不串扰
func TestBlacklistCacheKeyDistinguishesTargets(t *testing.T) {
	byID := BlacklistTarget{platform: "claude", providerID: 7, name: "Same"}
	byName := BlacklistTarget{platform: "claude", name: "Same"}
	if byID.cacheKey() == byName.cacheKey() {
		t.Fatalf("ID 定位与名字定位的缓存键不应相同: %s", byID.cacheKey())
	}
	custom := BlacklistTarget{platform: "custom", sourceID: "tool-a", providerID: 7, name: "Same"}
	if custom.cacheKey() == byID.cacheKey() {
		t.Fatalf("不同平台/工具的缓存键不应相同: %s", custom.cacheKey())
	}
}
