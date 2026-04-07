package services

import (
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

const (
	legacyProjectConfigDirName = ".code-switch"
	hotkeyDBFileName           = "suidemo.db"
	projectExportManifestFile  = "export-manifest.json"
)

type ProjectTransferInfo struct {
	LegacyConfigDir  string `json:"legacy_config_dir"`
	CurrentConfigDir string `json:"current_config_dir"`
	HotkeyDBPath     string `json:"hotkey_db_path"`
}

type ProjectTransferResult struct {
	SourcePath            string `json:"source_path"`
	TargetPath            string `json:"target_path"`
	CopiedFileCount       int    `json:"copied_file_count"`
	CopiedBytes           int64  `json:"copied_bytes"`
	ImportedRequestLogs   int64  `json:"imported_request_logs"`
	ImportedHealthChecks  int64  `json:"imported_health_checks"`
	ImportedBlacklistRows int64  `json:"imported_blacklist_rows"`
	ImportedAppSettings   int64  `json:"imported_app_settings"`
	ImportedHotkeys       int64  `json:"imported_hotkeys"`
	Warning               string `json:"warning"`
}

type projectExportManifest struct {
	ExportedAt       string `json:"exported_at"`
	SourceConfigDir  string `json:"source_config_dir"`
	IncludesHotkeyDB bool   `json:"includes_hotkey_db"`
}

func (is *ImportService) GetProjectTransferInfo() (ProjectTransferInfo, error) {
	home, err := getUserHomeDir()
	if err != nil {
		return ProjectTransferInfo{}, err
	}
	currentConfigDir, err := getAppConfigDir()
	if err != nil {
		return ProjectTransferInfo{}, err
	}
	hotkeyDBPath, err := getSafeDBPath()
	if err != nil {
		return ProjectTransferInfo{}, err
	}
	return ProjectTransferInfo{
		LegacyConfigDir:  filepath.Join(home, legacyProjectConfigDirName),
		CurrentConfigDir: currentConfigDir,
		HotkeyDBPath:     hotkeyDBPath,
	}, nil
}

func (is *ImportService) ImportLegacyProjectDirectory(path string) (ProjectTransferResult, error) {
	info, err := is.GetProjectTransferInfo()
	if err != nil {
		return ProjectTransferResult{}, err
	}
	sourceDir, err := resolveConfigDirectory(path, legacyProjectConfigDirName)
	if err != nil {
		return ProjectTransferResult{}, err
	}
	result, err := is.importProjectDirectory(sourceDir)
	if err != nil {
		return result, err
	}
	result.TargetPath = info.CurrentConfigDir
	if result.ImportedHotkeys == 0 {
		if legacyHotkeyPath, pathErr := getLegacyHotkeyDBPath(); pathErr == nil && FileExists(legacyHotkeyPath) {
			if importedHotkeys, importErr := replaceHotkeyDatabase(legacyHotkeyPath); importErr != nil {
				result.Warning = appendWarning(result.Warning, fmt.Sprintf("导入旧快捷键数据库失败: %v", importErr))
			} else {
				result.ImportedHotkeys = importedHotkeys
			}
		}
	}
	return result, nil
}

func (is *ImportService) ImportCurrentProjectDirectory(path string) (ProjectTransferResult, error) {
	sourceDir, err := resolveConfigDirectory(path, projectAppConfigDirName)
	if err != nil {
		return ProjectTransferResult{}, err
	}
	return is.importProjectDirectory(sourceDir)
}

func (is *ImportService) ExportCurrentProjectDirectory(path string) (ProjectTransferResult, error) {
	info, err := is.GetProjectTransferInfo()
	if err != nil {
		return ProjectTransferResult{}, err
	}
	targetDir, err := prepareOutputDirectory(path)
	if err != nil {
		return ProjectTransferResult{}, err
	}

	result := ProjectTransferResult{
		SourcePath: info.CurrentConfigDir,
		TargetPath: targetDir,
	}

	copiedFiles, copiedBytes, err := copyDirectoryContents(info.CurrentConfigDir, targetDir, shouldSkipLiveDBFile)
	if err != nil {
		return result, err
	}
	result.CopiedFileCount += copiedFiles
	result.CopiedBytes += copiedBytes

	dbBytes, err := exportSQLiteSnapshot(filepath.Join(targetDir, appDatabaseFilename), func(target string) error {
		db, dbErr := xdb.DB("default")
		if dbErr != nil {
			return dbErr
		}
		if _, execErr := db.Exec("PRAGMA wal_checkpoint(FULL)"); execErr != nil {
			return execErr
		}
		return vacuumInto(db, target)
	})
	if err != nil {
		result.Warning = appendWarning(result.Warning, fmt.Sprintf("导出 app.db 失败: %v", err))
	} else if dbBytes > 0 {
		result.CopiedFileCount++
		result.CopiedBytes += dbBytes
	}

	hotkeyBytes, err := exportSQLiteSnapshot(filepath.Join(targetDir, hotkeyDBFileName), func(target string) error {
		hotkeyPath, pathErr := getSafeDBPath()
		if pathErr != nil {
			return pathErr
		}
		db, dbErr := sql.Open("sqlite", hotkeyPath)
		if dbErr != nil {
			return dbErr
		}
		defer db.Close()
		return vacuumInto(db, target)
	})
	if err != nil {
		result.Warning = appendWarning(result.Warning, fmt.Sprintf("导出快捷键数据库失败: %v", err))
	} else if hotkeyBytes > 0 {
		result.CopiedFileCount++
		result.CopiedBytes += hotkeyBytes
	}

	manifest := projectExportManifest{
		ExportedAt:       time.Now().Format(time.RFC3339),
		SourceConfigDir:  info.CurrentConfigDir,
		IncludesHotkeyDB: hotkeyBytes > 0,
	}
	if err := AtomicWriteJSON(filepath.Join(targetDir, projectExportManifestFile), manifest); err == nil {
		result.CopiedFileCount++
	} else {
		result.Warning = appendWarning(result.Warning, fmt.Sprintf("写入导出清单失败: %v", err))
	}

	return result, nil
}

func (is *ImportService) importProjectDirectory(sourceDir string) (ProjectTransferResult, error) {
	currentConfigDir, err := ensureAppConfigDir()
	if err != nil {
		return ProjectTransferResult{}, err
	}

	result := ProjectTransferResult{
		SourcePath: sourceDir,
		TargetPath: currentConfigDir,
	}

	copiedFiles, copiedBytes, err := copyDirectoryContents(sourceDir, currentConfigDir, shouldSkipImportDBFile)
	if err != nil {
		return result, err
	}
	result.CopiedFileCount = copiedFiles
	result.CopiedBytes = copiedBytes

	prefsFiles, prefsBytes, prefsErr := importLegacyFrontendPreferencesIfNeeded(sourceDir, currentConfigDir)
	if prefsErr != nil {
		result.Warning = appendWarning(result.Warning, fmt.Sprintf("导入前端偏好失败: %v", prefsErr))
	} else {
		result.CopiedFileCount += prefsFiles
		result.CopiedBytes += prefsBytes
	}

	mainDBSource := filepath.Join(sourceDir, appDatabaseFilename)
	if FileExists(mainDBSource) {
		imported, importErr := replaceMainDatabaseTables(mainDBSource)
		if importErr != nil {
			result.Warning = appendWarning(result.Warning, fmt.Sprintf("导入 app.db 失败: %v", importErr))
		} else {
			result.ImportedAppSettings = imported["app_settings"]
			result.ImportedBlacklistRows = imported["provider_blacklist"]
			result.ImportedRequestLogs = imported["request_log"]
			result.ImportedHealthChecks = imported["health_check_history"]
		}
	}

	hotkeySource := resolveHotkeyDBSource(sourceDir)
	if hotkeySource != "" {
		importedHotkeys, importErr := replaceHotkeyDatabase(hotkeySource)
		if importErr != nil {
			result.Warning = appendWarning(result.Warning, fmt.Sprintf("导入快捷键数据库失败: %v", importErr))
		} else {
			result.ImportedHotkeys = importedHotkeys
		}
	}

	return result, nil
}

func resolveConfigDirectory(rawPath string, expectedChild string) (string, error) {
	path := expandTransferPath(rawPath)
	if path == "" {
		return "", fmt.Errorf("配置目录路径不能为空")
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return path, nil
	}
	if err == nil && !info.IsDir() {
		return "", fmt.Errorf("路径不是目录: %s", path)
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	candidate := filepath.Join(path, expectedChild)
	candidateInfo, candidateErr := os.Stat(candidate)
	if candidateErr == nil && candidateInfo.IsDir() {
		return candidate, nil
	}
	if candidateErr != nil && !os.IsNotExist(candidateErr) {
		return "", candidateErr
	}
	return "", fmt.Errorf("配置目录不存在: %s", path)
}

func prepareOutputDirectory(rawPath string) (string, error) {
	path := expandTransferPath(rawPath)
	if path == "" {
		return "", fmt.Errorf("导出目录路径不能为空")
	}
	if err := EnsureDir(path); err != nil {
		return "", err
	}
	return path, nil
}

func expandTransferPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~\\") || strings.HasPrefix(path, "~/") {
		home, err := getUserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if path == "~" {
		if home, err := getUserHomeDir(); err == nil {
			return home
		}
	}
	return filepath.Clean(path)
}

func shouldSkipLiveDBFile(relPath string, entry fs.DirEntry) bool {
	base := strings.ToLower(filepath.Base(relPath))
	return base == strings.ToLower(appDatabaseFilename) ||
		base == strings.ToLower(appDatabaseFilename)+"-wal" ||
		base == strings.ToLower(appDatabaseFilename)+"-shm"
}

func shouldSkipImportDBFile(relPath string, entry fs.DirEntry) bool {
	base := strings.ToLower(filepath.Base(relPath))
	if base == strings.ToLower(appDatabaseFilename) ||
		base == strings.ToLower(appDatabaseFilename)+"-wal" ||
		base == strings.ToLower(appDatabaseFilename)+"-shm" ||
		base == strings.ToLower(hotkeyDBFileName) {
		return true
	}
	if strings.HasSuffix(base, ".tmp") || strings.Contains(base, ".tmp.") || strings.Contains(base, ".bak.") {
		return true
	}
	return false
}

func copyDirectoryContents(sourceDir, targetDir string, skip func(string, fs.DirEntry) bool) (int, int64, error) {
	copiedFiles := 0
	var copiedBytes int64
	err := filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		if skip != nil && skip(relPath, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		targetPath := filepath.Join(targetDir, relPath)
		if entry.IsDir() {
			return EnsureDir(targetPath)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := copyTransferFile(path, targetPath, info.Mode()); err != nil {
			return err
		}
		copiedFiles++
		copiedBytes += info.Size()
		return nil
	})
	return copiedFiles, copiedBytes, err
}

func copyTransferFile(sourcePath, targetPath string, mode fs.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := EnsureDir(filepath.Dir(targetPath)); err != nil {
		return err
	}
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return err
	}
	return target.Close()
}

func exportSQLiteSnapshot(targetPath string, exportFn func(target string) error) (int64, error) {
	_ = os.Remove(targetPath)
	if err := exportFn(targetPath); err != nil {
		return 0, err
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func vacuumInto(db *sql.DB, targetPath string) error {
	statement := fmt.Sprintf("VACUUM INTO '%s'", strings.ReplaceAll(targetPath, "'", "''"))
	_, err := db.Exec(statement)
	return err
}

func resolveHotkeyDBSource(sourceDir string) string {
	candidates := []string{
		filepath.Join(sourceDir, hotkeyDBFileName),
		filepath.Join(sourceDir, "SuiNest", hotkeyDBFileName),
	}
	for _, candidate := range candidates {
		if FileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func replaceMainDatabaseTables(sourcePath string) (map[string]int64, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return nil, err
	}
	tables := []string{"app_settings", "provider_blacklist", "request_log", "health_check_history"}
	return replaceSQLiteTables(db, sourcePath, tables)
}

func replaceHotkeyDatabase(sourcePath string) (int64, error) {
	targetPath, err := getSafeDBPath()
	if err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", targetPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	results, err := replaceSQLiteTables(db, sourcePath, []string{"hotkeys"})
	if err != nil {
		return 0, err
	}
	return results["hotkeys"], nil
}

func replaceSQLiteTables(db *sql.DB, sourcePath string, tables []string) (map[string]int64, error) {
	if !FileExists(sourcePath) {
		return map[string]int64{}, nil
	}
	results := make(map[string]int64, len(tables))
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("ATTACH DATABASE ? AS importsrc", sourcePath); err != nil {
		return nil, err
	}
	defer tx.Exec("DETACH DATABASE importsrc")

	for _, tableName := range tables {
		imported, err := replaceSQLiteTable(tx, tableName)
		if err != nil {
			return nil, err
		}
		results[tableName] = imported
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func replaceSQLiteTable(tx *sql.Tx, tableName string) (int64, error) {
	exists, err := sqliteTableExists(tx, "importsrc", tableName)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	targetColumns, err := sqliteTableColumns(tx, "", tableName)
	if err != nil {
		return 0, err
	}
	sourceColumns, err := sqliteTableColumns(tx, "importsrc", tableName)
	if err != nil {
		return 0, err
	}
	columnList := intersectColumns(targetColumns, sourceColumns)
	if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", quoteSQLiteIdentifier(tableName))); err != nil {
		return 0, err
	}
	if len(columnList) == 0 {
		return 0, nil
	}

	quotedColumns := make([]string, 0, len(columnList))
	for _, column := range columnList {
		quotedColumns = append(quotedColumns, quoteSQLiteIdentifier(column))
	}
	joinedColumns := strings.Join(quotedColumns, ", ")
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM importsrc.%s",
		quoteSQLiteIdentifier(tableName),
		joinedColumns,
		joinedColumns,
		quoteSQLiteIdentifier(tableName),
	)
	if _, err := tx.Exec(insertSQL); err != nil {
		return 0, err
	}

	var count int64
	if err := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteSQLiteIdentifier(tableName))).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func sqliteTableExists(queryable interface {
	QueryRow(string, ...interface{}) *sql.Row
}, schema string, tableName string) (bool, error) {
	sqlText := "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	if schema != "" {
		sqlText = fmt.Sprintf("SELECT COUNT(*) FROM %s.sqlite_master WHERE type = 'table' AND name = ?", schema)
	}
	var count int
	if err := queryable.QueryRow(sqlText, tableName).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func sqliteTableColumns(queryable interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema string, tableName string) ([]string, error) {
	pragma := fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdentifier(tableName))
	if schema != "" {
		pragma = fmt.Sprintf("PRAGMA %s.table_info(%s)", schema, quoteSQLiteIdentifier(tableName))
	}
	rows, err := queryable.Query(pragma)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make([]string, 0)
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func intersectColumns(targetColumns, sourceColumns []string) []string {
	if len(targetColumns) == 0 || len(sourceColumns) == 0 {
		return nil
	}
	sourceSet := make(map[string]struct{}, len(sourceColumns))
	for _, column := range sourceColumns {
		sourceSet[column] = struct{}{}
	}
	result := make([]string, 0, len(targetColumns))
	for _, column := range targetColumns {
		if _, ok := sourceSet[column]; ok {
			result = append(result, column)
		}
	}
	return result
}

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func appendWarning(existing, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return next
	}
	return existing + "; " + next
}
