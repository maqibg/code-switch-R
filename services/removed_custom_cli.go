package services

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type removedCustomCLIStore struct {
	Tools []removedCustomCLITool `json:"tools"`
}

type removedCustomCLITool struct {
	ID             string                       `json:"id"`
	ConfigFiles    []removedCustomCLIConfigFile `json:"configFiles"`
	ProxyInjection []removedCustomCLIInjection  `json:"proxyInjection"`
}

type removedCustomCLIConfigFile struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Format string `json:"format"`
}

type removedCustomCLIInjection struct {
	TargetFileID   string `json:"targetFileId"`
	BaseURLField   string `json:"baseUrlField"`
	AuthTokenField string `json:"authTokenField"`
}

// CleanupRemovedCustomCLIData removes the on-disk state of the retired custom
// CLI feature. Database rows are removed by schema migration 8 so both cleanup
// parts remain idempotent across interrupted upgrades.
func CleanupRemovedCustomCLIData() error {
	configDir, err := getAppConfigDir()
	if err != nil {
		return fmt.Errorf("获取应用数据目录失败: %w", err)
	}
	configDir = filepath.Clean(configDir)
	if !filepath.IsAbs(configDir) {
		return fmt.Errorf("拒绝清理非绝对应用数据目录: %s", configDir)
	}
	storePath := filepath.Join(configDir, "custom-cli.json")
	if err := cleanupRemovedCustomCLIProxies(storePath); err != nil {
		return err
	}

	for _, target := range []string{
		storePath,
		filepath.Join(configDir, "providers"),
	} {
		if filepath.Dir(target) != configDir {
			return fmt.Errorf("拒绝清理应用数据目录之外的路径: %s", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("删除已移除的自定义 CLI 数据 %s 失败: %w", target, err)
		}
	}
	return nil
}

func cleanupRemovedCustomCLIProxies(storePath string) error {
	data, err := os.ReadFile(storePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取已移除的自定义 CLI 配置失败: %w", err)
	}
	var store removedCustomCLIStore
	if err := json.Unmarshal(data, &store); err != nil {
		return fmt.Errorf("解析已移除的自定义 CLI 配置失败: %w", err)
	}
	for _, tool := range store.Tools {
		files := make(map[string]removedCustomCLIConfigFile, len(tool.ConfigFiles))
		for _, file := range tool.ConfigFiles {
			files[file.ID] = file
		}
		for _, injection := range tool.ProxyInjection {
			file, ok := files[injection.TargetFileID]
			if !ok {
				continue
			}
			configPath, err := removedCustomCLIConfigPath(file.Path)
			if err != nil {
				return err
			}
			current, err := os.ReadFile(configPath)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("读取已移除的自定义 CLI 目标配置 %s 失败: %w", configPath, err)
			}
			managed, err := removedCustomCLIInjectionMatches(current, file.Format, injection, tool.ID)
			if err != nil {
				logWarn("无法验证已移除的自定义 CLI 代理字段，保留外部配置", "path", configPath, "error", err)
				continue
			}
			if !managed {
				continue
			}
			backupPath := configPath + ".code-switch.backup"
			if FileExists(backupPath) {
				if err := RestoreBackup(backupPath, configPath); err != nil {
					return fmt.Errorf("恢复已移除的自定义 CLI 配置备份 %s 失败: %w", backupPath, err)
				}
				if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("删除已恢复的自定义 CLI 配置备份 %s 失败: %w", backupPath, err)
				}
				continue
			}
			cleaned, err := removeRemovedCustomCLIInjection(current, file.Format, injection)
			if err != nil {
				return fmt.Errorf("清理已移除的自定义 CLI 代理字段 %s 失败: %w", configPath, err)
			}
			if err := AtomicWriteBytes(configPath, cleaned); err != nil {
				return fmt.Errorf("写回已移除的自定义 CLI 目标配置 %s 失败: %w", configPath, err)
			}
		}
	}
	return nil
}

func removedCustomCLIConfigPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	if path == "" {
		return "", fmt.Errorf("已移除的自定义 CLI 包含空配置路径")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func removedCustomCLIInjectionMatches(content []byte, format string, injection removedCustomCLIInjection, toolID string) (bool, error) {
	baseURL, authToken, err := removedCustomCLIInjectionValues(content, format, injection)
	if err != nil {
		return false, err
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false, nil
	}
	if strings.TrimSuffix(parsed.EscapedPath(), "/") != "/custom/"+url.PathEscape(toolID) {
		return false, nil
	}
	if injection.AuthTokenField == "" {
		return true, nil
	}
	authToken = strings.TrimSpace(authToken)
	return authToken == "code-switch-r" || authToken == "code-switch", nil
}

func removedCustomCLIInjectionValues(content []byte, format string, injection removedCustomCLIInjection) (string, string, error) {
	if strings.EqualFold(format, "env") {
		values := parseEnvFile(string(content))
		return values[removedCustomCLIEnvKey(injection.BaseURLField)], values[removedCustomCLIEnvKey(injection.AuthTokenField)], nil
	}
	var values map[string]any
	var err error
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		err = json.Unmarshal(content, &values)
	case "toml":
		err = toml.Unmarshal(content, &values)
	default:
		return "", "", fmt.Errorf("不支持的配置格式: %s", format)
	}
	if err != nil {
		return "", "", err
	}
	return removedCustomCLINestedString(values, injection.BaseURLField), removedCustomCLINestedString(values, injection.AuthTokenField), nil
}

func removeRemovedCustomCLIInjection(content []byte, format string, injection removedCustomCLIInjection) ([]byte, error) {
	if strings.EqualFold(format, "env") {
		values := parseEnvFile(string(content))
		delete(values, removedCustomCLIEnvKey(injection.BaseURLField))
		if injection.AuthTokenField != "" {
			delete(values, removedCustomCLIEnvKey(injection.AuthTokenField))
		}
		return []byte(applyEnvFileEdits(string(content), values)), nil
	}
	var values map[string]any
	var err error
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		if err = json.Unmarshal(content, &values); err == nil {
			removedCustomCLIDeleteNested(values, injection.BaseURLField)
			removedCustomCLIDeleteNested(values, injection.AuthTokenField)
			return json.MarshalIndent(values, "", "  ")
		}
	case "toml":
		if err = toml.Unmarshal(content, &values); err == nil {
			removedCustomCLIDeleteNested(values, injection.BaseURLField)
			removedCustomCLIDeleteNested(values, injection.AuthTokenField)
			return toml.Marshal(values)
		}
	default:
		return nil, fmt.Errorf("不支持的配置格式: %s", format)
	}
	return nil, err
}

func removedCustomCLIEnvKey(path string) string {
	if index := strings.LastIndex(path, "."); index >= 0 {
		return path[index+1:]
	}
	return path
}

func removedCustomCLINestedString(values map[string]any, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	var current any = values
	for _, part := range strings.Split(path, ".") {
		next, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = next[part]
	}
	value, _ := current.(string)
	return value
}

func removedCustomCLIDeleteNested(values map[string]any, path string) {
	parts := strings.Split(strings.TrimSpace(path), ".")
	if len(parts) == 0 || parts[0] == "" {
		return
	}
	current := values
	for index, part := range parts {
		if index == len(parts)-1 {
			delete(current, part)
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			return
		}
		current = next
	}
}

func migrateRemoveCustomCLI(tx sqlExecutor) error {
	for _, table := range []string{"relay_attempt", "request_log", "provider_blacklist", "provider"} {
		query := fmt.Sprintf("DELETE FROM %s WHERE platform = ? OR platform LIKE ?", table)
		if _, err := tx.Exec(query, "custom", "custom:%"); err != nil {
			return fmt.Errorf("清理 %s 中的自定义 CLI 数据失败: %w", table, err)
		}
	}
	return nil
}
