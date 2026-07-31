package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// A1 第三步：Provider 主数据的读写落到 SQLite。
//
// 本文件只提供 repository 原语，不改动 ProviderService 的行为——
// 接线是下一步。这样可以先单独验证「DB 读写与 JSON 读写等价」。
//
// 关键收益在于写入变成单个事务：现有实现是「先写 JSON → 再提交 DB 事务 →
// 失败时补偿回写文件」，进程在写完文件、提交事务之前崩溃就永久不一致，
// 而且补偿本身可能失败（代码只能打 CRITICAL 日志）。入库之后不存在这个窗口。

// providerScope 标识一组 provider 所属的范围。
// sourceID 保留为数据库结构字段，当前注册平台均使用空值。
type providerScope struct {
	platform string
	sourceID string
}

// scopeForKind 把 provider kind 解析成存储范围。
//
// kind 必须是注册平台（claude/codex/grok/reasonix/pi，含别名）或 gemini。
func scopeForKind(kind string) (providerScope, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if id := resolvePlatformID(normalized); id != "" {
		return providerScope{platform: id}, nil
	}
	// gemini 由独立的 GeminiService 管理，尚未并入 provider 表（A1 第 5 步）
	if normalized == "gemini" {
		return providerScope{platform: "gemini"}, nil
	}
	return providerScope{}, fmt.Errorf("不支持的 provider kind: %s", kind)
}

// providerRowColumns provider 表的读取列，顺序与 scanProviderRow 一致
const providerRowColumns = `id, name, api_url, api_key, enabled, level, config_json`

// scanProviderRow 把一行还原成 Provider。
// 列字段直接赋值，长尾字段由 config_json 恢复。
func scanProviderRow(scanner interface{ Scan(...any) error }) (Provider, error) {
	var (
		provider   Provider
		enabled    int
		configJSON string
	)
	if err := scanner.Scan(
		&provider.ID, &provider.Name, &provider.APIURL, &provider.APIKey,
		&enabled, &provider.Level, &configJSON,
	); err != nil {
		return Provider{}, err
	}
	provider.Enabled = enabled == 1
	if err := applyProviderConfig(&provider, configJSON); err != nil {
		return Provider{}, fmt.Errorf("解析 provider %q 的配置失败: %w", provider.Name, err)
	}
	return provider, nil
}

// loadProvidersFromDB 按范围读出 provider，顺序与用户在界面上的排序一致。
func loadProvidersFromDB(ctx context.Context, scope providerScope) ([]Provider, error) {
	db, err := dbHandle()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT `+providerRowColumns+`
		FROM provider
		WHERE platform = ? AND source_id = ?
		ORDER BY sort_order, id
	`, scope.platform, scope.sourceID)
	if err != nil {
		return nil, fmt.Errorf("查询 provider 失败: %w", err)
	}
	defer rows.Close()

	providers := make([]Provider, 0)
	for rows.Next() {
		provider, err := scanProviderRow(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 provider 失败: %w", err)
	}
	return providers, nil
}

// replaceProvidersInDB 用给定列表整体替换某个范围内的 provider。
//
// 语义与现有的 SaveProviders 一致：列表就是最终状态，缺失的视为删除。
// 整个替换在一个事务里完成，不存在「写了一半」的中间态，
// 因此不需要补偿回滚。
//
// 返回被删除的 provider（供调用方清理日志/黑名单等关联数据）。
func replaceProvidersInDB(ctx context.Context, scope providerScope, providers []Provider) ([]deletedProvider, error) {
	var deleted []deletedProvider

	err := dbExecInImmediateTx(ctx, func(tx dbTxExecutor) error {
		existing, err := loadProvidersInTx(ctx, tx, scope)
		if err != nil {
			return err
		}

		keep := make(map[int64]bool, len(providers))
		for _, provider := range providers {
			if provider.ID != 0 {
				keep[provider.ID] = true
			}
		}

		// 收集将被删除的 provider
		deleted = deleted[:0]
		for _, old := range existing {
			if !keep[old.ID] {
				deleted = append(deleted, deletedProvider{ID: old.ID, Name: old.Name})
			}
		}

		// 删除不在列表中的行
		for _, gone := range deleted {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM provider WHERE id = ? AND platform = ? AND source_id = ?`,
				gone.ID, scope.platform, scope.sourceID,
			); err != nil {
				return fmt.Errorf("删除 provider %q 失败: %w", gone.Name, err)
			}
		}

		// upsert 列表中的行，sort_order 取列表下标以保留用户排序
		for order, provider := range providers {
			if err := upsertProviderInTx(ctx, tx, scope, provider, order); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// loadProvidersInTx 在事务内读取当前范围的 provider（只取判断删除所需字段）
func loadProvidersInTx(ctx context.Context, tx dbTxExecutor, scope providerScope) ([]Provider, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, name FROM provider WHERE platform = ? AND source_id = ? ORDER BY sort_order, id`,
		scope.platform, scope.sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询现有 provider 失败: %w", err)
	}
	defer rows.Close()

	var existing []Provider
	for rows.Next() {
		var provider Provider
		if err := rows.Scan(&provider.ID, &provider.Name); err != nil {
			return nil, err
		}
		existing = append(existing, provider)
	}
	return existing, rows.Err()
}

// upsertProviderInTx 写入或更新一行 provider
func upsertProviderInTx(ctx context.Context, tx dbTxExecutor, scope providerScope, provider Provider, order int) error {
	configJSON, err := marshalProviderConfig(provider)
	if err != nil {
		return fmt.Errorf("序列化 provider %q 配置失败: %w", provider.Name, err)
	}

	if provider.ID == 0 {
		// 新增：交给 SQLite 分配 ID
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO provider
				(platform, source_id, name, api_url, api_key, enabled, level, sort_order, config_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			scope.platform, scope.sourceID, provider.Name, provider.APIURL, provider.APIKey,
			boolToInt(provider.Enabled), provider.Level, order, configJSON,
		); err != nil {
			return fmt.Errorf("新增 provider %q 失败: %w", provider.Name, err)
		}
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider
			(id, platform, source_id, name, api_url, api_key, enabled, level, sort_order, config_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			platform    = excluded.platform,
			source_id   = excluded.source_id,
			name        = excluded.name,
			api_url     = excluded.api_url,
			api_key     = excluded.api_key,
			enabled     = excluded.enabled,
			level       = excluded.level,
			sort_order  = excluded.sort_order,
			config_json = excluded.config_json,
			updated_at  = CURRENT_TIMESTAMP
	`,
		provider.ID, scope.platform, scope.sourceID, provider.Name, provider.APIURL, provider.APIKey,
		boolToInt(provider.Enabled), provider.Level, order, configJSON,
	); err != nil {
		return fmt.Errorf("保存 provider %q 失败: %w", provider.Name, err)
	}
	return nil
}

// renameProviderInDB 改名。
//
// 日志表已按 provider_id 关联（迁移 v3），所以这里只改一行，
// 不需要 UPDATE 日志表，也不需要 provider_alias 承接 in-flight 写入。
func renameProviderInDB(ctx context.Context, scope providerScope, providerID int64, newName string) error {
	return dbExecInImmediateTx(ctx, func(tx dbTxExecutor) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE provider SET name = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND platform = ? AND source_id = ?
		`, newName, providerID, scope.platform, scope.sourceID)
		if err != nil {
			return fmt.Errorf("改名失败: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("未找到 id=%d 的 provider", providerID)
		}
		return nil
	})
}

// providerCountInDB 统计某范围内的 provider 数量，用于判断是否已完成入库
func providerCountInDB(ctx context.Context, scope providerScope) (int, error) {
	db, err := dbHandle()
	if err != nil {
		return 0, err
	}
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM provider WHERE platform = ? AND source_id = ?`,
		scope.platform, scope.sourceID,
	).Scan(&count); err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("统计 provider 失败: %w", err)
	}
	return count, nil
}
