package services

import "fmt"

const (
	claudeSettingsDir      = ".claude"
	claudeSettingsFileName = "settings.json"
	claudeBackupFileName   = "cc-studio.back.settings.json"
)

type ClaudeProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

// ClaudeSettingsService 管理 ~/.claude/settings.json 的代理启停与直连应用。
//
// 具体逻辑由 jsonProxyPlatform 通用实现承接（见 json_proxy_settings.go），
// 这里只保留对外方法签名——它们注册进 Wails 绑定供前端调用。
type ClaudeSettingsService struct {
	relayAddr string
}

func NewClaudeSettingsService(relayAddr string) *ClaudeSettingsService {
	return &ClaudeSettingsService{relayAddr: relayAddr}
}

func (css *ClaudeSettingsService) ProxyStatus() (ClaudeProxyStatus, error) {
	enabled, baseURL, err := claudeProxyPlatform.proxyStatus(css.relayAddr)
	return ClaudeProxyStatus{Enabled: enabled, BaseURL: baseURL}, err
}

func (css *ClaudeSettingsService) EnableProxy() error {
	return claudeProxyPlatform.enableProxy(css.relayAddr)
}

func (css *ClaudeSettingsService) DisableProxy() error {
	return claudeProxyPlatform.disableProxy(css.relayAddr)
}

// ApplySingleProvider 参数是 int 而非 int64：这是既有的对外绑定签名，
// 前端依赖它，改动会连带改前端，因此保留。
func (css *ClaudeSettingsService) ApplySingleProvider(providerID int) error {
	return claudeProxyPlatform.applySingleProvider(css.relayAddr, int64(providerID))
}

func (css *ClaudeSettingsService) GetDirectAppliedProviderID() (*int64, error) {
	return claudeProxyPlatform.getDirectAppliedProviderID(css.relayAddr)
}

func (css *ClaudeSettingsService) baseURL() string {
	return claudeProxyPlatform.baseURL(css.relayAddr)
}

// anyToString 把 JSON 解出来的任意值转成字符串。
// 定义在这里是历史位置，多个平台的 settings 逻辑都在用。
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
