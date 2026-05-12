package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	deepseekCodeSettingsDir      = ".deepseek-code"
	deepseekCodeSettingsFileName = "settings.json"
	deepseekCodeBackupFileName   = "cc-studio.back.settings.json"
	deepseekCodePlatform         = "deepseekcode"
	deepseekCodeAuthPlaceholder  = "code-switch-r"
)

type DeepSeekCodeProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

type DeepSeekCodeSettingsService struct {
	relayAddr string
}

func NewDeepSeekCodeSettingsService(relayAddr string) *DeepSeekCodeSettingsService {
	return &DeepSeekCodeSettingsService{relayAddr: relayAddr}
}

func (ds *DeepSeekCodeSettingsService) ProxyStatus() (DeepSeekCodeProxyStatus, error) {
	status := DeepSeekCodeProxyStatus{Enabled: false, BaseURL: ds.baseURL()}
	settingsPath, _, err := ds.paths()
	if err != nil {
		return status, err
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return status, nil
	}
	env, _ := payload["env"].(map[string]any)
	if env == nil {
		return status, nil
	}
	baseURLVal := anyToString(env["DEEPSEEK_BASE_URL"])
	baseURL := ds.baseURL()
	enabled := strings.EqualFold(
		strings.TrimSuffix(strings.TrimSpace(baseURLVal), "/"),
		strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
	)
	status.Enabled = enabled
	return status, nil
}

func (ds *DeepSeekCodeSettingsService) EnableProxy() error {
	settingsPath, backupPath, err := ds.paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	stateExists, err := ProxyStateExists(deepseekCodePlatform)
	if err != nil {
		return err
	}

	var existingData map[string]interface{}
	fileExisted := false
	if _, statErr := os.Stat(settingsPath); statErr == nil {
		fileExisted = true
		content, readErr := os.ReadFile(settingsPath)
		if readErr != nil {
			return readErr
		}
		if !stateExists {
			if err := os.WriteFile(backupPath, content, 0o600); err != nil {
				return err
			}
		}
		if len(content) > 0 {
			if err := json.Unmarshal(content, &existingData); err != nil {
				fmt.Printf("[警告] settings.json 格式无效，已备份到 %s，将使用空配置: %v\n", backupPath, err)
				existingData = make(map[string]interface{})
			}
		}
		if existingData == nil {
			existingData = make(map[string]interface{})
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		existingData = make(map[string]interface{})
	} else {
		return fmt.Errorf("无法读取 settings.json: %w", statErr)
	}

	if !stateExists {
		envRaw, _ := existingData["env"].(map[string]interface{})
		state := &ProxyState{
			TargetPath:      settingsPath,
			FileExisted:     fileExisted,
			EnvExisted:      envRaw != nil,
			InjectedBaseURL:        ds.baseURL(),
			InjectedAuthToken:      deepseekCodeAuthPlaceholder,
		}
		if envRaw != nil {
			if v, ok := envRaw["DEEPSEEK_BASE_URL"]; ok {
				s := anyToString(v)
				state.OriginalBaseURL = &s
			}
			if v, ok := envRaw["DEEPSEEK_API_KEY"]; ok {
				s := anyToString(v)
				state.OriginalAuthToken = &s
			}
		}
		if err := SaveProxyState(deepseekCodePlatform, state); err != nil {
			return err
		}
	}

	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["DEEPSEEK_BASE_URL"] = ds.baseURL()
	env["DEEPSEEK_API_KEY"] = deepseekCodeAuthPlaceholder
	existingData["env"] = env

	return AtomicWriteJSON(settingsPath, existingData)
}

func (ds *DeepSeekCodeSettingsService) DisableProxy() error {
	settingsPath, _, err := ds.paths()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DeleteProxyState(deepseekCodePlatform)
		}
		return err
	}

	payload := make(map[string]interface{})
	if len(content) > 0 {
		if err := json.Unmarshal(content, &payload); err != nil {
			return fmt.Errorf("settings.json 解析失败，请检查文件格式: %w", err)
		}
	}
	if payload == nil {
		payload = make(map[string]interface{})
	}

	state, stateErr := LoadProxyState(deepseekCodePlatform)
	if stateErr != nil {
		env, _ := payload["env"].(map[string]interface{})
		if env == nil {
			return DeleteProxyState(deepseekCodePlatform)
		}

		changed := false
		proxyBaseURL := ds.baseURL()
		currentBaseURL := anyToString(env["DEEPSEEK_BASE_URL"])
		if strings.EqualFold(
			strings.TrimSuffix(strings.TrimSpace(currentBaseURL), "/"),
			strings.TrimSuffix(strings.TrimSpace(proxyBaseURL), "/"),
		) {
			delete(env, "DEEPSEEK_BASE_URL")
			changed = true
		}
		if anyToString(env["DEEPSEEK_API_KEY"]) == deepseekCodeAuthPlaceholder {
			delete(env, "DEEPSEEK_API_KEY")
			changed = true
		}

		if changed {
			payload["env"] = env
			if err := AtomicWriteJSON(settingsPath, payload); err != nil {
				return err
			}
		}

		return DeleteProxyState(deepseekCodePlatform)
	}

	env, _ := payload["env"].(map[string]interface{})
	if env == nil {
		env = make(map[string]interface{})
	}

	if state.OriginalBaseURL != nil {
		env["DEEPSEEK_BASE_URL"] = *state.OriginalBaseURL
	} else {
		delete(env, "DEEPSEEK_BASE_URL")
	}

	if state.OriginalAuthToken != nil {
		env["DEEPSEEK_API_KEY"] = *state.OriginalAuthToken
	} else {
		delete(env, "DEEPSEEK_API_KEY")
	}

	if len(env) == 0 && !state.EnvExisted {
		delete(payload, "env")
	} else {
		payload["env"] = env
	}

	if err := AtomicWriteJSON(settingsPath, payload); err != nil {
		return err
	}

	return DeleteProxyState(deepseekCodePlatform)
}

func (ds *DeepSeekCodeSettingsService) paths() (settingsPath string, backupPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, deepseekCodeSettingsDir)
	return filepath.Join(dir, deepseekCodeSettingsFileName), filepath.Join(dir, deepseekCodeBackupFileName), nil
}

func (ds *DeepSeekCodeSettingsService) baseURL() string {
	addr := strings.TrimSpace(ds.relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host
}

// ApplySingleProvider 直连应用单一供应商（仅在代理关闭时可用）
func (ds *DeepSeekCodeSettingsService) ApplySingleProvider(providerID int64) error {
	proxyStatus, err := ds.ProxyStatus()
	if err != nil {
		return fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return fmt.Errorf("本地代理已启用，请先关闭代理再进行直接应用")
	}

	providers, err := loadProviderSnapshot(deepseekCodePlatform)
	if err != nil {
		return fmt.Errorf("加载供应商配置失败: %w", err)
	}

	provider, found := findProviderByID(providers, providerID)
	if !found {
		return fmt.Errorf("未找到 ID 为 %d 的供应商", providerID)
	}

	if provider.APIURL == "" {
		return fmt.Errorf("供应商 '%s' 未配置 API 地址", provider.Name)
	}
	if provider.APIKey == "" {
		return fmt.Errorf("供应商 '%s' 未配置 API 密钥", provider.Name)
	}

	settingsPath, _, err := ds.paths()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	if _, err := CreateBackup(settingsPath); err != nil {
		fmt.Printf("[DeepSeekCodeSettingsService] 备份失败（非阻塞）: %v\n", err)
	}

	existingData := make(map[string]interface{})
	if data, readErr := os.ReadFile(settingsPath); readErr == nil && len(data) > 0 {
		if unmarshalErr := json.Unmarshal(data, &existingData); unmarshalErr != nil {
			return fmt.Errorf("settings.json 解析失败，请检查文件格式: %w", unmarshalErr)
		}
	}

	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["DEEPSEEK_BASE_URL"] = normalizeURLTrimSlash(provider.APIURL)
	env["DEEPSEEK_API_KEY"] = provider.APIKey
	existingData["env"] = env

	return AtomicWriteJSON(settingsPath, existingData)
}

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
func (ds *DeepSeekCodeSettingsService) GetDirectAppliedProviderID() (*int64, error) {
	proxyStatus, err := ds.ProxyStatus()
	if err != nil {
		return nil, fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return nil, nil
	}

	settingsPath, _, err := ds.paths()
	if err != nil {
		return nil, fmt.Errorf("获取配置路径失败: %w", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, nil
	}

	env, _ := payload["env"].(map[string]interface{})
	if env == nil {
		return nil, nil
	}

	currentURL := anyToString(env["DEEPSEEK_BASE_URL"])
	currentKey := anyToString(env["DEEPSEEK_API_KEY"])

	if currentURL == "" {
		return nil, nil
	}

	providers, err := loadProviderSnapshot(deepseekCodePlatform)
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}

	for _, p := range providers {
		if urlsEqualFold(p.APIURL, currentURL) && p.APIKey == currentKey {
			id := p.ID
			return &id, nil
		}
	}

	return nil, nil
}
