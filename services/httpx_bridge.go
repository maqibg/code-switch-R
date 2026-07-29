package services

import (
	"net/http"
	"time"

	"codeswitch/internal/httpx"
)

// 迁移期兼容层：代理感知的出站 HTTP 客户端已搬到 codeswitch/internal/httpx
// （A4/A5 拆包第 3 步）。CLI 代理状态文件（ProxyState）仍在本包 proxystate.go。
//
// ProxyConfig / ProxyTestResult 保留为本包的具体结构体而不是类型别名：
// 它们出现在 Wails 注册服务的导出签名里（AppSettingsService 三个方法），
// 绑定生成器会追到类型的声明包——用别名会把模型生成到
// frontend/bindings/codeswitch/internal/httpx/ 下（已实测），
// 门面类型必须留在门面包。两边字段同型，用 Go 的结构体显式转换衔接。

type ProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type ProxyTestResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	LatencyMs int    `json:"latencyMs,omitempty"`
	HTTPCode  int    `json:"httpCode,omitempty"`
	TestedURL string `json:"testedUrl,omitempty"`
}

const (
	defaultGlobalProxyProtocol = httpx.DefaultProxyProtocol
	defaultGlobalProxyHost     = httpx.DefaultProxyHost
	defaultGlobalProxyPort     = httpx.DefaultProxyPort
)

// Address 返回 host:port 形式的代理地址
func (config ProxyConfig) Address() string {
	return httpx.ProxyConfig(config).Address()
}

func NewHTTPClientWithProxy(timeout time.Duration, baseTransport *http.Transport, config ProxyConfig) (*http.Client, error) {
	return httpx.NewHTTPClientWithProxy(timeout, baseTransport, httpx.ProxyConfig(config))
}

func TestProxyConfig(config ProxyConfig) ProxyTestResult {
	return ProxyTestResult(httpx.TestProxyConfig(httpx.ProxyConfig(config)))
}

func normalizeProxyConfig(config ProxyConfig) ProxyConfig {
	return ProxyConfig(httpx.NormalizeProxyConfig(httpx.ProxyConfig(config)))
}

func DescribeProxyTransportError(err error, config ProxyConfig) string {
	return httpx.DescribeProxyTransportError(err, httpx.ProxyConfig(config))
}

func doProxyAwareRequest(
	timeout time.Duration,
	baseTransport *http.Transport,
	config ProxyConfig,
	requestFactory func() (*http.Request, error),
) (*http.Response, ProxyConfig, error) {
	resp, used, err := httpx.DoProxyAwareRequest(timeout, baseTransport, httpx.ProxyConfig(config), requestFactory)
	return resp, ProxyConfig(used), err
}

// Header 工具（原 upstream_policy.go 的纯函数部分）
var (
	blockedUpstreamHeaders     = httpx.BlockedUpstreamHeaders
	canonicalizeHeaderMap      = httpx.CanonicalizeHeaderMap
	validateAdditionalHeader   = httpx.ValidateAdditionalHeader
	validateHeaderNameAndValue = httpx.ValidateHeaderNameAndValue
	setHeader                  = httpx.SetHeader
	removeHeader               = httpx.RemoveHeader
	headerValue                = httpx.HeaderValue
	mergeCommaSeparatedHeader  = httpx.MergeCommaSeparatedHeader
)

// endpoint 工具（原 relay_forward_execution.go 的纯函数部分）
var (
	splitEndpointQuery = httpx.SplitEndpointQuery
	ensureEndpointPath = httpx.EnsureEndpointPath
	endpointWithQuery  = httpx.EndpointWithQuery
)
