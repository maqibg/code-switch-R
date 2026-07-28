package services

import (
	"context"
	"strings"

	"github.com/daodao97/xgo/xdb"
)

// 日志查询的供应商筛选。
//
// 前端传的是供应商名字（下拉框里显示的就是名字），但日志表里的 name 列
// 记录的是"请求发生当时"的名字。改名会同步更新这一列，所以按 name 筛选
// 通常是对的——除了一个窗口：改名瞬间仍在进行的请求（流式上限 32 小时），
// 它们的遥测在改名之后才落库，带的还是旧名字，按新名筛选就会漏掉。
//
// provider_alias 表当初就是为遮掩这个窗口而存在的。现在日志行带 provider_id，
// 把筛选改成按 ID 匹配就从根上绕开了它，alias 随之可以删除。

// logProviderFilter 描述一次供应商筛选的匹配方式
type logProviderFilter struct {
	// name 前端传入的原始名字，始终保留（用于回退与错误信息）
	name string
	// providerID 名字在 provider 表里对应的 ID；0 表示解析不到
	providerID int64
}

// resolveLogProviderFilter 把供应商名解析成筛选条件。
//
// 解析不到 ID 时回退按 name 匹配：这覆盖的是已删除供应商的历史记录——
// 它们的 provider_id 是 NULL，只能靠 name 找回来。
func resolveLogProviderFilter(platform, sourceID, providerName string) logProviderFilter {
	filter := logProviderFilter{name: strings.TrimSpace(providerName)}
	if filter.name == "" {
		return filter
	}

	scope := providerScope{platform: strings.TrimSpace(platform), sourceID: strings.TrimSpace(sourceID)}
	if scope.platform == "" {
		// 未指定平台时无法定位范围，只能按名字匹配
		return filter
	}

	providers, err := loadProvidersFromDB(context.Background(), scope)
	if err != nil {
		// 查询失败不应让筛选整体失败，退回按名字匹配
		logWarn("解析供应商筛选条件失败，回退按名字匹配",
			"platform", scope.platform, "provider", filter.name, "error", err)
		return filter
	}
	for _, provider := range providers {
		if strings.EqualFold(strings.TrimSpace(provider.Name), filter.name) {
			filter.providerID = provider.ID
			break
		}
	}
	return filter
}

// empty 是否未指定筛选
func (f logProviderFilter) empty() bool {
	return f.name == ""
}

// xdbOption 返回筛选用的 xdb 查询条件。
//
// 语义与 sqlCondition 一致，只是给走 xdb 构造器的查询路径使用。
// 用 WhereGroup 嵌套而不是拼字符串：provider 名来自用户输入，
// 必须走参数绑定。
func (f logProviderFilter) xdbOption() (xdb.Option, bool) {
	if f.empty() {
		return nil, false
	}
	if f.providerID == 0 {
		return xdb.WhereEq("provider", f.name), true
	}
	return xdb.WhereGroup(
		xdb.WhereEq("provider_id", f.providerID),
		xdb.WhereOrGroup(
			xdb.WhereIsNil("provider_id"),
			xdb.WhereEq("provider", f.name),
		),
	), true
}

// sqlCondition 返回筛选用的 SQL 片段与参数。
//
// 有 ID 时匹配 (provider_id = ? OR (provider_id IS NULL AND provider = ?))：
//   - provider_id 命中当前供应商的全部记录，无论记录里存的是哪个历史名字
//   - provider_id 为 NULL 的旧记录（迁移前写入且回填时匹配不上的）继续按名字兜底
//
// 无 ID 时只能按名字匹配（供应商已删除）。
func (f logProviderFilter) sqlCondition() (string, []any) {
	if f.empty() {
		return "", nil
	}
	if f.providerID == 0 {
		return "provider = ?", []any{f.name}
	}
	return "(provider_id = ? OR (provider_id IS NULL AND provider = ?))", []any{f.providerID, f.name}
}
