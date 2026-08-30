package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A1 第一步：把 Provider 主数据搬进 SQLite。
//
// 为什么要搬：Provider 元数据在 JSON、日志与黑名单在 DB，两者按 name 字符串关联。
// 这一个决定派生出下面全部机制——
//   - provider_alias 表 + 48 小时 TTL + 过期清理
//   - 禁止链式改名
//   - 先写 JSON 再提交 DB 事务的补偿式 Saga（rollbackFile）
//   - 改名要 UPDATE 三张表
//
// 而且有个补不了的洞：JSON 在事务提交前就落盘，进程在这个窗口崩溃就永久不一致，
// 启动时也没有对账。补偿本身还可能失败，代码只能打 CRITICAL 日志。
// Provider 本来就有稳定的 int64 ID，改成按 ID 关联后上面这些全部可以删除。
//
// 本步只建表并导入数据，不改动现有读写路径，因此可以独立验证。

// providerTableCreateSQL provider 主数据表。
//
// 只把会被 SQL 查询或排序的字段做成列，其余约 25 个长尾配置
// （模型映射、认证方式、自定义头、Pi 相关、Codex 续写开关等）
// 打包进 config_json。全部规范化没有收益，没有任何地方按它们过滤；
// 将来确实需要按某字段查询时再把它提升成列。
const providerTableCreateSQL = `CREATE TABLE IF NOT EXISTS provider (
	id          INTEGER PRIMARY KEY,
	platform    TEXT NOT NULL,
	source_id   TEXT NOT NULL DEFAULT '',
	name        TEXT NOT NULL,
	api_url     TEXT NOT NULL DEFAULT '',
	api_key     TEXT NOT NULL DEFAULT '',
	enabled     INTEGER NOT NULL DEFAULT 1,
	level       INTEGER NOT NULL DEFAULT 0,
	sort_order  INTEGER NOT NULL DEFAULT 0,
	config_json TEXT NOT NULL DEFAULT '{}',
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(platform, source_id, name)
)`

// migrateProviderTable 建 provider 表并从现有 JSON 文件导入。
func migrateProviderTable(tx sqlExecutor) error {
	if _, err := tx.Exec(providerTableCreateSQL); err != nil {
		return fmt.Errorf("创建 provider 表失败: %w", err)
	}
	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_provider_scope ON provider(platform, source_id, enabled, level, sort_order)`,
	); err != nil {
		return fmt.Errorf("创建 provider 索引失败: %w", err)
	}

	// 已有数据说明导入过了，不重复导入（迁移本身也只跑一次，这里是双重保险）
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM provider`).Scan(&existing); err != nil {
		return fmt.Errorf("统计 provider 行数失败: %w", err)
	}
	if existing > 0 {
		return nil
	}

	return importProvidersFromJSON(tx)
}

// providerImportSource 一个待导入的 JSON 文件
type providerImportSource struct {
	platform string
	sourceID string
	path     string
}

// collectProviderImportSources 列出所有需要导入的 provider JSON 文件
func collectProviderImportSources() ([]providerImportSource, error) {
	dir, err := getAppConfigDir()
	if err != nil {
		return nil, err
	}

	var sources []providerImportSource
	// 注册平台各一个文件
	for _, definition := range providerPlatformDefinitions {
		sources = append(sources, providerImportSource{
			platform: definition.ID,
			path:     filepath.Join(dir, definition.ProviderFile),
		})
	}

	// 自定义 CLI：providers/{toolId}.json
	customDir := filepath.Join(dir, "providers")
	entries, err := os.ReadDir(customDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("读取自定义 CLI 供应商目录失败: %w", err)
		}
		return sources, nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		toolID := strings.TrimSuffix(entry.Name(), ".json")
		if toolID == "" {
			continue
		}
		sources = append(sources, providerImportSource{
			platform: "custom",
			sourceID: toolID,
			path:     filepath.Join(customDir, entry.Name()),
		})
	}
	return sources, nil
}

// importProvidersFromJSON 把各平台 JSON 里的 provider 写入 provider 表。
//
// 现有 int64 ID 必须原样保留：紧接着就要把日志行按这些 ID 关联过去。
func importProvidersFromJSON(tx sqlExecutor) error {
	sources, err := collectProviderImportSources()
	if err != nil {
		return err
	}

	imported := 0
	for _, source := range sources {
		providers, err := readProviderEnvelope(source.path)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", source.path, err)
		}
		for order, provider := range providers {
			if err := insertProviderRow(tx, source, provider, order); err != nil {
				return err
			}
			imported++
		}
	}
	if imported > 0 {
		logInfo("已把 Provider 主数据导入数据库", "count", imported, "files", len(sources))
	}
	markImportedProviderFiles(sources)
	return nil
}

// markImportedProviderFiles 把已导入的 JSON 文件改名为 *.migrated。
//
// 导入之后数据库才是权威来源，JSON 不再被写入。留着原名会让人误以为
// 改配置可以直接编辑那些文件（编辑不生效），也分不清哪边是真数据。
// 改名而不是删除：内容保留下来，出问题时还能人工比对。
//
// 失败只记警告：这一步纯属整理，不该让迁移失败。
func markImportedProviderFiles(sources []providerImportSource) {
	for _, source := range sources {
		if _, err := os.Stat(source.path); err != nil {
			continue
		}
		target := source.path + ".migrated"
		if err := os.Rename(source.path, target); err != nil {
			logWarn("标记已导入的 Provider 配置文件失败", "path", source.path, "error", err)
		}
	}
}

// readProviderEnvelope 读取一个 provider JSON 文件；文件不存在返回空列表
func readProviderEnvelope(path string) ([]Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var envelope providerEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	return envelope.Providers, nil
}

// insertProviderRow 写入一行 provider
func insertProviderRow(tx sqlExecutor, source providerImportSource, provider Provider, order int) error {
	configJSON, err := marshalProviderConfig(provider)
	if err != nil {
		return fmt.Errorf("序列化 provider %q 配置失败: %w", provider.Name, err)
	}

	// ID 为 0 的历史数据（早期版本可能未分配）交给 SQLite 自动编号
	var idValue any
	if provider.ID != 0 {
		idValue = provider.ID
	}

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO provider
			(id, platform, source_id, name, api_url, api_key, enabled, level, sort_order, config_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		idValue, source.platform, source.sourceID, provider.Name,
		provider.APIURL, provider.APIKey, boolToInt(provider.Enabled),
		provider.Level, order, configJSON,
	); err != nil {
		return fmt.Errorf("写入 provider %q 失败: %w", provider.Name, err)
	}
	return nil
}
