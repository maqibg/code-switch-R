package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

const proxyStateVersion = 1

const (
	defaultGlobalProxyProtocol = "http"
	defaultGlobalProxyHost     = "127.0.0.1"
	defaultGlobalProxyPort     = 7890
	defaultProxyTestURL        = "https://www.gstatic.com/generate_204"
)

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

// ProxyState 记录代理启用前的基线信息，用于禁用代理时做"手术式"恢复，避免回滚整文件导致用户配置丢失。
// 设计原则：
// 1. 使用指针表示"是否存在"：nil 表示启用代理前不存在该键
// 2. 记录注入值用于禁用时判断"当前值是否仍为我们注入的"
// 3. 代理状态存储在程序目录的 .code-switch-R/proxy-state/{platform}.json，避免写回用户目录旧路径
type ProxyState struct {
	Version           int     `json:"version"`
	CreatedAt         string  `json:"created_at"`
	TargetPath        string  `json:"target_path"`
	FileExisted       bool    `json:"file_existed"`
	EnvExisted        bool    `json:"env_existed"`
	OriginalBaseURL   *string `json:"original_base_url,omitempty"`
	OriginalAuthToken *string `json:"original_auth_token,omitempty"`
	InjectedBaseURL   string  `json:"injected_base_url"`
	InjectedAuthToken string  `json:"injected_auth_token"`

	// ========== Codex 专用字段 ==========
	// Codex 使用 TOML 配置，结构更复杂，需要额外字段

	// AuthFilePath: Codex 的 auth.json 路径
	AuthFilePath string `json:"auth_file_path,omitempty"`
	// AuthFileExisted: auth.json 是否在启用代理前存在
	AuthFileExisted bool `json:"auth_file_existed,omitempty"`
	// OriginalModelProvider: model_provider 的原始值
	OriginalModelProvider *string `json:"original_model_provider,omitempty"`
	// OriginalPreferredAuth: preferred_auth_method 的原始值
	OriginalPreferredAuth *string `json:"original_preferred_auth,omitempty"`
	// InjectedProviderKey: 注入的 model_providers 键名（如 "code-switch-r"）
	InjectedProviderKey string `json:"injected_provider_key,omitempty"`
	// ModelProvidersKeyExisted: model_providers.{key} 是否在启用前存在
	ModelProvidersKeyExisted bool `json:"model_providers_key_existed,omitempty"`
}

// normalizeProxyPlatform 对 platform 做最小安全校验，避免路径穿越等问题。
func normalizeProxyPlatform(platform string) (string, error) {
	p := strings.TrimSpace(strings.ToLower(platform))
	if p == "" {
		return "", fmt.Errorf("platform 不能为空")
	}
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("非法 platform: %s", platform)
	}
	return p, nil
}

// GetProxyStatePath 返回状态文件路径：<exeDir>/.code-switch-R/proxy-state/{platform}.json
func GetProxyStatePath(platform string) (string, error) {
	p, err := normalizeProxyPlatform(platform)
	if err != nil {
		return "", err
	}
	configDir, err := getAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "proxy-state", p+".json"), nil
}

// ProxyStateExists 检查指定平台的代理状态文件是否存在
func ProxyStateExists(platform string) (bool, error) {
	path, err := GetProxyStatePath(platform)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// LoadProxyState 读取并解析指定平台的代理状态文件。
func LoadProxyState(platform string) (*ProxyState, error) {
	path, err := GetProxyStatePath(platform)
	if err != nil {
		return nil, err
	}

	var state ProxyState
	if err := ReadJSONFile(path, &state); err != nil {
		return nil, err
	}

	if strings.TrimSpace(state.TargetPath) == "" {
		return nil, fmt.Errorf("代理状态文件无效: target_path 为空")
	}

	return &state, nil
}

// SaveProxyState 保存代理状态文件（原子写入）。
func SaveProxyState(platform string, state *ProxyState) error {
	if state == nil {
		return fmt.Errorf("state 不能为空")
	}

	path, err := GetProxyStatePath(platform)
	if err != nil {
		return err
	}

	if state.Version == 0 {
		state.Version = proxyStateVersion
	}
	if strings.TrimSpace(state.CreatedAt) == "" {
		state.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(state.TargetPath) == "" {
		return fmt.Errorf("state.target_path 不能为空")
	}

	// 确保目录存在（权限收敛：Unix 下 0700，Windows 下无影响）
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建代理状态目录失败: %w", err)
	}

	return AtomicWriteJSON(path, state)
}

// DeleteProxyState 删除指定平台的代理状态文件（不存在则忽略）。
func DeleteProxyState(platform string) error {
	path, err := GetProxyStatePath(platform)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

func normalizeProxyProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http", "https", "socks5":
		return strings.ToLower(strings.TrimSpace(protocol))
	default:
		return defaultGlobalProxyProtocol
	}
}

func normalizeProxyConfig(config ProxyConfig) ProxyConfig {
	host := strings.TrimSpace(config.Host)
	if host == "" {
		host = defaultGlobalProxyHost
	}
	port := config.Port
	if port <= 0 || port > 65535 {
		port = defaultGlobalProxyPort
	}
	return ProxyConfig{
		Enabled:  config.Enabled,
		Protocol: normalizeProxyProtocol(config.Protocol),
		Host:     host,
		Port:     port,
	}
}

func normalizeAppProxySettings(settings AppSettings) AppSettings {
	config := normalizeProxyConfig(settings.GlobalProxyConfig())
	settings.GlobalProxyProtocol = config.Protocol
	settings.GlobalProxyHost = config.Host
	settings.GlobalProxyPort = config.Port
	return settings
}

func (settings AppSettings) GlobalProxyConfig() ProxyConfig {
	return normalizeProxyConfig(ProxyConfig{
		Enabled:  settings.GlobalProxyEnabled,
		Protocol: settings.GlobalProxyProtocol,
		Host:     settings.GlobalProxyHost,
		Port:     settings.GlobalProxyPort,
	})
}

func (config ProxyConfig) address() string {
	return net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
}

func (config ProxyConfig) URL() (*url.URL, error) {
	normalized := normalizeProxyConfig(config)
	return url.Parse(fmt.Sprintf("%s://%s", normalized.Protocol, normalized.address()))
}

func cloneHTTPTransport(base *http.Transport) *http.Transport {
	transport := &http.Transport{
		ForceAttemptHTTP2: false,
	}

	if base == nil {
		return transport
	}

	transport.Proxy = base.Proxy
	transport.GetProxyConnectHeader = base.GetProxyConnectHeader
	transport.DialContext = base.DialContext
	transport.DialTLSContext = base.DialTLSContext
	transport.DisableKeepAlives = base.DisableKeepAlives
	transport.DisableCompression = base.DisableCompression
	transport.MaxIdleConns = base.MaxIdleConns
	transport.MaxIdleConnsPerHost = base.MaxIdleConnsPerHost
	transport.MaxConnsPerHost = base.MaxConnsPerHost
	transport.IdleConnTimeout = base.IdleConnTimeout
	transport.ResponseHeaderTimeout = base.ResponseHeaderTimeout
	transport.ExpectContinueTimeout = base.ExpectContinueTimeout
	transport.TLSHandshakeTimeout = base.TLSHandshakeTimeout
	transport.MaxResponseHeaderBytes = base.MaxResponseHeaderBytes
	transport.WriteBufferSize = base.WriteBufferSize
	transport.ReadBufferSize = base.ReadBufferSize
	transport.ProxyConnectHeader = base.ProxyConnectHeader.Clone()
	transport.TLSClientConfig = base.TLSClientConfig
	return transport
}

func dialContextFromProxyDialer(dialer xproxy.Dialer) func(context.Context, string, string) (net.Conn, error) {
	if contextDialer, ok := dialer.(xproxy.ContextDialer); ok {
		return contextDialer.DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		type dialResult struct {
			conn net.Conn
			err  error
		}
		resultCh := make(chan dialResult, 1)
		go func() {
			conn, err := dialer.Dial(network, addr)
			resultCh <- dialResult{conn: conn, err: err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-resultCh:
			return result.conn, result.err
		}
	}
}

func NewHTTPClientWithProxy(timeout time.Duration, baseTransport *http.Transport, config ProxyConfig) (*http.Client, error) {
	transport := cloneHTTPTransport(baseTransport)
	transport.Proxy = nil
	transport.DialContext = nil
	transport.DialTLSContext = nil

	normalized := normalizeProxyConfig(config)
	if normalized.Enabled {
		proxyURL, err := normalized.URL()
		if err != nil {
			return nil, fmt.Errorf("解析代理地址失败: %w", err)
		}

		switch normalized.Protocol {
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
		case "socks5":
			baseDialer := &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			dialer, err := xproxy.FromURL(proxyURL, baseDialer)
			if err != nil {
				return nil, fmt.Errorf("创建 SOCKS5 代理失败: %w", err)
			}
			transport.DialContext = dialContextFromProxyDialer(dialer)
		default:
			return nil, fmt.Errorf("不支持的代理协议: %s", normalized.Protocol)
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

func describeProxyTransportError(err error, config ProxyConfig) string {
	if err == nil {
		return ""
	}

	raw := strings.TrimSpace(err.Error())
	if raw == "" {
		raw = "未知网络错误"
	}
	if !config.Enabled {
		return raw
	}

	normalized := normalizeProxyConfig(config)
	addr := normalized.address()

	if strings.Contains(raw, "malformed HTTP response") && strings.Contains(raw, `\x05`) &&
		(normalized.Protocol == "http" || normalized.Protocol == "https") {
		return fmt.Sprintf(
			"代理协议可能配置错误：当前按 %s 连接 %s，但该端口返回了 SOCKS 数据。请把代理协议改成 SOCKS5 后重试。",
			strings.ToUpper(normalized.Protocol),
			addr,
		)
	}

	if normalized.Protocol == "https" &&
		(strings.Contains(raw, "first record does not look like a TLS handshake") ||
			strings.Contains(raw, "server gave HTTP response to HTTPS client")) {
		return fmt.Sprintf(
			"代理协议可能配置错误：当前按 HTTPS 连接 %s，但该端口看起来是普通 HTTP 代理。请改成 HTTP 后重试。",
			addr,
		)
	}

	if normalized.Protocol == "socks5" &&
		(strings.Contains(raw, "HTTP/1.1") ||
			strings.Contains(raw, "HTTP/1.0") ||
			strings.Contains(raw, "Bad Request") ||
			strings.Contains(raw, "Proxy Authentication Required")) {
		return fmt.Sprintf(
			"代理协议可能配置错误：当前按 SOCKS5 连接 %s，但该端口返回了 HTTP 响应。请改成 HTTP 后重试。",
			addr,
		)
	}

	return raw
}

func alternateProxyConfigForError(err error, config ProxyConfig) (ProxyConfig, string, bool) {
	if err == nil {
		return ProxyConfig{}, "", false
	}

	normalized := normalizeProxyConfig(config)
	if !normalized.Enabled {
		return ProxyConfig{}, "", false
	}

	raw := strings.TrimSpace(err.Error())
	addr := normalized.address()

	if (normalized.Protocol == "http" || normalized.Protocol == "https") &&
		strings.Contains(raw, "malformed HTTP response") && strings.Contains(raw, `\x05`) {
		alt := normalized
		alt.Protocol = "socks5"
		return alt, fmt.Sprintf("检测到 %s 返回了 SOCKS 数据，自动改用 SOCKS5 重试 %s", strings.ToUpper(normalized.Protocol), addr), true
	}

	if normalized.Protocol == "socks5" &&
		(strings.Contains(raw, "HTTP/1.1") ||
			strings.Contains(raw, "HTTP/1.0") ||
			strings.Contains(raw, "Bad Request") ||
			strings.Contains(raw, "Proxy Authentication Required")) {
		alt := normalized
		alt.Protocol = "http"
		return alt, fmt.Sprintf("检测到 SOCKS5 握手收到了 HTTP 响应，自动改用 HTTP 重试 %s", addr), true
	}

	if normalized.Protocol == "https" &&
		(strings.Contains(raw, "first record does not look like a TLS handshake") ||
			strings.Contains(raw, "server gave HTTP response to HTTPS client")) {
		alt := normalized
		alt.Protocol = "http"
		return alt, fmt.Sprintf("检测到 HTTPS 代理握手收到普通 HTTP 响应，自动改用 HTTP 重试 %s", addr), true
	}

	return ProxyConfig{}, "", false
}

func isLoopbackProxyHost(host string) bool {
	normalized := strings.TrimSpace(strings.ToLower(host))
	switch normalized {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func candidateFallbackProxyConfigs(err error, config ProxyConfig) []ProxyConfig {
	normalized := normalizeProxyConfig(config)
	candidates := make([]ProxyConfig, 0, 2)
	add := func(protocol string) {
		if protocol == normalized.Protocol {
			return
		}
		next := normalized
		next.Protocol = protocol
		for _, existing := range candidates {
			if existing.Protocol == next.Protocol {
				return
			}
		}
		candidates = append(candidates, next)
	}

	if alt, _, ok := alternateProxyConfigForError(err, normalized); ok {
		add(alt.Protocol)
	}

	if isLoopbackProxyHost(normalized.Host) {
		add("http")
		add("socks5")
	}

	return candidates
}

func doProxyAwareRequest(
	timeout time.Duration,
	baseTransport *http.Transport,
	config ProxyConfig,
	requestFactory func() (*http.Request, error),
) (*http.Response, ProxyConfig, error) {
	normalized := normalizeProxyConfig(config)
	run := func(cfg ProxyConfig) (*http.Response, error) {
		client, err := NewHTTPClientWithProxy(timeout, baseTransport, cfg)
		if err != nil {
			return nil, err
		}
		req, err := requestFactory()
		if err != nil {
			return nil, err
		}
		return client.Do(req)
	}

	resp, err := run(normalized)
	if err == nil {
		return resp, normalized, nil
	}

	candidates := candidateFallbackProxyConfigs(err, normalized)
	if len(candidates) == 0 {
		return nil, normalized, err
	}
	parts := []string{fmt.Sprintf("首次失败：%s", describeProxyTransportError(err, normalized))}
	for _, retryConfig := range candidates {
		resp, retryErr := run(retryConfig)
		if retryErr == nil {
			fmt.Printf("[Proxy] %s -> 自动切换为 %s 成功\n", normalized.address(), strings.ToUpper(retryConfig.Protocol))
			return resp, retryConfig, nil
		}
		parts = append(parts, fmt.Sprintf("切换 %s 失败：%s", strings.ToUpper(retryConfig.Protocol), describeProxyTransportError(retryErr, retryConfig)))
	}

	return nil, normalized, errors.New(strings.Join(parts, "；"))
}

func TestProxyConfig(config ProxyConfig) ProxyTestResult {
	result := ProxyTestResult{
		Success:   false,
		TestedURL: defaultProxyTestURL,
	}

	start := time.Now()
	resp, usedConfig, err := doProxyAwareRequest(
		10*time.Second,
		nil,
		ProxyConfig{
			Enabled:  true,
			Protocol: config.Protocol,
			Host:     config.Host,
			Port:     config.Port,
		},
		func() (*http.Request, error) {
			req, err := http.NewRequest(http.MethodGet, defaultProxyTestURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "code-switch-R")
			req.Header.Set("Accept", "text/plain")
			return req, nil
		},
	)
	result.LatencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		result.Message = describeProxyTransportError(err, normalizeProxyConfig(config))
		return result
	}
	defer resp.Body.Close()

	result.HTTPCode = resp.StatusCode
	if resp.StatusCode == http.StatusProxyAuthRequired {
		result.Message = "代理需要认证，当前配置不可直接使用"
		return result
	}

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusBadRequest {
		result.Success = true
		if usedConfig.Protocol != normalizeProxyConfig(config).Protocol {
			result.Message = fmt.Sprintf("代理连接成功（%dms，自动识别为 %s）", result.LatencyMs, strings.ToUpper(usedConfig.Protocol))
		} else {
			result.Message = fmt.Sprintf("代理连接成功（%dms）", result.LatencyMs)
		}
		return result
	}

	if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
		result.Success = true
		if usedConfig.Protocol != normalizeProxyConfig(config).Protocol {
			result.Message = fmt.Sprintf("代理链路已连通（%dms，自动识别为 %s，目标返回 HTTP %d）", result.LatencyMs, strings.ToUpper(usedConfig.Protocol), resp.StatusCode)
		} else {
			result.Message = fmt.Sprintf("代理链路已连通（%dms，目标返回 HTTP %d）", result.LatencyMs, resp.StatusCode)
		}
		return result
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	result.Message = fmt.Sprintf("代理测试失败：HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	return result
}
