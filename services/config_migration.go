package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	configMigrationFormat  = "code-switch-r-config"
	configMigrationVersion = 1
	configMigrationExt     = ".csrconfig"
)

var configMigrationFiles = map[string]struct{}{
	appSettingsFile:                  {},
	frontendPreferencesFileName:      {},
	"network.json":                   {},
	"pi-ui.json":                     {},
	piProviderTemplatesFilename:      {},
	providerRequestTemplatesFilename: {},
	"prompts.json":                   {},
	"mcp.json":                       {},
	"mcp-claude-code.json":           {},
	"mcp-codex.json":                 {},
	"mcp-gemini.json":                {},
	"mcp-reasonix.json":              {},
	filepath.Join("model-pricing", pricingRulesFilename): {},
}

type ConfigMigrationProvider struct {
	Platform   string          `json:"platform"`
	SourceID   string          `json:"source_id,omitempty"`
	Name       string          `json:"name"`
	APIURL     string          `json:"api_url"`
	Level      int             `json:"level"`
	SortOrder  int             `json:"sort_order"`
	ConfigJSON json.RawMessage `json:"config"`
}

type ConfigMigrationPackage struct {
	Format     string                     `json:"format"`
	Version    int                        `json:"version"`
	ExportedAt string                     `json:"exported_at"`
	Providers  []ConfigMigrationProvider  `json:"providers"`
	Files      map[string]json.RawMessage `json:"files"`
}

type ConfigMigrationResult struct {
	Path          string `json:"path"`
	ProviderCount int    `json:"provider_count"`
	FileCount     int    `json:"file_count"`
	Bytes         int64  `json:"bytes"`
	Warning       string `json:"warning,omitempty"`
}

type transferFileBackup struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func (is *ImportService) ExportSanitizedConfig(path string) (ConfigMigrationResult, error) {
	target, err := resolveTransferOutputFile(path, configMigrationExt)
	if err != nil {
		return ConfigMigrationResult{}, err
	}
	providers, err := exportSanitizedProviders()
	if err != nil {
		return ConfigMigrationResult{}, err
	}
	files, err := exportSanitizedConfigFiles()
	if err != nil {
		return ConfigMigrationResult{}, err
	}
	pkg := ConfigMigrationPackage{
		Format: configMigrationFormat, Version: configMigrationVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), Providers: providers, Files: files,
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return ConfigMigrationResult{}, fmt.Errorf("序列化配置迁移包失败: %w", err)
	}
	if err := atomicWriteFile(target, data, 0o600); err != nil {
		return ConfigMigrationResult{}, fmt.Errorf("写入配置迁移包失败: %w", err)
	}
	return ConfigMigrationResult{Path: target, ProviderCount: len(providers), FileCount: len(files), Bytes: int64(len(data))}, nil
}

func (is *ImportService) ImportSanitizedConfig(path string) (ConfigMigrationResult, error) {
	source := expandTransferPath(path)
	if source == "" {
		return ConfigMigrationResult{}, fmt.Errorf("配置迁移包路径不能为空")
	}
	data, err := readTransferFile(source, 64<<20)
	if err != nil {
		return ConfigMigrationResult{}, err
	}
	var pkg ConfigMigrationPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ConfigMigrationResult{}, fmt.Errorf("解析配置迁移包失败: %w", err)
	}
	if pkg.Format != configMigrationFormat || pkg.Version != configMigrationVersion {
		return ConfigMigrationResult{}, fmt.Errorf("不支持的配置迁移包格式或版本")
	}

	fileBackups, importedFiles, err := importSanitizedConfigFiles(pkg.Files)
	if err != nil {
		return ConfigMigrationResult{}, err
	}
	providerSnapshots, importedProviders, err := is.importSanitizedProviders(pkg.Providers)
	if err != nil {
		rollbackTransferFiles(fileBackups)
		return ConfigMigrationResult{}, err
	}
	_ = providerSnapshots
	if is.appSettings != nil {
		is.appSettings.mu.Lock()
		is.appSettings.cacheValid = false
		is.appSettings.mu.Unlock()
	}
	return ConfigMigrationResult{
		Path: source, ProviderCount: importedProviders, FileCount: importedFiles, Bytes: int64(len(data)),
		Warning: "配置已导入；凭据不会从迁移包导入，新供应商默认停用。",
	}, nil
}

func exportSanitizedProviders() ([]ConfigMigrationProvider, error) {
	db, err := dbHandle()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT platform, source_id, name, api_url, level, sort_order, config_json
		FROM provider
		WHERE platform <> 'custom' AND platform NOT LIKE 'custom:%'
		ORDER BY platform, source_id, sort_order, id
	`)
	if err != nil {
		return nil, fmt.Errorf("读取 Provider 配置失败: %w", err)
	}
	defer rows.Close()

	result := make([]ConfigMigrationProvider, 0)
	for rows.Next() {
		var record ConfigMigrationProvider
		var configJSON string
		if err := rows.Scan(&record.Platform, &record.SourceID, &record.Name, &record.APIURL, &record.Level, &record.SortOrder, &configJSON); err != nil {
			return nil, err
		}
		if record.SourceID != "" {
			return nil, fmt.Errorf("Provider %s 使用了不受支持的 source_id", record.Name)
		}
		record.APIURL = sanitizeURLCredential(record.APIURL)
		var config any
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return nil, fmt.Errorf("解析 Provider %s 配置失败: %w", record.Name, err)
		}
		config = sanitizeConfigValue("config", config)
		configData, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		record.ConfigJSON = configData
		result = append(result, record)
	}
	return result, rows.Err()
}

func exportSanitizedConfigFiles() (map[string]json.RawMessage, error) {
	configDir, err := getAppConfigDir()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(configMigrationFiles))
	for path := range configMigrationFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make(map[string]json.RawMessage)
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(configDir, rel))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("读取配置文件 %s 失败: %w", rel, err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("配置文件 %s 不是合法 JSON: %w", rel, err)
		}
		value = sanitizeConfigValue(filepath.Base(rel), value)
		sanitized, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		result[filepath.ToSlash(rel)] = sanitized
	}
	return result, nil
}

func (is *ImportService) importSanitizedProviders(records []ConfigMigrationProvider) (map[string][]Provider, int, error) {
	grouped := make(map[string][]Provider)
	for _, record := range records {
		platform := strings.ToLower(strings.TrimSpace(record.Platform))
		if platform == "custom" || strings.HasPrefix(platform, "custom:") || record.SourceID != "" {
			return nil, 0, fmt.Errorf("配置迁移包包含已移除的自定义 CLI Provider")
		}
		if resolvePlatformID(platform) == "" && platform != "gemini" {
			return nil, 0, fmt.Errorf("配置迁移包包含未知平台: %s", record.Platform)
		}
		var config any
		if err := json.Unmarshal(record.ConfigJSON, &config); err != nil {
			return nil, 0, fmt.Errorf("Provider %s 配置无效: %w", record.Name, err)
		}
		configData, err := json.Marshal(sanitizeConfigValue("config", config))
		if err != nil {
			return nil, 0, err
		}
		provider := Provider{Name: strings.TrimSpace(record.Name), APIURL: sanitizeURLCredential(record.APIURL), Level: record.Level}
		if provider.Name == "" {
			return nil, 0, fmt.Errorf("配置迁移包包含空 Provider 名称")
		}
		if err := applyProviderConfig(&provider, string(configData)); err != nil {
			return nil, 0, fmt.Errorf("还原 Provider %s 配置失败: %w", provider.Name, err)
		}
		provider.Enabled = false
		grouped[platform] = append(grouped[platform], provider)
	}

	platforms := make([]string, 0, len(grouped))
	for platform := range grouped {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	snapshots := make(map[string][]Provider, len(platforms))
	applied := make([]string, 0, len(platforms))
	imported := 0
	for _, platform := range platforms {
		existing, err := is.providerService.LoadProviders(platform)
		if err != nil {
			rollbackProviderSnapshots(snapshots, applied)
			return nil, 0, err
		}
		snapshots[platform] = existing
		merged := mergeMigratedProviders(existing, grouped[platform])
		scope, err := scopeForKind(platform)
		if err != nil {
			rollbackProviderSnapshots(snapshots, applied)
			return nil, 0, err
		}
		if _, err := replaceProvidersInDB(context.Background(), scope, merged); err != nil {
			rollbackProviderSnapshots(snapshots, applied)
			return nil, 0, fmt.Errorf("导入 %s Provider 失败: %w", platform, err)
		}
		applied = append(applied, platform)
		imported += len(grouped[platform])
	}
	return snapshots, imported, nil
}

func mergeMigratedProviders(existing, imported []Provider) []Provider {
	byName := make(map[string]int, len(existing))
	result := make([]Provider, len(existing))
	copy(result, existing)
	for index := range existing {
		byName[strings.ToLower(strings.TrimSpace(existing[index].Name))] = index
	}
	for _, incoming := range imported {
		key := strings.ToLower(strings.TrimSpace(incoming.Name))
		if index, ok := byName[key]; ok {
			merged := incoming
			preserveProviderCredentials(&merged, existing[index])
			result[index] = merged
			continue
		}
		incoming.ID = 0
		incoming.APIKey = ""
		incoming.Enabled = false
		byName[key] = len(result)
		result = append(result, incoming)
	}
	return result
}

func preserveProviderCredentials(incoming *Provider, existing Provider) {
	incoming.ID = existing.ID
	incoming.APIKey = existing.APIKey
	incoming.Enabled = existing.Enabled
	incoming.APIURL = mergeSensitiveURL(existing.APIURL, incoming.APIURL)
	incoming.Headers = mergeStringMaps(existing.Headers, incoming.Headers)
	if incoming.MetadataUserID == "" {
		incoming.MetadataUserID = existing.MetadataUserID
	}
	if incoming.RequestIdentity != nil && existing.RequestIdentity != nil {
		mergeIdentityCredentials(incoming.RequestIdentity, *existing.RequestIdentity)
	}
	for model, identity := range incoming.ModelRequestIdentities {
		if old, ok := existing.ModelRequestIdentities[model]; ok {
			mergeIdentityCredentials(&identity, old)
			incoming.ModelRequestIdentities[model] = identity
		}
	}
	if incoming.gemini != nil && existing.gemini != nil {
		incoming.gemini.EnvConfig = mergeStringMaps(existing.gemini.EnvConfig, incoming.gemini.EnvConfig)
		incoming.gemini.SettingsConfig = mergeJSONMaps(existing.gemini.SettingsConfig, incoming.gemini.SettingsConfig)
	}
}

func mergeIdentityCredentials(incoming *ProviderRequestIdentity, existing ProviderRequestIdentity) {
	incoming.Headers = mergeStringMaps(existing.Headers, incoming.Headers)
	if incoming.MetadataUserID == "" {
		incoming.MetadataMode = existing.MetadataMode
		incoming.MetadataUserID = existing.MetadataUserID
	}
}

func rollbackProviderSnapshots(snapshots map[string][]Provider, platforms []string) {
	for index := len(platforms) - 1; index >= 0; index-- {
		platform := platforms[index]
		scope, err := scopeForKind(platform)
		if err == nil {
			_, _ = replaceProvidersInDB(context.Background(), scope, snapshots[platform])
		}
	}
}

func importSanitizedConfigFiles(files map[string]json.RawMessage) ([]transferFileBackup, int, error) {
	configDir, err := ensureAppConfigDir()
	if err != nil {
		return nil, 0, err
	}
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	backups := make([]transferFileBackup, 0, len(paths))
	for _, rawRel := range paths {
		rel := filepath.Clean(filepath.FromSlash(rawRel))
		if _, ok := configMigrationFiles[rel]; !ok || filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			rollbackTransferFiles(backups)
			return nil, 0, fmt.Errorf("配置迁移包包含不允许的文件: %s", rawRel)
		}
		var incoming any
		if err := json.Unmarshal(files[rawRel], &incoming); err != nil {
			rollbackTransferFiles(backups)
			return nil, 0, fmt.Errorf("配置文件 %s 无效: %w", rawRel, err)
		}
		incoming = sanitizeConfigValue(filepath.Base(rel), incoming)
		target := filepath.Join(configDir, rel)
		backup := transferFileBackup{path: target, mode: 0o600}
		if old, err := os.ReadFile(target); err == nil {
			backup.exists, backup.data = true, old
			if info, statErr := os.Stat(target); statErr == nil {
				backup.mode = info.Mode().Perm()
			}
			var existing any
			if json.Unmarshal(old, &existing) == nil {
				incoming = mergeJSONValue(existing, incoming)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rollbackTransferFiles(backups)
			return nil, 0, err
		}
		data, err := json.MarshalIndent(incoming, "", "  ")
		if err != nil {
			rollbackTransferFiles(backups)
			return nil, 0, err
		}
		if err := atomicWriteFile(target, data, backup.mode); err != nil {
			rollbackTransferFiles(backups)
			return nil, 0, fmt.Errorf("写入配置文件 %s 失败: %w", rawRel, err)
		}
		backups = append(backups, backup)
	}
	return backups, len(backups), nil
}

func rollbackTransferFiles(backups []transferFileBackup) {
	for index := len(backups) - 1; index >= 0; index-- {
		backup := backups[index]
		if backup.exists {
			_ = atomicWriteFile(backup.path, backup.data, backup.mode)
		} else {
			_ = os.Remove(backup.path)
		}
	}
}

func sanitizeConfigValue(parentKey string, value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if isCredentialKey(key) {
				continue
			}
			result[key] = sanitizeConfigValue(key, item)
		}
		return result
	case []any:
		if strings.EqualFold(parentKey, "args") {
			return sanitizeCommandArgs(typed)
		}
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = sanitizeConfigValue(parentKey, item)
		}
		return result
	case string:
		return sanitizeURLCredential(typed)
	default:
		return value
	}
}

func sanitizeCommandArgs(args []any) []any {
	result := make([]any, 0, len(args))
	skipNext := false
	for _, raw := range args {
		if skipNext {
			skipNext = false
			continue
		}
		arg, ok := raw.(string)
		if !ok {
			result = append(result, sanitizeConfigValue("args", raw))
			continue
		}
		name := arg
		if index := strings.IndexByte(name, '='); index >= 0 {
			name = name[:index]
		}
		if isCredentialKey(strings.TrimLeft(name, "-/")) {
			if !strings.Contains(arg, "=") {
				skipNext = true
			}
			continue
		}
		result = append(result, sanitizeURLCredential(arg))
	}
	return result
}

func isCredentialKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(normalized)
	for _, marker := range []string{
		"apikey", "accesstoken", "refreshtoken", "authtoken", "bearertoken", "password",
		"passwd", "secret", "credential", "authorization", "proxyauthorization", "privatekey",
		"sessionid", "deviceid", "accountuuid", "metadatauserid", "cookie",
	} {
		if normalized == marker || strings.HasSuffix(normalized, marker) {
			return true
		}
	}
	return normalized == "key" || strings.HasSuffix(normalized, "token")
}

func sanitizeURLCredential(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	parsed.User = nil
	query := parsed.Query()
	for key, values := range query {
		if !isCredentialKey(key) {
			continue
		}
		kept := values[:0]
		for _, value := range values {
			if strings.Contains(value, "{") && strings.Contains(value, "}") {
				kept = append(kept, value)
			}
		}
		if len(kept) == 0 {
			query.Del(key)
		} else {
			query[key] = kept
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func mergeSensitiveURL(existing, incoming string) string {
	oldURL, oldErr := url.Parse(existing)
	newURL, newErr := url.Parse(incoming)
	if oldErr != nil || newErr != nil || oldURL.Scheme == "" || newURL.Scheme == "" {
		return incoming
	}
	query := newURL.Query()
	for key, values := range oldURL.Query() {
		if isCredentialKey(key) && len(query[key]) == 0 {
			query[key] = append([]string(nil), values...)
		}
	}
	newURL.RawQuery = query.Encode()
	if newURL.User == nil {
		newURL.User = oldURL.User
	}
	return newURL.String()
}

func mergeStringMaps(existing, incoming map[string]string) map[string]string {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	result := make(map[string]string, len(existing)+len(incoming))
	for key, value := range existing {
		result[key] = value
	}
	for key, value := range incoming {
		result[key] = value
	}
	return result
}

func mergeJSONMaps(existing, incoming map[string]any) map[string]any {
	merged, _ := mergeJSONValue(existing, incoming).(map[string]any)
	return merged
}

func mergeJSONValue(existing, incoming any) any {
	oldMap, oldOK := existing.(map[string]any)
	newMap, newOK := incoming.(map[string]any)
	if !oldOK || !newOK {
		if oldString, ok := existing.(string); ok {
			if newString, ok := incoming.(string); ok {
				return mergeSensitiveURL(oldString, newString)
			}
		}
		return incoming
	}
	result := make(map[string]any, len(oldMap)+len(newMap))
	for key, value := range oldMap {
		result[key] = value
	}
	for key, value := range newMap {
		if old, ok := result[key]; ok {
			result[key] = mergeJSONValue(old, value)
		} else {
			result[key] = value
		}
	}
	return result
}

func resolveTransferOutputFile(rawPath, extension string) (string, error) {
	path := expandTransferPath(rawPath)
	if path == "" {
		return "", fmt.Errorf("导出文件路径不能为空")
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "code-switch-R"+extension)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += extension
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	configDir, err := getAppConfigDir()
	if err != nil {
		return "", err
	}
	configAbs, err := filepath.Abs(configDir)
	if err != nil {
		return "", err
	}
	if pathWithin(configAbs, abs) {
		return "", fmt.Errorf("导出文件不能放在应用数据目录内")
	}
	if err := EnsureDir(filepath.Dir(abs)); err != nil {
		return "", err
	}
	return abs, nil
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func readTransferFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("路径不是普通文件: %s", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("文件过大: %d bytes", info.Size())
	}
	return os.ReadFile(path)
}
