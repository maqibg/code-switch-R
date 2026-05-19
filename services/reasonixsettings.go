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
	reasonixSettingsDir      = ".reasonix"
	reasonixSettingsFileName = "config.json"
	reasonixBackupFileName   = "cc-studio.back.config.json"
	reasonixPlatform         = "reasonix"
	reasonixAuthPlaceholder  = "code-switch-r"
)

type ReasonixProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

type ReasonixSettingsService struct {
	relayAddr string
}

func NewReasonixSettingsService(relayAddr string) *ReasonixSettingsService {
	return &ReasonixSettingsService{relayAddr: relayAddr}
}

func (rs *ReasonixSettingsService) ProxyStatus() (ReasonixProxyStatus, error) {
	status := ReasonixProxyStatus{Enabled: false, BaseURL: rs.baseURL()}
	settingsPath, _, err := rs.paths()
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
	baseURLVal := anyToString(payload["baseUrl"])
	baseURL := rs.baseURL()
	enabled := strings.EqualFold(
		strings.TrimSuffix(strings.TrimSpace(baseURLVal), "/"),
		strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
	)
	status.Enabled = enabled
	return status, nil
}

func (rs *ReasonixSettingsService) EnableProxy() error {
	settingsPath, backupPath, err := rs.paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	stateExists, err := ProxyStateExists(reasonixPlatform)
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
				fmt.Printf("[警告] config.json 格式无效，已备份到 %s，将使用空配置: %v\n", backupPath, err)
				existingData = make(map[string]interface{})
			}
		}
		if existingData == nil {
			existingData = make(map[string]interface{})
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		existingData = make(map[string]interface{})
	} else {
		return fmt.Errorf("无法读取 config.json: %w", statErr)
	}

	if !stateExists {
		state := &ProxyState{
			TargetPath:        settingsPath,
			FileExisted:       fileExisted,
			EnvExisted:        true, // Reasonix 扁平结构，字段始终"存在"于顶层
			InjectedBaseURL:   rs.baseURL(),
			InjectedAuthToken: reasonixAuthPlaceholder,
		}
		if v, ok := existingData["baseUrl"]; ok {
			s := anyToString(v)
			state.OriginalBaseURL = &s
		}
		if v, ok := existingData["apiKey"]; ok {
			s := anyToString(v)
			state.OriginalAuthToken = &s
		}
		if err := SaveProxyState(reasonixPlatform, state); err != nil {
			return err
		}
	}

	existingData["baseUrl"] = rs.baseURL()
	existingData["apiKey"] = reasonixAuthPlaceholder

	return AtomicWriteJSON(settingsPath, existingData)
}

func (rs *ReasonixSettingsService) DisableProxy() error {
	settingsPath, _, err := rs.paths()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DeleteProxyState(reasonixPlatform)
		}
		return err
	}

	payload := make(map[string]interface{})
	if len(content) > 0 {
		if err := json.Unmarshal(content, &payload); err != nil {
			return fmt.Errorf("config.json 解析失败，请检查文件格式: %w", err)
		}
	}
	if payload == nil {
		payload = make(map[string]interface{})
	}

	state, stateErr := LoadProxyState(reasonixPlatform)
	if stateErr != nil {
		changed := false
		proxyBaseURL := rs.baseURL()
		currentBaseURL := anyToString(payload["baseUrl"])
		if strings.EqualFold(
			strings.TrimSuffix(strings.TrimSpace(currentBaseURL), "/"),
			strings.TrimSuffix(strings.TrimSpace(proxyBaseURL), "/"),
		) {
			delete(payload, "baseUrl")
			changed = true
		}
		if anyToString(payload["apiKey"]) == reasonixAuthPlaceholder {
			delete(payload, "apiKey")
			changed = true
		}

		if changed {
			if err := AtomicWriteJSON(settingsPath, payload); err != nil {
				return err
			}
		}

		return DeleteProxyState(reasonixPlatform)
	}

	if state.OriginalBaseURL != nil {
		payload["baseUrl"] = *state.OriginalBaseURL
	} else {
		delete(payload, "baseUrl")
	}

	if state.OriginalAuthToken != nil {
		payload["apiKey"] = *state.OriginalAuthToken
	} else {
		delete(payload, "apiKey")
	}

	if err := AtomicWriteJSON(settingsPath, payload); err != nil {
		return err
	}

	return DeleteProxyState(reasonixPlatform)
}

func (rs *ReasonixSettingsService) ApplySingleProvider(providerID int64) error {
	proxyStatus, err := rs.ProxyStatus()
	if err != nil {
		return fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return fmt.Errorf("本地代理已启用，请先关闭代理再进行直接应用")
	}

	providers, err := loadProviderSnapshot(reasonixPlatform)
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

	settingsPath, _, err := rs.paths()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	if _, err := CreateBackup(settingsPath); err != nil {
		fmt.Printf("[ReasonixSettingsService] 备份失败（非阻塞）: %v\n", err)
	}

	existingData := make(map[string]interface{})
	if data, readErr := os.ReadFile(settingsPath); readErr == nil && len(data) > 0 {
		if unmarshalErr := json.Unmarshal(data, &existingData); unmarshalErr != nil {
			return fmt.Errorf("config.json 解析失败，请检查文件格式: %w", unmarshalErr)
		}
	}

	existingData["baseUrl"] = normalizeURLTrimSlash(provider.APIURL)
	existingData["apiKey"] = provider.APIKey

	return AtomicWriteJSON(settingsPath, existingData)
}

func (rs *ReasonixSettingsService) GetDirectAppliedProviderID() (*int64, error) {
	proxyStatus, err := rs.ProxyStatus()
	if err != nil {
		return nil, fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus.Enabled {
		return nil, nil
	}

	settingsPath, _, err := rs.paths()
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

	currentURL := anyToString(payload["baseUrl"])
	currentKey := anyToString(payload["apiKey"])

	if currentURL == "" {
		return nil, nil
	}

	providers, err := loadProviderSnapshot(reasonixPlatform)
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

func (rs *ReasonixSettingsService) paths() (settingsPath string, backupPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, reasonixSettingsDir)
	return filepath.Join(dir, reasonixSettingsFileName), filepath.Join(dir, reasonixBackupFileName), nil
}

func (rs *ReasonixSettingsService) baseURL() string {
	addr := strings.TrimSpace(rs.relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimSuffix(addr, "/") + "/reasonix"
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return strings.TrimSuffix(host, "/") + "/reasonix"
}
