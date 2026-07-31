package services

import (
	"context"
	"strings"
)

// 黑名单的定位方式：优先 provider_id，回退 name。
//
// 原本只按 (platform, provider_name) 定位，这带来两个问题：
//  1. 改名必须 UPDATE provider_blacklist
//  2. 改名瞬间仍在进行的请求（流式上限 32 小时）失败时带的是旧名字，
//     查不到已改名的那一行，于是插入第二条黑名单行——
//     失败计数被拆成两份，拉黑阈值永远达不到
//
// provider_alias 表当初就是为第 2 点存在的（把旧名映射回新名）。
// 调用方本来就持有 Provider，把 ID 直接传进来就不需要这层映射了。

// BlacklistTarget 标识一个黑名单条目
type BlacklistTarget struct {
	// platform 规范化平台 ID
	platform string
	// sourceID 保留为数据库结构字段，当前注册平台均为空
	sourceID string
	// providerID 关联 provider 表；0 表示未知（供应商已被删除，
	// 或调用方只拿到名字——如 UI 手动解除拉黑）
	providerID int64
	// name 供应商名，用于 providerID 为 0 时回退，以及新建行时写入
	name string
}

// BlacklistTargetFor 由 relay 的 scope 与 Provider 构造定位信息。
//
// Gemini 不再需要单独的构造函数：它的转发循环已改用统一 Provider 类型
// （A3 阶段 1），走 BlacklistTargetFor("gemini", provider) 即可。
func BlacklistTargetFor(relayScope string, provider Provider) BlacklistTarget {
	target := BlacklistTarget{name: provider.Name, providerID: provider.ID}
	normalized := strings.ToLower(strings.TrimSpace(relayScope))
	if id := resolvePlatformID(normalized); id != "" {
		target.platform = id
		return target
	}
	target.platform = normalized
	return target
}

// BlacklistTargetByName 仅有名字时的定位（Gemini，以及手动解除拉黑等 UI 入口）。
//
// 会尝试从 provider 表解析 ID：解析得到就按 ID 定位，
// 解析不到则回退按名字（供应商已删除，或 Gemini 这类未并入 provider 表的平台）。
func BlacklistTargetByName(relayScope string, providerName string) BlacklistTarget {
	target := BlacklistTarget{name: strings.TrimSpace(providerName)}
	normalized := strings.ToLower(strings.TrimSpace(relayScope))
	if id := resolvePlatformID(normalized); id != "" {
		target.platform = id
	} else {
		target.platform = normalized
	}

	if target.name == "" || target.platform == "" {
		return target
	}
	providers, err := loadProvidersFromDB(context.Background(), providerScope{
		platform: target.platform, sourceID: target.sourceID,
	})
	if err != nil {
		return target
	}
	for _, provider := range providers {
		if strings.EqualFold(strings.TrimSpace(provider.Name), target.name) {
			target.providerID = provider.ID
			break
		}
	}
	return target
}

// locator 返回定位用的 SQL 条件与参数。
//
// 有 ID 时按 (platform, source_id, provider_id) 定位——名字变了也能找到同一行。
// 无 ID 时回退 (platform, source_id, provider_name)。
func (t BlacklistTarget) locator() (string, []any) {
	if t.providerID != 0 {
		return "platform = ? AND source_id = ? AND provider_id = ?",
			[]any{t.platform, t.sourceID, t.providerID}
	}
	return "platform = ? AND source_id = ? AND provider_name = ?",
		[]any{t.platform, t.sourceID, t.name}
}

// nullableID 供插入时使用，0 转为 NULL
func (t BlacklistTarget) nullableID() any {
	return nullableProviderID(t.providerID)
}

// Platform 返回规范化平台 ID（relay 测试按定位字段查库用）
func (t BlacklistTarget) Platform() string { return t.platform }

// SourceID 返回数据库定位字段，当前注册平台为空。
func (t BlacklistTarget) SourceID() string { return t.sourceID }
