package services

const (
	reasonixSettingsDir      = ".reasonix"
	reasonixSettingsFileName = "config.json"
	reasonixBackupFileName   = "cc-studio.back.config.json"
	reasonixPlatform         = "reasonix"
)

type ReasonixProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

// ReasonixSettingsService 管理 ~/.reasonix/config.json 的代理启停与直连应用。
//
// 具体逻辑由 jsonProxyPlatform 通用实现承接（见 json_proxy_settings.go），
// 这里只保留对外方法签名——它们注册进 Wails 绑定供前端调用。
type ReasonixSettingsService struct {
	relayAddr string
}

func NewReasonixSettingsService(relayAddr string) *ReasonixSettingsService {
	return &ReasonixSettingsService{relayAddr: relayAddr}
}

func (rs *ReasonixSettingsService) ProxyStatus() (ReasonixProxyStatus, error) {
	enabled, baseURL, err := reasonixProxyPlatform.proxyStatus(rs.relayAddr)
	return ReasonixProxyStatus{Enabled: enabled, BaseURL: baseURL}, err
}

func (rs *ReasonixSettingsService) EnableProxy() error {
	return reasonixProxyPlatform.enableProxy(rs.relayAddr)
}

func (rs *ReasonixSettingsService) DisableProxy() error {
	return reasonixProxyPlatform.disableProxy(rs.relayAddr)
}

func (rs *ReasonixSettingsService) ApplySingleProvider(providerID int64) error {
	return reasonixProxyPlatform.applySingleProvider(rs.relayAddr, providerID)
}

func (rs *ReasonixSettingsService) GetDirectAppliedProviderID() (*int64, error) {
	return reasonixProxyPlatform.getDirectAppliedProviderID(rs.relayAddr)
}

func (rs *ReasonixSettingsService) baseURL() string {
	return reasonixProxyPlatform.baseURL(rs.relayAddr)
}
