package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ConnectivityTestService 提供供应商编辑页的单次连接测试。
type ConnectivityTestService struct {
	appSettings *AppSettingsService
}

func NewConnectivityTestService(appSettings *AppSettingsService) *ConnectivityTestService {
	return &ConnectivityTestService{appSettings: appSettings}
}

// getEffectiveEndpoint 获取单次连接测试使用的端点。
func (cts *ConnectivityTestService) getEffectiveEndpoint(provider *Provider, platform string) string {
	endpoint := strings.TrimSpace(provider.ConnectivityTestEndpoint)
	if endpoint != "" {
		return resolveProviderTestEndpoint(platform, *provider, endpoint)
	}
	return resolveProviderTestEndpoint(platform, *provider, "")
}

// buildTargetURL 根据用户配置的端点构建目标 URL
func (cts *ConnectivityTestService) buildTargetURL(provider *Provider, platform string) string {
	baseURL := strings.TrimSuffix(provider.APIURL, "/")
	endpoint := cts.getEffectiveEndpoint(provider, platform)
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	return baseURL + endpoint
}

// IsTimeoutError 检测错误是否为超时类型
// 超时包括：context.DeadlineExceeded、net.Error.Timeout()、以及错误消息中包含 timeout 的情况
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// 检查 context.DeadlineExceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// 检查 os.ErrDeadlineExceeded（Go 1.15+）
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	// 检查 net.Error 接口的 Timeout() 方法
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// 检查错误消息（兜底方案）
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline exceeded") ||
		strings.Contains(errMsg, "context canceled")
}

// ManualTestResult 手动测试结果
type ManualTestResult struct {
	Success   bool   `json:"success"`
	LatencyMs int    `json:"latencyMs"`
	HTTPCode  int    `json:"httpCode"`
	Message   string `json:"message"`
}

func (cts *ConnectivityTestService) probeProviderLatency(
	ctx context.Context,
	provider Provider,
	platform string,
) ManualTestResult {
	result := ManualTestResult{
		Success: false,
	}

	targetURL := cts.buildTargetURL(&provider, platform)
	proxyConfig := ProxyConfig{}
	var err error
	if provider.ProxyEnabled {
		if cts.appSettings == nil {
			result.Message = "代理配置服务未初始化"
			return result
		}
		proxyConfig, err = cts.appSettings.GetProviderProxyConfig(true)
		if err != nil {
			result.Message = fmt.Sprintf("读取代理配置失败: %v", err)
			return result
		}
	}

	requestFactory := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}
		headers, err := BuildUpstreamHeaders(
			provider,
			platform,
			map[string]string{"User-Agent": "code-switch-R"},
			ResolveProviderUpstreamProtocol(platform, provider, cts.getEffectiveEndpoint(&provider, platform)),
		)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		req.Header.Set("Accept", "application/json, text/plain, */*")
		return req, nil
	}

	start := time.Now()
	resp, usedProxyConfig, err := doProxyAwareRequest(
		15*time.Second,
		&http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 5,
		},
		proxyConfig,
		requestFactory,
	)
	result.LatencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		message := DescribeProxyTransportError(err, usedProxyConfig)
		if !provider.ProxyEnabled {
			message = fmt.Sprintf("网络错误: %s", message)
		}
		result.Message = message
		return result
	}
	defer resp.Body.Close()

	result.Success = resp.StatusCode != http.StatusProxyAuthRequired
	result.HTTPCode = resp.StatusCode

	protocolNote := ""
	if provider.ProxyEnabled {
		normalized := normalizeProxyConfig(proxyConfig)
		if usedProxyConfig.Protocol != normalized.Protocol {
			protocolNote = fmt.Sprintf("，自动识别为 %s", strings.ToUpper(usedProxyConfig.Protocol))
		}
	}

	if resp.StatusCode == http.StatusProxyAuthRequired {
		result.Message = "代理需要认证，当前配置不可直接使用"
		return result
	}

	result.Message = fmt.Sprintf("延迟 %dms%s，接口返回 HTTP %d", result.LatencyMs, protocolNote, resp.StatusCode)
	return result
}

// TestProviderManual 手动做供应商延迟检查（供前端按钮调用）
func (cts *ConnectivityTestService) TestProviderManual(
	platform string,
	apiURL string,
	apiKey string,
	endpoint string,
	authType string,
	proxyEnabled bool,
) ManualTestResult {
	// 调试日志：打印前端传递的参数
	fmt.Printf("[DEBUG] TestProviderManual 收到参数:\n")
	fmt.Printf("  platform: %q\n", platform)
	fmt.Printf("  apiURL:   %q\n", apiURL)
	fmt.Printf("  apiKey:   %q (len=%d)\n", maskAPIKey(apiKey), len(apiKey))
	fmt.Printf("  endpoint: %q\n", endpoint)
	fmt.Printf("  authType: %q\n", authType)

	// 平台参数校验
	if platform == "" {
		platform = "claude"
	}
	if strings.EqualFold(strings.TrimSpace(platform), "pi") {
		return ManualTestResult{Message: "Pi 平台不支持连通性测试"}
	}

	// 构建临时 Provider
	provider := Provider{
		APIURL:                   apiURL,
		APIKey:                   apiKey,
		ConnectivityTestEndpoint: endpoint,
		ConnectivityAuthType:     authType,
		ProxyEnabled:             proxyEnabled,
	}
	if strings.EqualFold(strings.TrimSpace(platform), "opencode") {
		endpointLower := strings.ToLower(strings.TrimSpace(endpoint))
		switch {
		case strings.Contains(endpointLower, "/v1beta"):
			provider.UpstreamProtocol = string(UpstreamProtocolGoogle)
		case strings.Contains(endpointLower, "/responses"):
			provider.UpstreamProtocol = string(UpstreamProtocolOpenAIResponses)
		case strings.Contains(endpointLower, "/chat/completions"):
			provider.UpstreamProtocol = string(UpstreamProtocolOpenAIChat)
		default:
			provider.UpstreamProtocol = string(UpstreamProtocolAnthropic)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return cts.probeProviderLatency(ctx, provider, platform)
}

// maskAPIKey 隐藏 API Key 的中间部分，用于安全日志输出
func maskAPIKey(key string) string {
	if len(key) <= 10 {
		return "***"
	}
	return key[:6] + "..." + key[len(key)-4:]
}
