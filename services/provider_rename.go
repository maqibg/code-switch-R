package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type providerDataScope struct {
	identityPlatform  string
	telemetryPlatform string
	sourceID          string
}

type providerRenameCommit struct {
	kind                         string
	scope                        providerDataScope
	providerID                   int64
	oldName, newName             string
	originalProviders, providers []Provider
}

func resolveProviderDataScope(kind string) (providerDataScope, error) {
	platform, err := resolvePlatform(kind)
	if err != nil {
		return providerDataScope{}, err
	}
	scope := providerDataScope{identityPlatform: platform, telemetryPlatform: platform}
	return scope, nil
}

// RenameProvider 改名 provider:事务更新 DB 中按 name 存储的历史数据,
// 写入 48h alias 兜底 in-flight 请求,最后原子替换配置文件。
//
// 校验规则:
//   - newName 非空且 trim 后与 oldName 不等
//   - 同 kind 下不存在其它 provider 用同名(当前 snapshot)
//   - 48h 内 alias 表未占用该 newName
//   - 该 provider_id 在 48h 内未 rename 过(禁止链式)
func (ps *ProviderService) RenameProvider(kind string, id int64, newName string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("新名字不能为空")
	}

	scope, err := resolveProviderDataScope(kind)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(kind), "pi") && strings.Contains(newName, "/") {
		return fmt.Errorf("Pi Provider 名称不能包含 '/'")
	}
	// 幂等兜底：正常启动已由 InitDatabase 跑过迁移。
	// 早先这里只 ensure 了 request_log，而 rename 事务还要更新其他表，
	// 于是全新安装（从未删除过 provider）首次改名必失败。现在统一走迁移。
	if err := RunMigrations(); err != nil {
		return fmt.Errorf("应用数据库迁移失败: %w", err)
	}

	// 加载当前配置(原样读,不触发迁移保存)
	providers, err := ps.loadProvidersRaw(kind)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}

	var target *Provider
	for i := range providers {
		if providers[i].ID == id {
			target = &providers[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("未找到 id=%d 的 provider", id)
	}
	oldName := target.Name

	if strings.EqualFold(strings.TrimSpace(oldName), newName) {
		return fmt.Errorf("新名字与旧名字相同")
	}

	// 校验当前 snapshot 内同 kind 不冲突
	for i := range providers {
		if providers[i].ID == id {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(providers[i].Name), newName) {
			return fmt.Errorf("同 kind 下已存在名为 %q 的 provider", newName)
		}
	}

	// 不再需要 alias 层面的校验：日志与黑名单都按 provider_id 关联，
	// 改名不产生"旧名残留"，因此也不再禁止链式改名。
	originalProviders := append([]Provider(nil), providers...)

	// 更新内存中的 provider.Name（写入由 commitProviderRenameLocked 落到 provider 表）
	target.Name = newName
	return ps.commitProviderRenameLocked(providerRenameCommit{
		kind: kind, scope: scope, providerID: id, oldName: oldName, newName: newName,
		originalProviders: originalProviders, providers: providers,
	})
}

// SaveProvidersWithRename 原子保存一次编辑中的字段更新和名称变更。
// 该入口只允许更新现有 provider 集合，新增和删除仍使用 SaveProviders。
func (ps *ProviderService) SaveProvidersWithRename(kind string, providerID int64, providers []Provider) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	existingProviders, err := ps.loadProvidersRaw(kind)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}

	var oldProvider *Provider
	for i := range existingProviders {
		if existingProviders[i].ID == providerID {
			oldProvider = &existingProviders[i]
			break
		}
	}
	if oldProvider == nil {
		return fmt.Errorf("未找到 id=%d 的 provider", providerID)
	}

	var nextProvider *Provider
	for i := range providers {
		if providers[i].ID == providerID {
			nextProvider = &providers[i]
			break
		}
	}
	if nextProvider == nil {
		return fmt.Errorf("待保存配置中缺少 id=%d 的 provider", providerID)
	}
	nextProvider.Name = strings.TrimSpace(nextProvider.Name)
	if nextProvider.Name == "" {
		return fmt.Errorf("新名字不能为空")
	}
	if err := validateProviderSetUnchanged(existingProviders, providers); err != nil {
		return err
	}
	if oldProvider.Name == nextProvider.Name {
		return ps.saveProvidersLocked(kind, providers)
	}

	scope, err := resolveProviderDataScope(kind)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(kind), "pi") && strings.Contains(nextProvider.Name, "/") {
		return fmt.Errorf("Pi Provider 名称不能包含 '/'")
	}
	for _, provider := range providers {
		if provider.ID != providerID && strings.EqualFold(strings.TrimSpace(provider.Name), nextProvider.Name) {
			return fmt.Errorf("同 kind 下已存在名为 %q 的 provider", nextProvider.Name)
		}
	}
	if err := RunMigrations(); err != nil {
		return fmt.Errorf("应用数据库迁移失败: %w", err)
	}

	plan, err := prepareProviderSave(kind, providers, existingProviders, providerID)
	if err != nil {
		return err
	}
	if len(plan.deletedProviders) != 0 {
		return fmt.Errorf("原子改名保存不能同时删除 provider")
	}

	return ps.commitProviderRenameLocked(providerRenameCommit{
		kind: kind, scope: scope, providerID: providerID,
		oldName: oldProvider.Name, newName: nextProvider.Name,
		originalProviders: existingProviders, providers: plan.providers,
	})
}

// commitProviderRenameLocked 提交改名。
//
// 主数据入库后（A1），provider 表与历史数据表在同一个事务里更新，
// 不再需要「先写 JSON 文件、失败时补偿回写」那一套：
// 原实现在写完文件、提交事务之前崩溃会永久不一致，且补偿本身可能失败
// （只能打 CRITICAL 日志）。现在这个窗口不存在。
//
// Pi 网关同步仍需补偿：models.json 是本进程之外的文件，
// 无法与数据库事务原子提交。
func (ps *ProviderService) commitProviderRenameLocked(commit providerRenameCommit) error {
	ctx := context.Background()

	// provider 表与历史数据表的 scope 必须统一走 scopeForKind。
	repoScope, err := scopeForKind(commit.kind)
	if err != nil {
		return err
	}

	isPi := strings.EqualFold(strings.TrimSpace(commit.kind), "pi") && ps.piGatewaySync != nil

	piSyncAttempted := false

	err = dbExecInImmediateTx(ctx, func(tx dbTxExecutor) error {
		// 写入整份 provider 列表，而不是只改 name：
		// SaveProvidersWithRename 允许在改名的同时修改其它字段（APIURL、模型映射等），
		// 只更新 name 会把这些改动丢掉。
		found := false
		for order, provider := range commit.providers {
			if provider.ID == commit.providerID {
				found = true
			}
			if err := upsertProviderInTx(ctx, tx, repoScope, provider, order); err != nil {
				return err
			}
		}
		if !found {
			return fmt.Errorf("未找到 id=%d 的 provider", commit.providerID)
		}

		// 历史数据表的名字与 alias
		if err := doRenameTxCtx(ctx, tx, commit.scope, commit.providerID, commit.oldName, commit.newName); err != nil {
			return fmt.Errorf("更新历史数据失败: %w", err)
		}

		// Pi 网关同步放在提交之前：models.json 是进程外的文件，无法与事务原子提交，
		// 但放在这里可以让同步失败连带回滚 provider 改名、历史数据和 alias。
		// 若放在提交之后，同步失败时这三者已经落库，只能靠补偿逐个回退。
		if isPi {
			piSyncAttempted = true
			if syncErr := ps.piGatewaySync(commit.providers); syncErr != nil {
				return fmt.Errorf("同步 Pi models.json 失败: %w", syncErr)
			}
		}
		return nil
	})
	if err != nil {
		// 事务已回滚。只要尝试过同步就要恢复 models.json——
		// 同步可能写到一半才失败，此时文件内容已经变了。
		if piSyncAttempted {
			if syncErr := ps.piGatewaySync(commit.originalProviders); syncErr != nil {
				return fmt.Errorf("%w; Pi gateway 回滚失败: %v", err, syncErr)
			}
		}
		return err
	}
	return nil
}

func validateProviderSetUnchanged(existingProviders, providers []Provider) error {
	if len(existingProviders) != len(providers) {
		return fmt.Errorf("原子改名保存不能同时新增或删除 provider")
	}
	existingIDs := make(map[int64]struct{}, len(existingProviders))
	nextIDs := make(map[int64]struct{}, len(providers))
	for _, provider := range existingProviders {
		existingIDs[provider.ID] = struct{}{}
	}
	for _, provider := range providers {
		if _, ok := existingIDs[provider.ID]; !ok {
			return fmt.Errorf("原子改名保存不能同时新增 provider id=%d", provider.ID)
		}
		nextIDs[provider.ID] = struct{}{}
	}
	for providerID := range existingIDs {
		if _, ok := nextIDs[providerID]; !ok {
			return fmt.Errorf("原子改名保存不能同时删除 provider id=%d", providerID)
		}
	}
	return nil
}

// doRenameTx 在 tx 内完成 DB 侧所有改动:
// doRenameTxCtx 在事务内完成历史数据侧的改名。
//
// 接受 dbTxExecutor（*sql.Conn 与 *sql.Tx 均满足），
// 以便与 BEGIN IMMEDIATE 事务配合使用。
func doRenameTxCtx(ctx context.Context, tx dbTxExecutor, scope providerDataScope, providerID int64, oldName, newName string) error {
	// request_log / relay_attempt 的 provider 名。
	// 注：迁移 v3 之后这两张表已有 provider_id，按 ID 关联的部分无需改动；
	// 这里更新 name 列是为了让历史记录显示改名后的名字，
	// 与 A1 第 4 步删除 alias 时一并重新评估。
	if scope.sourceID == "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE request_log SET provider = ? WHERE platform = ? AND provider = ?`,
			newName, scope.identityPlatform, oldName,
		); err != nil {
			return fmt.Errorf("更新 request_log 失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE relay_attempt SET provider = ? WHERE platform = ? AND provider = ?`,
			newName, scope.identityPlatform, oldName,
		); err != nil {
			return fmt.Errorf("更新 relay_attempt 失败: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE request_log SET provider = ?
			 WHERE provider = ? AND platform = ? AND source_id = ?`,
			newName, oldName, scope.telemetryPlatform, scope.sourceID,
		); err != nil {
			return fmt.Errorf("更新 request_log 失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE relay_attempt SET provider = ?
			 WHERE provider = ? AND platform = ? AND source_id = ?`,
			newName, oldName, scope.telemetryPlatform, scope.sourceID,
		); err != nil {
			return fmt.Errorf("更新 relay_attempt 失败: %w", err)
		}
	}

	// 黑名单按 provider_id 定位（迁移 v4），这里同步名字只为让展示一致。
	// 即使这次更新漏掉某行（例如它的 provider_id 为 NULL 的历史行），
	// 也不会影响失败计数的正确性。
	if _, err := tx.ExecContext(ctx,
		`UPDATE provider_blacklist SET provider_name = ? WHERE platform = ? AND provider_name = ?`,
		newName, scope.identityPlatform, oldName,
	); err != nil {
		return fmt.Errorf("更新 provider_blacklist 失败: %w", err)
	}

	// 不再写 provider_alias：日志与黑名单都按 provider_id 关联，
	// 改名瞬间 in-flight 的写入靠 ID 落到同一行，不需要旧名映射表。
	return nil
}

// resolvePlatform 把 kind 归一到 DB 使用的 platform 值(与 request_log/blacklist 一致)。
//
// 别名匹配统一交给 platform_registry.go 的 resolvePlatformID，
// 不再在这里重复维护一份别名列表——之前 claude 的三种写法在多个文件里各写一遍，
// 新增平台时漏改任何一处都会让同一平台在日志和黑名单里出现两个不同的 scope key。
//
// gemini 有意不进 provider 注册表，尽管它的数据已在 provider 表里：
// 注册表的 ProviderFile 驱动迁移 v2 的 JSON 导入，而 Gemini 的导入由
// 迁移 v6 单独负责——它要处理 string ID → int64 主键的映射，
// 且 gemini-providers.json 是裸数组，不是 v2 期望的 envelope 格式。
// 加进注册表会让 v2 先把文件改名 .migrated，v6 再也读不到，legacyId 全丢。
// platform 值仍是 "gemini"，所以这里单独放行。
func resolvePlatform(kind string) (string, error) {
	trimmed := strings.TrimSpace(kind)
	normalized := strings.ToLower(trimmed)

	if id := resolvePlatformID(normalized); id != "" {
		return id, nil
	}
	if normalized == "gemini" {
		return "gemini", nil
	}
	if normalized == openCodePlatform {
		return openCodePlatform, nil
	}
	return "", fmt.Errorf("不支持的 provider kind: %s", kind)
}

// serializeProviders 按 saveProvidersLocked 相同的 MarshalIndent 格式输出。
func serializeProviders(providers []Provider) ([]byte, error) {
	return json.MarshalIndent(providerEnvelope{Providers: providers}, "", "  ")
}
