package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/daodao97/xgo/xdb"
)

type deletedProvider struct {
	ID   int64
	Name string
}

func diffDeletedProviders(existing, next []Provider) []deletedProvider {
	nextIDs := make(map[int64]struct{}, len(next))
	nextNames := make(map[string]struct{}, len(next))
	for _, p := range next {
		if p.ID != 0 {
			nextIDs[p.ID] = struct{}{}
			continue
		}
		nextNames[p.Name] = struct{}{}
	}

	deleted := make([]deletedProvider, 0)
	for _, p := range existing {
		if p.ID != 0 {
			if _, ok := nextIDs[p.ID]; !ok {
				deleted = append(deleted, deletedProvider{ID: p.ID, Name: p.Name})
			}
			continue
		}
		if _, ok := nextNames[p.Name]; !ok {
			deleted = append(deleted, deletedProvider{Name: p.Name})
		}
	}
	return deleted
}

func cleanupDeletedProviders(platform string, providers []deletedProvider) error {
	if len(providers) == 0 {
		return nil
	}
	if err := ensureProviderDeleteTables(); err != nil {
		return err
	}

	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	for _, p := range providers {
		if err := cleanupDeletedProviderTx(tx, platform, p); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交删除供应商数据清理事务失败: %w", err)
	}
	return nil
}

// ensureProviderDeleteTables 保证删除路径依赖的表已就绪。
//
// 正常启动时 InitDatabase 已经跑过迁移，这里是幂等的兜底（迁移已应用时
// 只读一次 schema_version）。保留它是因为部分测试不经 InitDatabase 直接
// 构造服务；生产路径不再依赖这种惰性建表——那正是"全新安装首次改名失败"
// 的成因：rename 依赖的表只在删除路径被创建。
func ensureProviderDeleteTables() error {
	if err := RunMigrations(); err != nil {
		return fmt.Errorf("应用数据库迁移失败: %w", err)
	}
	return nil
}

func rollbackProviderFile(path string, providers []Provider, primary error) error {
	originalBytes, err := serializeProviders(providers)
	if err != nil {
		return fmt.Errorf("保存供应商配置失败: %w; 序列化回滚配置失败: %v", primary, err)
	}
	if err := atomicWriteFile(path, originalBytes, 0o644); err != nil {
		return fmt.Errorf("保存供应商配置失败: %w; 配置文件回滚失败: %v", primary, err)
	}
	return fmt.Errorf("保存供应商配置失败: %w", primary)
}

func cleanupDeletedProviderByName(platform, providerName string) error {
	if strings.TrimSpace(providerName) == "" {
		return nil
	}
	return cleanupDeletedProviders(platform, []deletedProvider{{Name: providerName}})
}

func cleanupDeletedProviderTx(tx *sql.Tx, platform string, provider deletedProvider) error {
	names, err := deletedProviderNames(tx, platform, provider)
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := deleteDeletedProviderNameRows(tx, platform, name); err != nil {
			return err
		}
	}
	if err := deleteDeletedProviderIDRows(tx, platform, provider.ID); err != nil {
		return err
	}

	log.Printf("[ProviderDelete] 已清理已删除供应商数据 platform=%s provider_id=%d provider_name=%q", platform, provider.ID, provider.Name)
	return nil
}

func deleteDeletedProviderNameRows(tx *sql.Tx, platform, name string) error {
	scope, err := resolveProviderDataScope(platform)
	if err != nil {
		return err
	}
	if err := deleteRequestLogProviderNameRows(tx, scope, name); err != nil {
		return fmt.Errorf("删除 request_log 失败: %w", err)
	}
	if err := deleteRelayAttemptProviderNameRows(tx, scope, name); err != nil {
		return fmt.Errorf("删除 relay_attempt 失败: %w", err)
	}
	blacklistQuery := `DELETE FROM provider_blacklist WHERE platform = ? AND provider_name = ?`
	blacklistArgs := []any{platform, name}
	if platform == openCodePlatform {
		blacklistQuery = `DELETE FROM provider_blacklist WHERE provider_name = ? AND (platform = ? OR platform LIKE ?)`
		blacklistArgs = []any{name, platform, platform + ":%"}
	}
	if _, err := tx.Exec(blacklistQuery, blacklistArgs...); err != nil {
		return fmt.Errorf("删除 provider_blacklist 失败: %w", err)
	}
	return nil
}

// 旧格式 platform='custom:<toolId>' 的历史行已由迁移 v3 归一化，
// 写入侧也只产生当前格式，因此这两个函数不再需要兼容 OR。
func deleteRequestLogProviderNameRows(tx *sql.Tx, scope providerDataScope, name string) error {
	if scope.sourceID == "" {
		_, err := tx.Exec(`DELETE FROM request_log WHERE platform = ? AND provider = ?`, scope.identityPlatform, name)
		return err
	}
	_, err := tx.Exec(
		`DELETE FROM request_log WHERE provider = ? AND platform = ? AND source_id = ?`,
		name, scope.telemetryPlatform, scope.sourceID,
	)
	return err
}

func deleteRelayAttemptProviderNameRows(tx *sql.Tx, scope providerDataScope, name string) error {
	if scope.sourceID == "" {
		_, err := tx.Exec(`DELETE FROM relay_attempt WHERE platform = ? AND provider = ?`, scope.identityPlatform, name)
		return err
	}
	_, err := tx.Exec(
		`DELETE FROM relay_attempt WHERE provider = ? AND platform = ? AND source_id = ?`,
		name, scope.telemetryPlatform, scope.sourceID,
	)
	return err
}

// deleteDeletedProviderIDRows 按 provider_id 清理关联数据。
//
// 这是主数据入库后的主要清理方式：按 ID 一次覆盖该 provider 的全部历史记录，
// 无论这些记录当时用的是哪个名字。按名字清理只作为补充，
// 用于处理 provider_id 为 NULL 的早期数据。
func deleteDeletedProviderIDRows(tx *sql.Tx, platform string, providerID int64) error {
	if providerID == 0 {
		return nil
	}
	for _, table := range []string{"request_log", "relay_attempt"} {
		if _, err := tx.Exec(
			fmt.Sprintf(`DELETE FROM %s WHERE provider_id = ?`, table), providerID,
		); err != nil {
			return fmt.Errorf("按 provider_id 删除 %s 失败: %w", table, err)
		}
	}
	if _, err := tx.Exec(
		`DELETE FROM provider_blacklist WHERE provider_id = ?`, providerID,
	); err != nil {
		return fmt.Errorf("按 provider_id 删除 provider_blacklist 失败: %w", err)
	}
	return nil
}

// deletedProviderNames 返回被删除 provider 需要清理的名字集合。
//
// 原先还要从 provider_alias 里收集该 provider 用过的所有历史名字，
// 因为日志与黑名单按名字关联，漏掉任一历史名就会留下清理不掉的孤儿数据。
// 现在两者都带 provider_id，按 ID 清理即可覆盖全部历史记录，
// 名字只用于清理 provider_id 为 NULL 的早期数据。
func deletedProviderNames(tx *sql.Tx, platform string, provider deletedProvider) ([]string, error) {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		return nil, nil
	}
	return []string{name}, nil
}
