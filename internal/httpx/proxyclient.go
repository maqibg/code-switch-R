// Package httpx 提供代理感知的出站 HTTP 客户端。
//
// 全局代理配置的形状（ProxyConfig）、按配置构造客户端、代理协议错配的
// 识别与自动回退都在这里。app 设置、定价下载、连通性测试、转发层
// 共用这一套，不各自造客户端。
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"

	"codeswitch/internal/infra"
)

const (
	DefaultProxyProtocol = "http"
	DefaultProxyHost     = "127.0.0.1"
	DefaultProxyPort     = 7890
	defaultProxyTestURL  = "https://www.gstatic.com/generate_204"
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

func normalizeProxyProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http", "https", "socks5":
		return strings.ToLower(strings.TrimSpace(protocol))
	default:
		return DefaultProxyProtocol
	}
}

// NormalizeProxyConfig 补齐空字段并归一化协议名
func NormalizeProxyConfig(config ProxyConfig) ProxyConfig {
	host := strings.TrimSpace(config.Host)
	if host == "" {
		host = DefaultProxyHost
	}
	port := config.Port
	if port <= 0 || port > 65535 {
		port = DefaultProxyPort
	}
	return ProxyConfig{
		Enabled:  config.Enabled,
		Protocol: normalizeProxyProtocol(config.Protocol),
		Host:     host,
		Port:     port,
	}
}

// Address 返回 host:port 形式的代理地址
func (config ProxyConfig) Address() string {
	return net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
}

func (config ProxyConfig) URL() (*url.URL, error) {
	normalized := NormalizeProxyConfig(config)
	return url.Parse(fmt.Sprintf("%s://%s", normalized.Protocol, normalized.Address()))
}

func cloneHTTPTransport(base *http.Transport) *http.Transport {
	transport := &http.Transport{
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
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

type proxyTransportKey struct {
	enabled  bool
	protocol string
	host     string
	port     int
}

var sharedProxyTransports sync.Map

func newProxyTransport(baseTransport *http.Transport, config ProxyConfig) (*http.Transport, error) {
	transport := cloneHTTPTransport(baseTransport)
	transport.Proxy = nil
	transport.DialContext = nil
	transport.DialTLSContext = nil

	normalized := NormalizeProxyConfig(config)
	if !normalized.Enabled {
		return transport, nil
	}
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
	return transport, nil
}

func sharedProxyTransport(config ProxyConfig) (*http.Transport, error) {
	normalized := NormalizeProxyConfig(config)
	key := proxyTransportKey{
		enabled:  normalized.Enabled,
		protocol: normalized.Protocol,
		host:     normalized.Host,
		port:     normalized.Port,
	}
	if cached, ok := sharedProxyTransports.Load(key); ok {
		return cached.(*http.Transport), nil
	}
	transport, err := newProxyTransport(nil, normalized)
	if err != nil {
		return nil, err
	}
	actual, _ := sharedProxyTransports.LoadOrStore(key, transport)
	return actual.(*http.Transport), nil
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

// NewHTTPClientWithProxy 按代理配置构造客户端。
// baseTransport 为 nil 时复用按配置缓存的共享 transport。
func NewHTTPClientWithProxy(timeout time.Duration, baseTransport *http.Transport, config ProxyConfig) (*http.Client, error) {
	var transport *http.Transport
	var err error
	if baseTransport == nil {
		transport, err = sharedProxyTransport(config)
	} else {
		transport, err = newProxyTransport(baseTransport, config)
	}
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

// DescribeProxyTransportError 把常见的代理协议错配翻译成可操作的提示
func DescribeProxyTransportError(err error, config ProxyConfig) string {
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

	normalized := NormalizeProxyConfig(config)
	addr := normalized.Address()

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

	normalized := NormalizeProxyConfig(config)
	if !normalized.Enabled {
		return ProxyConfig{}, "", false
	}

	raw := strings.TrimSpace(err.Error())
	addr := normalized.Address()

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
	normalized := NormalizeProxyConfig(config)
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

// DoProxyAwareRequest 发起请求；代理协议错配时按候选协议自动重试。
// requestFactory 每次重试都会被调用（*http.Request 不能复用）。
func DoProxyAwareRequest(
	timeout time.Duration,
	baseTransport *http.Transport,
	config ProxyConfig,
	requestFactory func() (*http.Request, error),
) (*http.Response, ProxyConfig, error) {
	normalized := NormalizeProxyConfig(config)
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
	parts := []string{fmt.Sprintf("首次失败：%s", DescribeProxyTransportError(err, normalized))}
	for _, retryConfig := range candidates {
		resp, retryErr := run(retryConfig)
		if retryErr == nil {
			infra.LogInfo("代理协议自动切换成功", "proxy", normalized.Address(), "protocol", strings.ToUpper(retryConfig.Protocol))
			return resp, retryConfig, nil
		}
		parts = append(parts, fmt.Sprintf("切换 %s 失败：%s", strings.ToUpper(retryConfig.Protocol), DescribeProxyTransportError(retryErr, retryConfig)))
	}

	return nil, normalized, errors.New(strings.Join(parts, "；"))
}

// TestProxyConfig 探测代理连通性（设置页的手动测试按钮）
func TestProxyConfig(config ProxyConfig) ProxyTestResult {
	result := ProxyTestResult{
		Success:   false,
		TestedURL: defaultProxyTestURL,
	}

	start := time.Now()
	resp, usedConfig, err := DoProxyAwareRequest(
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
		result.Message = DescribeProxyTransportError(err, NormalizeProxyConfig(config))
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
		if usedConfig.Protocol != NormalizeProxyConfig(config).Protocol {
			result.Message = fmt.Sprintf("代理连接成功（%dms，自动识别为 %s）", result.LatencyMs, strings.ToUpper(usedConfig.Protocol))
		} else {
			result.Message = fmt.Sprintf("代理连接成功（%dms）", result.LatencyMs)
		}
		return result
	}

	if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
		result.Success = true
		if usedConfig.Protocol != NormalizeProxyConfig(config).Protocol {
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
