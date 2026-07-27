package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daodao97/xgo/xdb"
)

// aliasTTL 定义 rename 后旧名保留时长,必须 > in-flight 请求上限(32h)并留 buffer。
const aliasTTL = 48 * time.Hour

var providerAliasLookupEnabled atomic.Bool

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
	data                         []byte
}

func resolveProviderDataScope(kind string) (providerDataScope, error) {
	platform, err := resolvePlatform(kind)
	if err != nil {
		return providerDataScope{}, err
	}
	scope := providerDataScope{identityPlatform: platform, telemetryPlatform: platform}
	if strings.HasPrefix(platform, "custom:") {
		scope.telemetryPlatform = "custom"
		scope.sourceID = strings.TrimPrefix(platform, "custom:")
	}
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
	if err := ensureRequestLogTable(); err != nil {
		return fmt.Errorf("初始化请求统计表失败: %w", err)
	}

	// 清理过期 alias(MVP:不起后台 job,借 rename 顺手 GC)
	if err := cleanupExpiredAliases(); err != nil {
		return fmt.Errorf("清理过期 alias 失败: %w", err)
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

	// 校验 alias 表内是否被占用 + 该 provider_id 48h 内是否已 rename
	if err := checkAliasConstraints(scope.identityPlatform, id, newName); err != nil {
		return err
	}

	originalProviders := append([]Provider(nil), providers...)

	// 更新内存中的 provider.Name,序列化新配置
	target.Name = newName
	newBytes, err := serializeProviders(providers)
	if err != nil {
		return fmt.Errorf("序列化新配置失败: %w", err)
	}
	return ps.commitProviderRenameLocked(providerRenameCommit{
		kind: kind, scope: scope, providerID: id, oldName: oldName, newName: newName,
		originalProviders: originalProviders, providers: providers, data: newBytes,
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
	if err := ensureRequestLogTable(); err != nil {
		return fmt.Errorf("初始化请求统计表失败: %w", err)
	}
	if err := cleanupExpiredAliases(); err != nil {
		return fmt.Errorf("清理过期 alias 失败: %w", err)
	}
	if err := checkAliasConstraints(scope.identityPlatform, providerID, nextProvider.Name); err != nil {
		return err
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
		originalProviders: existingProviders, providers: plan.providers, data: plan.data,
	})
}

func (ps *ProviderService) commitProviderRenameLocked(commit providerRenameCommit) error {
	path, err := providerFilePath(commit.kind)
	if err != nil {
		return err
	}
	originalBytes, err := serializeProviders(commit.originalProviders)
	if err != nil {
		return fmt.Errorf("序列化原配置失败: %w", err)
	}
	if err := atomicWriteFile(path, commit.data, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	rollbackFile := func(primary error) error {
		if rollbackErr := atomicWriteFile(path, originalBytes, 0o644); rollbackErr != nil {
			log.Printf("[ProviderRename] CRITICAL 回滚配置文件失败 path=%s primary=%v rollback=%v", path, primary, rollbackErr)
			return fmt.Errorf("%w; 配置文件回滚失败: %v", primary, rollbackErr)
		}
		return primary
	}

	db, err := xdb.DB("default")
	if err != nil {
		return rollbackFile(fmt.Errorf("获取数据库连接失败: %w", err))
	}
	tx, err := db.Begin()
	if err != nil {
		return rollbackFile(fmt.Errorf("开启事务失败: %w", err))
	}
	if err := doRenameTx(tx, commit.scope, commit.providerID, commit.oldName, commit.newName); err != nil {
		_ = tx.Rollback()
		return rollbackFile(fmt.Errorf("更新历史数据失败: %w", err))
	}

	piSynced := false
	if strings.EqualFold(strings.TrimSpace(commit.kind), "pi") && ps.piGatewaySync != nil {
		if err := ps.piGatewaySync(commit.providers); err != nil {
			_ = tx.Rollback()
			primary := fmt.Errorf("同步 Pi models.json 失败: %w", err)
			if syncErr := ps.piGatewaySync(commit.originalProviders); syncErr != nil {
				primary = fmt.Errorf("%w; Pi gateway 回滚失败: %v", primary, syncErr)
			}
			return rollbackFile(primary)
		}
		piSynced = true
	}
	if err := tx.Commit(); err != nil {
		primary := fmt.Errorf("提交事务失败: %w", err)
		if piSynced {
			if syncErr := ps.piGatewaySync(commit.originalProviders); syncErr != nil {
				primary = fmt.Errorf("%w; Pi gateway 回滚失败: %v", primary, syncErr)
			}
		}
		return rollbackFile(primary)
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
// request_log.provider / provider_blacklist.provider_name / health_check_history + 写 alias。
func doRenameTx(tx *sql.Tx, scope providerDataScope, providerID int64, oldName, newName string) error {
	if err := updateRequestLogProviderNameTx(tx, scope, oldName, newName); err != nil {
		return fmt.Errorf("更新 request_log 失败: %w", err)
	}
	if err := updateRelayAttemptProviderNameTx(tx, scope, oldName, newName); err != nil {
		return fmt.Errorf("更新 relay_attempt 失败: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE provider_blacklist SET provider_name = ? WHERE platform = ? AND provider_name = ?`,
		newName, scope.identityPlatform, oldName,
	); err != nil {
		return fmt.Errorf("更新 provider_blacklist 失败: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE health_check_history SET provider_name = ? WHERE platform = ? AND provider_id = ?`,
		newName, scope.identityPlatform, providerID,
	); err != nil {
		return fmt.Errorf("更新 health_check_history 失败: %w", err)
	}

	expiresAt := time.Now().Add(aliasTTL).UTC().Format("2006-01-02 15:04:05")
	if _, err := tx.Exec(
		`INSERT INTO provider_alias (platform, provider_id, alias_name, canonical_name, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		scope.identityPlatform, providerID, oldName, newName, expiresAt,
	); err != nil {
		return fmt.Errorf("写入 alias 失败: %w", err)
	}
	providerAliasLookupEnabled.Store(true)

	return nil
}

func updateRequestLogProviderNameTx(tx *sql.Tx, scope providerDataScope, oldName, newName string) error {
	if scope.sourceID == "" {
		_, err := tx.Exec(`UPDATE request_log SET provider = ? WHERE platform = ? AND provider = ?`, newName, scope.identityPlatform, oldName)
		return err
	}
	_, err := tx.Exec(
		`UPDATE request_log SET provider = ?
		 WHERE provider = ? AND (platform = ? OR (platform = ? AND source_id = ?))`,
		newName, oldName, scope.identityPlatform, scope.telemetryPlatform, scope.sourceID,
	)
	return err
}

func updateRelayAttemptProviderNameTx(tx *sql.Tx, scope providerDataScope, oldName, newName string) error {
	if scope.sourceID == "" {
		_, err := tx.Exec(`UPDATE relay_attempt SET provider = ? WHERE platform = ? AND provider = ?`, newName, scope.identityPlatform, oldName)
		return err
	}
	_, err := tx.Exec(
		`UPDATE relay_attempt SET provider = ?
		 WHERE provider = ? AND (platform = ? OR (platform = ? AND source_id = ?))`,
		newName, oldName, scope.identityPlatform, scope.telemetryPlatform, scope.sourceID,
	)
	return err
}

// checkAliasConstraints 校验 alias 表层面的约束:
//   - newName 未被 48h 内其它 alias 占用
//   - 该 provider_id 48h 内没有产生过 alias(禁止链式 rename)
func checkAliasConstraints(platform string, providerID int64, newName string) error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	var occupied int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM provider_alias
		 WHERE platform = ? AND alias_name = ? AND expires_at > CURRENT_TIMESTAMP`,
		platform, newName,
	).Scan(&occupied)
	if err != nil {
		return fmt.Errorf("查询 alias 占用失败: %w", err)
	}
	if occupied > 0 {
		return fmt.Errorf("新名字 %q 在 48h 内被历史别名占用,无法使用", newName)
	}

	var chained int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM provider_alias
		 WHERE platform = ? AND provider_id = ? AND expires_at > CURRENT_TIMESTAMP`,
		platform, providerID,
	).Scan(&chained)
	if err != nil {
		return fmt.Errorf("查询链式 rename 失败: %w", err)
	}
	if chained > 0 {
		return fmt.Errorf("该 provider 48h 内已改过名,请等 alias 过期后再操作")
	}

	return nil
}

// checkNameNotOccupiedByAlias 校验 `name` 未被其它 provider 的 48h 活动 alias 占用。
// 用于 SaveProviders 新建/更新时阻止"复用旧名新建 provider 污染历史"。
// providerID 为当前被保存的 provider id;如果 alias 的 provider_id 等于它本身,则不算冲突
// (意味着是该 provider 自己的老别名,canonical_name 仍指向它自己,不会误归并)。
func checkNameNotOccupiedByAlias(platform string, providerID int64, name string) error {
	if name == "" {
		return nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	var owner int64
	err = db.QueryRow(
		`SELECT provider_id FROM provider_alias
		 WHERE platform = ? AND alias_name = ? AND expires_at > CURRENT_TIMESTAMP
		 LIMIT 1`,
		platform, name,
	).Scan(&owner)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("查询 alias 占用失败: %w", err)
	}
	if owner != providerID {
		log.Printf("[Provider] 名字 %q 被其他 provider(id=%d)的 48h 活动别名占用,拒绝保存", name, owner)
		return fmt.Errorf("名字 %q 被其他供应商的历史别名暂时占用(48h 内),请换个名字或等待过期", name)
	}
	return nil
}

// cleanupExpiredAliases 删除已过期的 alias 记录。
func cleanupExpiredAliases() error {
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	if _, err = db.Exec(`DELETE FROM provider_alias WHERE expires_at <= CURRENT_TIMESTAMP`); err != nil {
		return err
	}
	return refreshProviderAliasLookupEnabled(db)
}

func refreshProviderAliasLookupEnabled(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM provider_alias WHERE expires_at > CURRENT_TIMESTAMP`).Scan(&count); err != nil {
		return err
	}
	providerAliasLookupEnabled.Store(count > 0)
	return nil
}

// ResolveProviderAlias 将旧名翻译为当前 canonical name(未过期),找不到返回原名。
// 只做 1 跳查询,由 RenameProvider 的链式拒绝约束保证不会出现多层 alias。
func ResolveProviderAlias(platform, name string) string {
	if name == "" || !providerAliasLookupEnabled.Load() {
		return name
	}
	db, err := xdb.DB("default")
	if err != nil {
		return name
	}
	var canonical string
	err = db.QueryRow(
		`SELECT canonical_name FROM provider_alias
		 WHERE platform = ? AND alias_name = ? AND expires_at > CURRENT_TIMESTAMP
		 LIMIT 1`,
		platform, name,
	).Scan(&canonical)
	if err != nil || canonical == "" {
		return name
	}
	return canonical
}

// resolvePlatform 把 kind 归一到 DB 使用的 platform 值(与 request_log/blacklist 一致)。
func resolvePlatform(kind string) (string, error) {
	trimmed := strings.TrimSpace(kind)
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case "claude", "claude-code", "claude_code":
		return "claude", nil
	case "codex":
		return "codex", nil
	case "gemini":
		return "gemini", nil
	case "reasonix":
		return "reasonix", nil
	case "pi":
		return "pi", nil
	default:
		if strings.HasPrefix(normalized, "custom:") && len(normalized) > len("custom:") {
			return "custom:" + strings.TrimSpace(trimmed[len("custom:"):]), nil
		}
		return "", fmt.Errorf("不支持的 provider kind: %s", kind)
	}
}

// serializeProviders 按 saveProvidersLocked 相同的 MarshalIndent 格式输出。
func serializeProviders(providers []Provider) ([]byte, error) {
	return json.MarshalIndent(providerEnvelope{Providers: providers}, "", "  ")
}
