package services

import (
	"fmt"
	"net/url"
	"strings"
)

// GeminiCredentialType 是 Gemini Native 与 Gemini CLI 之间的显式认证边界。
// 认证类型来自配置字段，不根据名称、URL 或 Key 前缀推断。
type GeminiCredentialType string

const (
	GeminiCredentialAPIKey        GeminiCredentialType = "gemini_api_key"
	GeminiCredentialNativeOAuth   GeminiCredentialType = "gemini_native_oauth"
	GeminiCredentialVertexAPIKey  GeminiCredentialType = "vertex_api_key"
	GeminiCredentialVertexADC     GeminiCredentialType = "vertex_adc"
	GeminiCredentialVertexService GeminiCredentialType = "vertex_service_account"
	GeminiCredentialGateway       GeminiCredentialType = "gemini_gateway"
	GeminiCredentialCLIOAuth      GeminiCredentialType = "gemini_cli_oauth"
)

type GeminiEndpointKind string

const (
	GeminiEndpointOfficial GeminiEndpointKind = "official"
	GeminiEndpointGateway  GeminiEndpointKind = "gateway"
	GeminiEndpointVertex   GeminiEndpointKind = "vertex"
)

type GeminiEndpointAction string

const (
	GeminiActionModels         GeminiEndpointAction = "models"
	GeminiActionModel          GeminiEndpointAction = "model"
	GeminiActionGenerate       GeminiEndpointAction = "generateContent"
	GeminiActionStreamGenerate GeminiEndpointAction = "streamGenerateContent"
	GeminiActionCountTokens    GeminiEndpointAction = "countTokens"
)

type GeminiEndpointRequest struct {
	Version string
	Model   string
	Action  GeminiEndpointAction
	Query   url.Values
}

func (r GeminiEndpointRequest) IsStream() bool {
	return r.Action == GeminiActionStreamGenerate
}

func (r GeminiEndpointRequest) IsGeneration() bool {
	return r.Action == GeminiActionGenerate || r.Action == GeminiActionStreamGenerate || r.Action == GeminiActionCountTokens
}

// NormalizeGeminiModelID 去掉 Gemini API 常见的 models/ 前缀。
func NormalizeGeminiModelID(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "models/")
}

// ParseGeminiEndpointPath 解析本地 Relay 的 Gemini 路径，不接受任意透传路径。
func ParseGeminiEndpointPath(rawPath string) (GeminiEndpointRequest, error) {
	path := strings.TrimSpace(rawPath)
	if index := strings.IndexByte(path, '?'); index >= 0 {
		path = path[:index]
	}
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "gemini/")
	path = strings.TrimPrefix(path, "gemini")
	path = strings.TrimPrefix(path, "/")

	version := ""
	for _, candidate := range []string{"v1beta", "v1"} {
		if path == candidate || strings.HasPrefix(path, candidate+"/") {
			version = candidate
			path = strings.TrimPrefix(strings.TrimPrefix(path, candidate), "/")
			break
		}
	}
	if version == "" {
		return GeminiEndpointRequest{}, fmt.Errorf("Gemini 路径缺少 v1 或 v1beta 版本")
	}
	if path == "models" {
		return GeminiEndpointRequest{Version: version, Action: GeminiActionModels, Query: url.Values{}}, nil
	}
	if !strings.HasPrefix(path, "models/") {
		return GeminiEndpointRequest{}, fmt.Errorf("Gemini 路径必须位于 models/ 下")
	}

	rest := strings.TrimPrefix(path, "models/")
	// 有些客户端会把 models/ 前缀重复带入路径；规范化后只保留一次。
	rest = strings.TrimPrefix(rest, "models/")
	if rest == "" || strings.Contains(rest, "/") {
		return GeminiEndpointRequest{}, fmt.Errorf("Gemini model ID 无效")
	}
	model := rest
	action := GeminiActionModel
	if index := strings.IndexByte(rest, ':'); index >= 0 {
		model = rest[:index]
		action = GeminiEndpointAction(rest[index+1:])
		switch action {
		case GeminiActionGenerate, GeminiActionStreamGenerate, GeminiActionCountTokens:
		default:
			return GeminiEndpointRequest{}, fmt.Errorf("不支持的 Gemini action: %s", action)
		}
	}
	model, err := url.PathUnescape(NormalizeGeminiModelID(model))
	if err != nil || model == "" {
		return GeminiEndpointRequest{}, fmt.Errorf("Gemini model ID 无效")
	}
	return GeminiEndpointRequest{Version: version, Model: model, Action: action, Query: url.Values{}}, nil
}

// GeminiCredentialType 返回 Provider 的显式 Credential 类型。
// 没有新字段的旧配置只在兼容边界上按旧数据迁移规则处理。
func (p Provider) GeminiCredentialType() GeminiCredentialType {
	if p.gemini != nil && strings.TrimSpace(p.gemini.CredentialType) != "" {
		return GeminiCredentialType(strings.TrimSpace(p.gemini.CredentialType))
	}
	if strings.EqualFold(strings.TrimSpace(p.UpstreamProtocol), string(UpstreamProtocolGoogle)) && p.APIKey != "" {
		return GeminiCredentialAPIKey
	}
	return legacyGeminiCredentialType(p)
}

func legacyGeminiCredentialType(provider Provider) GeminiCredentialType {
	if provider.APIKey != "" {
		return GeminiCredentialAPIKey
	}
	if provider.gemini != nil && strings.EqualFold(provider.gemini.Category, "official") {
		return GeminiCredentialCLIOAuth
	}
	return GeminiCredentialGateway
}

func (p Provider) GeminiEndpointKind() GeminiEndpointKind {
	if p.gemini != nil && strings.TrimSpace(p.gemini.EndpointKind) != "" {
		return GeminiEndpointKind(strings.TrimSpace(p.gemini.EndpointKind))
	}
	switch p.GeminiCredentialType() {
	case GeminiCredentialVertexAPIKey, GeminiCredentialVertexADC, GeminiCredentialVertexService:
		return GeminiEndpointVertex
	case GeminiCredentialGateway:
		return GeminiEndpointGateway
	default:
		return GeminiEndpointOfficial
	}
}

func (p Provider) GeminiAPIVersion(fallback string) string {
	if p.gemini != nil {
		if version := strings.TrimSpace(p.gemini.APIVersion); version == "v1" || version == "v1beta" {
			return version
		}
	}
	if fallback == "v1" || fallback == "v1beta" {
		return fallback
	}
	return "v1beta"
}

// BuildGeminiEndpoint 构造完整上游 URL。BaseURL 被视为端点配置，不做裸字符串拼接。
func BuildGeminiEndpoint(provider Provider, request GeminiEndpointRequest) (string, error) {
	baseValue := strings.TrimSpace(provider.APIURL)
	if baseValue == "" {
		return "", fmt.Errorf("Gemini Provider 缺少 Base URL")
	}
	base, err := url.Parse(baseValue)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("Gemini Base URL 无效")
	}
	base.Fragment = ""

	if request.Action == GeminiActionModels && provider.gemini != nil && strings.TrimSpace(provider.gemini.ModelsEndpoint) != "" {
		explicit, err := resolveGeminiExplicitEndpoint(base, provider.gemini.ModelsEndpoint)
		if err != nil {
			return "", err
		}
		return mergeGeminiEndpointQuery(explicit, request.Query, true)
	}

	version := provider.GeminiAPIVersion(request.Version)
	model := NormalizeGeminiModelID(request.Model)
	basePath := strings.TrimRight(base.Path, "/")
	kind := provider.GeminiEndpointKind()
	var endpointPath string

	switch kind {
	case GeminiEndpointVertex:
		if request.Action == GeminiActionModels {
			return "", fmt.Errorf("Vertex Gemini Provider 不支持通用 models 目录")
		}
		project, location := "", ""
		if provider.gemini != nil {
			project = strings.TrimSpace(provider.gemini.Project)
			location = strings.TrimSpace(provider.gemini.Location)
		}
		if project == "" || location == "" || model == "" {
			return "", fmt.Errorf("Vertex Gemini Provider 缺少 project、location 或 model")
		}
		endpointPath = fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/google/models/%s", url.PathEscape(project), url.PathEscape(location), url.PathEscape(model))
		if basePath != "" && strings.Contains(basePath, "/projects/") {
			endpointPath = basePath
		}
		switch request.Action {
		case GeminiActionGenerate, GeminiActionStreamGenerate, GeminiActionCountTokens:
			endpointPath += ":" + string(request.Action)
		case GeminiActionModel:
		default:
			return "", fmt.Errorf("不支持的 Vertex Gemini action: %s", request.Action)
		}
	default:
		root := basePath
		if strings.HasSuffix(root, "/models") {
			root = strings.TrimSuffix(root, "/models")
		}
		if !strings.HasSuffix(root, "/v1") && !strings.HasSuffix(root, "/v1beta") {
			if kind == GeminiEndpointOfficial || root == "" {
				root += "/" + version
			} else if root == "" {
				root = "/" + version
			}
		}
		if root == "" {
			root = "/" + version
		}
		root = strings.TrimRight(root, "/")
		switch request.Action {
		case GeminiActionModels:
			endpointPath = root + "/models"
		case GeminiActionModel:
			if model == "" {
				return "", fmt.Errorf("Gemini model ID 不能为空")
			}
			endpointPath = root + "/models/" + url.PathEscape(model)
		case GeminiActionGenerate, GeminiActionStreamGenerate, GeminiActionCountTokens:
			if model == "" {
				return "", fmt.Errorf("Gemini model ID 不能为空")
			}
			endpointPath = root + "/models/" + url.PathEscape(model) + ":" + string(request.Action)
		default:
			return "", fmt.Errorf("不支持的 Gemini action: %s", request.Action)
		}
	}

	base.Path = endpointPath
	base.RawPath = ""
	query := base.Query()
	for key, values := range request.Query {
		query.Del(key)
		for _, value := range values {
			query.Add(key, value)
		}
	}
	// API Key 只能来自 Credential，不能把客户端 query key 传给上游。
	query.Del("key")
	if request.IsStream() && query.Get("alt") == "" {
		query.Set("alt", "sse")
	}
	base.RawQuery = query.Encode()
	return base.String(), nil
}

// BuildGeminiUpstreamHeaders 清理客户端认证信息后，按 Credential 类型构造唯一上游认证头。
func BuildGeminiUpstreamHeaders(provider Provider, clientHeaders map[string]string) (map[string]string, error) {
	headers := make(map[string]string, len(clientHeaders)+len(provider.Headers)+4)
	for key, value := range clientHeaders {
		if shouldDropClientHeader(key) {
			continue
		}
		setHeader(headers, key, value)
	}
	if err := applyUserAgentPolicy(headers, provider); err != nil {
		return nil, err
	}
	additional, err := canonicalizeHeaderMap(provider.Headers)
	if err != nil {
		return nil, err
	}
	for key, value := range additional {
		if strings.EqualFold(key, "authorization") || strings.EqualFold(key, "x-api-key") || strings.EqualFold(key, "x-goog-api-key") {
			return nil, fmt.Errorf("Gemini 自定义 Header 不得覆盖认证头")
		}
		if err := validateAdditionalHeader(key, value); err != nil {
			return nil, err
		}
		setHeader(headers, key, value)
	}
	removeHeader(headers, "Authorization")
	removeHeader(headers, "x-api-key")
	removeHeader(headers, "x-goog-api-key")

	credential := provider.GeminiCredentialType()
	switch credential {
	case GeminiCredentialAPIKey, GeminiCredentialVertexAPIKey:
		if strings.TrimSpace(provider.APIKey) == "" {
			return nil, fmt.Errorf("Gemini Credential 缺少 API Key")
		}
		setHeader(headers, "x-goog-api-key", provider.APIKey)
	case GeminiCredentialNativeOAuth, GeminiCredentialVertexADC, GeminiCredentialVertexService:
		if strings.TrimSpace(provider.APIKey) == "" {
			return nil, fmt.Errorf("Gemini Credential 缺少 Bearer Token")
		}
		setHeader(headers, "Authorization", "Bearer "+provider.APIKey)
	case GeminiCredentialGateway:
		scheme, customHeader := provider.effectiveAuthScheme("gemini")
		switch scheme {
		case "none":
		case "bearer":
			if provider.APIKey != "" {
				setHeader(headers, "Authorization", "Bearer "+provider.APIKey)
			}
		case "x-api-key":
			if provider.APIKey != "" {
				setHeader(headers, "x-api-key", provider.APIKey)
			}
		case "custom":
			if provider.APIKey == "" {
				return nil, fmt.Errorf("Gemini 网关 Credential 缺少认证值")
			}
			if err := validateHeaderNameAndValue(customHeader, provider.APIKey); err != nil {
				return nil, err
			}
			setHeader(headers, customHeader, provider.APIKey)
		default:
			return nil, fmt.Errorf("不支持的 Gemini 网关认证方式: %s", scheme)
		}
	case GeminiCredentialCLIOAuth:
		return nil, fmt.Errorf("Gemini CLI OAuth 不能用于 Native Relay")
	default:
		return nil, fmt.Errorf("不支持的 Gemini Credential 类型: %s", credential)
	}
	if headerValue(headers, "Accept") == "" {
		setHeader(headers, "Accept", "application/json")
	}
	return headers, nil
}

func GeminiProviderEligibleForNative(provider Provider) bool {
	if !provider.Enabled || strings.TrimSpace(provider.APIURL) == "" {
		return false
	}
	switch provider.GeminiCredentialType() {
	case GeminiCredentialAPIKey, GeminiCredentialNativeOAuth, GeminiCredentialVertexAPIKey, GeminiCredentialGateway:
		scheme, _ := provider.effectiveAuthScheme("gemini")
		return provider.APIKey != "" || provider.GeminiCredentialType() == GeminiCredentialGateway && scheme == "none"
	case GeminiCredentialVertexADC, GeminiCredentialVertexService, GeminiCredentialCLIOAuth:
		// ADC/service-account exchange is intentionally explicit and is not guessed
		// from the local process environment by the Relay.
		return false
	default:
		return false
	}
}

func resolveGeminiExplicitEndpoint(base *url.URL, raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("Gemini models endpoint 无效")
	}
	resolved := base.ResolveReference(parsed)
	resolved.Fragment = ""
	return resolved.String(), nil
}

func mergeGeminiEndpointQuery(raw string, values url.Values, removeAPIKey bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("Gemini endpoint 无效")
	}
	query := parsed.Query()
	for key, incoming := range values {
		query.Del(key)
		for _, value := range incoming {
			query.Add(key, value)
		}
	}
	if removeAPIKey {
		query.Del("key")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", minInt(len(value)-4, 16)) + value[len(value)-2:]
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
