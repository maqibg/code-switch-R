package services

import (
	"strconv"
	"sync"
	"time"
)

// 黑名单读缓存（P1）。
//
// isBlacklistedFor 在每次请求的候选过滤阶段被逐 provider 调用，
// 原实现每次直查 SQLite，单请求 N 次 DB 往返。
// 缓存的值是该目标的 blacklisted_until（nil = 确认无拉黑行），
// 到期判断在读取时用缓存值做时间比较，因此过期不需要失效；
// 只有写路径（成败记账、手动解禁、自动恢复）会改变数据，写入低频，
// 统一整表清空，简单且必然正确。
//
// 已知边界：provider 删除的清理 SQL 不经过 BlacklistService，
// 但被删供应商不再出现在候选列表里，其残留条目不会被读到，
// 且 provider ID 是自增主键不会复用；改名不改 ID，按 ID 的缓存键不变，
// 拉黑状态也不随改名变化。
type blacklistCache struct {
	mu      sync.RWMutex
	entries map[string]*time.Time
}

func newBlacklistCache() *blacklistCache {
	return &blacklistCache{entries: map[string]*time.Time{}}
}

func (c *blacklistCache) get(key string) (*time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	until, ok := c.entries[key]
	return until, ok
}

func (c *blacklistCache) set(key string, until *time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = until
}

func (c *blacklistCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*time.Time{}
}

// cacheKey 与 locator() 的定位语义一一对应：
// 有 provider_id 按 (platform, source_id, provider_id)，否则按名字回退。
func (t BlacklistTarget) cacheKey() string {
	if t.providerID != 0 {
		return t.platform + "|" + t.sourceID + "|id:" + strconv.FormatInt(t.providerID, 10)
	}
	return t.platform + "|" + t.sourceID + "|name:" + t.name
}
