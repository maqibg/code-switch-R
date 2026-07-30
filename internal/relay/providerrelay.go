package relay

import (
	"bytes"
	"codeswitch/internal/infra"
	"codeswitch/services"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
	relayprotocol "codeswitch/services/protocol"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// warnedServiceTiers 去重容器:首次见到未知 service_tier 时告警,之后静默。
var warnedServiceTiers sync.Map

var relayDebugLogging = strings.EqualFold(strings.TrimSpace(os.Getenv("CODE_SWITCH_RELAY_DEBUG")), "1") ||
	strings.EqualFold(strings.TrimSpace(os.Getenv("CODE_SWITCH_RELAY_DEBUG")), "true")

func relayDebugf(format string, args ...any) {
	if relayDebugLogging {
		fmt.Printf(format, args...)
	}
}

// warnUnknownTier 在首次遇到未知 service_tier 值时打印一次警告。
// 同值的后续请求静默,不同未知 tier 分别告警一次。
func warnUnknownTier(tier string) {
	if tier == "" {
		return
	}
	if _, loaded := warnedServiceTiers.LoadOrStore(tier, struct{}{}); loaded {
		return
	}
	fmt.Printf("⚠️  unknown service_tier=%q,保留原值入库,按 default 档计费\n", tier)
}

// LastUsedProvider 最后使用的供应商信息
// @author sm
type LastUsedProvider struct {
	Platform     string `json:"platform"`      // claude/codex/gemini
	ProviderName string `json:"provider_name"` // 供应商名称
	UpdatedAt    int64  `json:"updated_at"`    // 更新时间（毫秒）
}

type ProviderRelayService struct {
	providerService     *services.ProviderService
	blacklistService    *services.BlacklistService
	notificationService *services.NotificationService
	appSettings         *services.AppSettingsService // 应用设置服务（用于获取轮询开关状态）
	pricingService      *services.PricingService
	server              *http.Server
	addr                string
	lastUsed            map[string]*LastUsedProvider // 各平台最后使用的供应商
	lastUsedMu          sync.RWMutex                 // 保护 lastUsed 的锁
	rrMu                sync.Mutex                   // 轮询状态锁
	rrLastStart         map[string]string            // 轮询状态：key="platform:level" → value=上次起始 Provider Name
	codexChatHistory    *CodexChatBridgeHistoryStore
}

// errClientAbort 表示客户端中断连接，不应计入 provider 失败次数
var errClientAbort = errors.New("client aborted, skip failure count")

// errResponseCommitted 表示响应头或响应体已经发送，后续不得再切换 Provider。
var errResponseCommitted = errors.New("response already committed")

// NewProviderRelayService 构造转发服务。
//
// 不再需要 GeminiService：Gemini provider 已并入 provider 表，
// 转发时和其他平台一样走 ProviderService.LoadProviders("gemini")。
//
// pricingService 从构造参数进来，而不是原先的 SetPricingService 后置注入：
// 依赖顺序上它本来就在这之前构造好，后置注入只是造出一段"已构造未注入"的
// 窗口态，逼着 telemetry 与 LogService 各留一套定价回退。
func NewProviderRelayService(
	providerService *services.ProviderService,
	blacklistService *services.BlacklistService,
	notificationService *services.NotificationService,
	appSettings *services.AppSettingsService,
	pricingService *services.PricingService,
	addr string,
) *ProviderRelayService {
	if addr == "" {
		addr = "127.0.0.1:18100" // 【安全修复】仅监听本地回环地址，防止 API Key 暴露到局域网
	}

	// 【修复】数据库初始化已移至 main.go 的 InitDatabase()
	// 此处不再调用 xdb.Inits()、ensureRequestLogTable()

	return &ProviderRelayService{
		providerService:     providerService,
		blacklistService:    blacklistService,
		notificationService: notificationService,
		appSettings:         appSettings,
		pricingService:      pricingService,
		addr:                addr,
		lastUsed: map[string]*LastUsedProvider{
			"claude": nil,
			"codex":  nil,
			"gemini": nil,
		},
		rrLastStart:      make(map[string]string),
		codexChatHistory: NewCodexChatBridgeHistoryStore(128),
	}
}

// setLastUsedProvider 记录最后使用的供应商
// @author sm
func (prs *ProviderRelayService) setLastUsedProvider(platform, providerName string) {
	prs.lastUsedMu.Lock()
	defer prs.lastUsedMu.Unlock()
	prs.lastUsed[platform] = &LastUsedProvider{
		Platform:     platform,
		ProviderName: providerName,
		UpdatedAt:    time.Now().UnixMilli(),
	}
}

// GetLastUsedProvider 获取指定平台最后使用的供应商
// @author sm
func (prs *ProviderRelayService) GetLastUsedProvider(platform string) *LastUsedProvider {
	prs.lastUsedMu.RLock()
	defer prs.lastUsedMu.RUnlock()
	return prs.lastUsed[platform]
}

// GetAllLastUsedProviders 获取所有平台最后使用的供应商
// @author sm
func (prs *ProviderRelayService) GetAllLastUsedProviders() map[string]*LastUsedProvider {
	prs.lastUsedMu.RLock()
	defer prs.lastUsedMu.RUnlock()
	result := make(map[string]*LastUsedProvider)
	for k, v := range prs.lastUsed {
		result[k] = v
	}
	return result
}

// isRoundRobinSettingEnabled 检查轮询设置是否启用（纯读取 AppSettings，不受 Fixed Mode 影响）
// 用于在 Fixed Mode 分支内也支持轮询排序
func (prs *ProviderRelayService) isRoundRobinSettingEnabled() bool {
	if prs.appSettings == nil {
		return false
	}
	settings, err := prs.appSettings.GetAppSettings()
	if err != nil {
		return false
	}
	return settings.EnableRoundRobin
}

// roundRobinOrder 对同 Level 的 providers 进行轮询排序
// 算法：基于 name 追踪，将上次起始 provider 移到末尾，实现轮询效果
// 参数：
//   - platform: 平台标识（claude/codex/gemini/custom:xxx）
//   - level: 当前 Level
//   - providers: 同 Level 的 providers 列表（已过滤、按用户排序）
//
// 返回：轮询排序后的 providers 列表（新切片，不修改原切片）
func (prs *ProviderRelayService) roundRobinOrder(platform string, level int, providers []services.Provider) []services.Provider {
	if len(providers) <= 1 {
		return providers
	}

	// 构建 key: "platform:level"
	key := fmt.Sprintf("%s:%d", platform, level)

	prs.rrMu.Lock()
	defer prs.rrMu.Unlock()

	lastStart := prs.rrLastStart[key]

	// 记录本次起始 provider 名称（更新状态）
	prs.rrLastStart[key] = providers[0].Name

	// 如果没有历史记录，返回原顺序
	if lastStart == "" {
		return providers
	}

	// 查找上次起始 provider 在当前列表中的位置
	lastIdx := -1
	for i, p := range providers {
		if p.Name == lastStart {
			lastIdx = i
			break
		}
	}

	// 上次起始 provider 不在当前列表（可能被禁用/黑名单），返回原顺序
	if lastIdx == -1 {
		return providers
	}

	// 构建轮询顺序：从 lastIdx+1 开始，环形遍历
	result := make([]services.Provider, len(providers))
	for i := 0; i < len(providers); i++ {
		idx := (lastIdx + 1 + i) % len(providers)
		result[i] = providers[idx]
	}

	// 更新本次起始 provider 名称
	prs.rrLastStart[key] = result[0].Name

	return result
}

func (prs *ProviderRelayService) Start() error {
	// 启动前验证配置
	if warnings := prs.validateConfig(); len(warnings) > 0 {
		fmt.Println("======== Provider 配置验证警告 ========")
		for _, warn := range warnings {
			fmt.Printf("⚠️  %s\n", warn)
		}
		fmt.Println("========================================")
	}

	router := gin.New()
	router.Use(gin.Recovery())
	prs.registerRoutes(router)

	prs.server = &http.Server{
		Addr:    prs.addr,
		Handler: router,
	}

	fmt.Printf("provider relay server listening on %s\n", prs.addr)

	go func() {
		if err := prs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("provider relay server error: %v\n", err)
		}
	}()
	return nil
}

// validateConfig 验证所有 provider 的配置
// 返回警告列表（非阻塞性错误）
func (prs *ProviderRelayService) validateConfig() []string {
	warnings := make([]string, 0)

	for _, kind := range services.ProviderPlatformIDs() {
		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("[%s] 加载配置失败: %v", kind, err))
			continue
		}

		enabledCount := 0
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			enabledCount++

			// 验证每个启用的 provider
			if errs := p.ValidateConfiguration(); len(errs) > 0 {
				for _, errMsg := range errs {
					warnings = append(warnings, fmt.Sprintf("[%s/%s] %s", kind, p.Name, errMsg))
				}
			}

			// 检查是否配置了模型白名单或映射
			if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
				(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 未配置 supportedModels 或 modelMapping，将假设支持所有模型（可能导致降级失败）",
					kind, p.Name))
			}

			// 检查是否只配置了映射但没有白名单
			if len(p.ModelMapping) > 0 && len(p.SupportedModels) == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 配置了 modelMapping 但未配置 supportedModels，映射目标将不做校验，请确认目标模型在供应商处可用",
					kind, p.Name))
			}
		}

		if enabledCount == 0 {
			warnings = append(warnings, fmt.Sprintf("[%s] 没有启用的 provider", kind))
		}
	}

	return warnings
}

func (prs *ProviderRelayService) Stop() error {
	if prs.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return prs.server.Shutdown(ctx)
}

func (prs *ProviderRelayService) Addr() string {
	return prs.addr
}

func (prs *ProviderRelayService) registerRoutes(router gin.IRouter) {
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))
	router.POST("/responses", prs.proxyHandler("codex", "/responses"))
	router.POST("/v1/responses", prs.proxyHandler("codex", "/v1/responses"))
	router.POST("/v1/v1/responses", prs.proxyHandler("codex", "/v1/responses"))
	router.POST("/codex/v1/responses", prs.proxyHandler("codex", "/v1/responses"))
	router.POST("/responses/compact", prs.proxyHandler("codex", "/responses/compact"))
	router.POST("/v1/responses/compact", prs.proxyHandler("codex", "/v1/responses/compact"))
	router.POST("/v1/v1/responses/compact", prs.proxyHandler("codex", "/v1/responses/compact"))
	router.POST("/codex/v1/responses/compact", prs.proxyHandler("codex", "/v1/responses/compact"))

	// Grok Build 使用独立平台范围，不能复用 Codex 的 Provider、黑名单或日志。
	router.POST("/grok/v1/responses", prs.proxyHandler("grok", "/v1/responses"))
	router.POST("/grok/v1/responses/compact", prs.proxyHandler("grok", "/v1/responses/compact"))

	// /v1/models 端点（OpenAI-compatible API）
	// 支持 Claude 和 Codex 平台
	router.GET("/v1/models", prs.modelsHandler("claude"))
	router.GET("/grok/v1/models", prs.grokModelsHandler)

	// Gemini API 端点（使用专门的路径前缀避免与 Claude 冲突）
	router.POST("/gemini/v1beta/*any", prs.geminiProxyHandler("/v1beta"))
	router.POST("/gemini/v1/*any", prs.geminiProxyHandler("/v1"))

	// Reasonix 端点 — 请求格式为 OpenAI Chat Completions
	router.POST("/reasonix/chat/completions", prs.proxyHandler("reasonix", "/chat/completions"))
	router.GET("/reasonix/models", prs.modelsHandler("reasonix"))

	// Pi 的每个 models.json Provider 使用独立路由，避免跨平台模型 ID 冲突。
	router.POST("/pi/providers/:provider/*any", prs.piPlatformProxyHandler())

	// 自定义 CLI 工具端点（路由格式: /custom/:toolId/v1/messages）
	// toolId 用于区分不同的 CLI 工具，对应 provider kind 为 "custom:{toolId}"
	router.POST("/custom/:toolId/v1/messages", prs.customCliProxyHandler())

	// 自定义 CLI 工具的 /v1/models 端点
	router.GET("/custom/:toolId/v1/models", prs.customModelsHandler())
}

func (prs *ProviderRelayService) proxyHandler(kind string, endpoint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		if kind == "pi" {
			services.LogPiDebugInbound(c.GetString(services.PiPlatformContextKey), endpoint, flattenQuery(c.Request.URL.Query()), cloneHeaders(c.Request.Header), bodyBytes)
		}

		isStream := gjson.GetBytes(bodyBytes, "stream").Bool()
		requestedModel := gjson.GetBytes(bodyBytes, "model").String()
		telemetryModel := requestedModel
		piPlatform := ""
		if kind == "pi" {
			piPlatform = c.GetString(services.PiPlatformContextKey)
			filteredBody, bareModel, err := services.PreparePiRelayRequest(bodyBytes, endpoint)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			bodyBytes = filteredBody
			requestedModel = bareModel
			telemetryModel = bareModel
			isStream = isStream || strings.Contains(strings.ToLower(endpoint), "streamgeneratecontent")
		}
		clientProtocol := relayprotocol.ClientProtocolForPlatform(kind, endpoint)
		if protocolName, exists := c.Get(services.PiClientProtocolContextKey); exists {
			clientProtocol = relayprotocol.Protocol(fmt.Sprint(protocolName))
		}
		telemetry := beginRelayTelemetry(c, kind, clientProtocol, telemetryModel, isStream, prs.pricingService, piPlatform)
		if piPlatform != "" {
			telemetry.SourceID = piPlatform
		}
		defer telemetry.finish(c)
		relayScope := kind
		if piPlatform != "" {
			relayScope = "pi:" + piPlatform
		}

		// 如果未指定模型，记录警告但不拦截
		if requestedModel == "" {
			infra.LogWarn("请求未指定模型名，无法执行模型智能降级")
		}

		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
			return
		}

		active := make([]services.Provider, 0, len(providers))
		skippedCount := 0
		for _, provider := range providers {
			if piPlatform != "" && provider.PiPlatformKey() != piPlatform {
				continue
			}
			// 无认证上游允许空 API Key，其余认证方式必须提供凭据。
			if !services.ProviderEligibleForRelay(provider, kind) {
				continue
			}
			if kind == "grok" && isResponsesCompactEndpoint(endpoint) &&
				services.ResolveProviderUpstreamProtocol(kind, provider, endpoint) != services.UpstreamProtocolOpenAIResponses {
				// compact 没有跨协议转换语义，非原生 Responses Provider 不能参与调度。
				skippedCount++
				continue
			}

			// 配置验证：失败则自动跳过
			if errs := provider.CachedValidationErrors(); len(errs) > 0 {
				infra.LogWarn(fmt.Sprintf("Provider %s 配置验证失败，已自动跳过: %v", provider.Name, errs))
				skippedCount++
				continue
			}

			// 核心过滤：只保留支持请求模型的 provider
			if requestedModel != "" && !provider.IsModelSupported(requestedModel) {
				relayDebugf("[INFO] Provider %s 不支持模型 %s，已跳过\n", provider.Name, requestedModel)
				skippedCount++
				continue
			}

			// 黑名单检查：跳过已拉黑的 provider
			if isBlacklisted, until := services.BlacklistedFor(prs.blacklistService, services.BlacklistTargetFor(relayScope, provider)); isBlacklisted {
				fmt.Printf("⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
				skippedCount++
				continue
			}

			active = append(active, provider)
		}

		if len(active) == 0 {
			if requestedModel != "" {
				c.JSON(http.StatusNotFound, gin.H{
					"error": fmt.Sprintf("没有可用的 provider 支持模型 '%s'（已跳过 %d 个不兼容的 provider）", requestedModel, skippedCount),
				})
			} else {
				c.JSON(http.StatusNotFound, gin.H{"error": "no providers available"})
			}
			return
		}
		if kind == "pi" {
			services.LogPiDebugRoute(piPlatform, requestedModel, endpoint, active)
		}

		if relayDebugLogging {
			infra.LogInfo(fmt.Sprintf("找到 %d 个可用的 provider（已过滤 %d 个）：", len(active), skippedCount))
			for _, p := range active {
				fmt.Printf("%s ", p.Name)
			}
			fmt.Println()
		}

		query := flattenQuery(c.Request.URL.Query())
		if piPlatform != "" {
			services.DropPiClientCredentialQuery(query)
		}
		clientHeaders := cloneHeaders(c.Request.Header)

		result := prs.dispatchWithFailover(c, dispatchRequest{
			Scope:     relayScope,
			Providers: active,
			Notify:    true,
			Forward: func(provider services.Provider) (bool, error) {
				effectiveModel := provider.GetEffectiveModel(requestedModel)
				effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)
				currentBodyBytes := bodyBytes
				if effectiveModel != requestedModel && requestedModel != "" {
					relayDebugf("[INFO] Provider %s 映射模型: %s -> %s\n",
						provider.Name, requestedModel, effectiveModel)
					modifiedBody, modifiedEndpoint, err := services.ApplyPiAwareModelMapping(
						bodyBytes, effectiveEndpoint, requestedModel, effectiveModel, clientProtocol)
					if err != nil {
						// 映射失败是配置问题，不是 provider 不可靠：跳过但不记失败
						return false, fmt.Errorf("%w: 模型映射失败: %v", errSkipProvider, err)
					}
					currentBodyBytes = modifiedBody
					effectiveEndpoint = modifiedEndpoint
				}
				return prs.forwardProviderRequest(c, kind, provider, effectiveEndpoint,
					query, clientHeaders, currentBodyBytes, isStream, effectiveModel)
			},
		})

		switch result.Outcome {
		case dispatchSucceeded, dispatchStopped:
			// 成功，或响应已写出/客户端已断开：不能再写响应
			return
		case dispatchClientRejected:
			message := result.ErrorMessage()
			relayDebugf("[INFO] 🚫 客户端请求被拒绝: %s\n", message)
			c.JSON(http.StatusBadRequest, gin.H{
				"type":    "error",
				"error":   map[string]string{"type": "invalid_request_error", "message": message},
				"message": message,
			})
			return
		}

		// 所有 provider 都失败或被拉黑
		if result.FixedMode {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s",
					result.LastProvider, result.ErrorMessage()),
				"lastProvider":  result.LastProvider,
				"totalAttempts": result.TotalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"error": fmt.Sprintf("所有 %d 个 provider 均失败，最后错误: %s",
				result.TotalAttempts, result.ErrorMessage()),
			"last_provider":  result.LastProvider,
			"last_duration":  fmt.Sprintf("%.2fs", result.LastDuration.Seconds()),
			"total_attempts": result.TotalAttempts,
		})
	}
}

func (prs *ProviderRelayService) forwardProviderRequest(
	c *gin.Context,
	kind string,
	provider services.Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (bool, error) {
	return prs.forwardRequest(c, kind, provider, endpoint, query, clientHeaders, bodyBytes, isStream, model)
}

func (prs *ProviderRelayService) forwardRequest(
	c *gin.Context,
	kind string,
	provider services.Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	isStream bool,
	model string,
) (success bool, forwardErr error) {
	requestLog := services.RequestLog{Platform: kind, Provider: provider.Name, Model: model, IsStream: isStream}
	attemptStarted := time.Now()
	var execution *relayForwardExecution
	defer func() {
		if execution != nil && execution.UseCodexContinue {
			return
		}
		if telemetry := relayTelemetryFromContext(c); telemetry != nil {
			telemetry.recordAttempt(provider, execution, requestLog, attemptStarted, success, forwardErr)
		}
	}()

	clientProtocol := relayprotocol.ClientProtocolForPlatform(kind, endpoint)
	if telemetry := relayTelemetryFromContext(c); telemetry != nil {
		clientProtocol = telemetry.ClientProtocol
	}
	execution, forwardErr = prs.newRelayForwardExecution(kind, clientProtocol, provider, endpoint, bodyBytes, isStream, model)
	if forwardErr != nil {
		return false, forwardErr
	}
	execution.BodyBytes, forwardErr = services.ApplyProviderRequestBodyPolicyForModel(execution.BodyBytes, provider, model, execution.UpstreamProtocol)
	if forwardErr != nil {
		return false, forwardErr
	}
	if execution.UseCodexContinue {
		return prs.forwardCodexResponsesWithContinue(c, execution, endpoint, query, clientHeaders, bodyBytes, model, &requestLog)
	}
	headers, forwardErr := services.BuildUpstreamHeadersForModel(provider, kind, model, clientHeaders, execution.UpstreamProtocol)
	if forwardErr != nil {
		return false, forwardErr
	}
	bodyBytes = execution.BodyBytes
	if forwardErr = services.ApplyBodyAwareRequestIdentityHeaders(headers, provider, model, bodyBytes, execution.UpstreamProtocol); forwardErr != nil {
		return false, forwardErr
	}
	targetURL := joinURL(provider.APIURL, execution.TargetEndpoint)
	if kind == "pi" {
		services.LogPiDebugUpstream(provider.PiPlatformKey(), provider, targetURL, query, headers, bodyBytes)
	}

	req := xrequest.New().
		// 绑定客户端请求 context：客户端断开时立即释放上游连接。
		// 否则配合下面 32 小时的超时，非流式请求会在客户端离开后继续占用
		// goroutine 和上游连接，最长可达 32 小时。
		WithContext(c.Request.Context()).
		SetHeaders(headers).
		SetQueryParams(query).
		SetRetry(0, 0).
		SetTimeout(32 * time.Hour) // 32小时超时，适配超大型项目分析
	proxyConfig := services.ProxyConfig{}
	if prs.appSettings != nil {
		proxyConfig, forwardErr = prs.providerProxyConfigFor(c, provider.ProxyEnabled)
		if forwardErr != nil {
			return false, fmt.Errorf("读取代理配置失败: %w", forwardErr)
		}
	} else if provider.ProxyEnabled {
		return false, fmt.Errorf("代理配置服务未初始化")
	}
	client, forwardErr := services.NewHTTPClientWithProxy(0, nil, proxyConfig)
	if forwardErr != nil {
		return false, fmt.Errorf("创建代理客户端失败: %w", forwardErr)
	}
	req = req.SetClient(client)

	reqBody := bytes.NewReader(bodyBytes)
	req = req.SetBody(reqBody)

	resp, err := req.Post(targetURL)

	// 无论成功失败，先尝试记录 HttpCode
	if resp != nil {
		requestLog.HttpCode = resp.StatusCode()
		if kind == "pi" {
			services.LogPiDebugResponse(provider.Name, requestLog.HttpCode)
		}
	}

	if err != nil {
		friendly := services.DescribeProxyTransportError(err, proxyConfig)
		// resp 存在但 err != nil：可能是客户端中断，不计入失败
		if resp != nil && requestLog.HttpCode == 0 {
			relayDebugf("[INFO] Provider %s 响应存在但状态码为0，判定为客户端中断\n", provider.Name)
			return false, fmt.Errorf("%w: %v", errClientAbort, err)
		}
		// 尝试从响应体提取供应商原始错误信息
		if resp != nil {
			if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
				return false, fmt.Errorf("upstream status %d: %s", resp.StatusCode(), upstreamBody)
			}
		}
		return false, fmt.Errorf("%s", friendly)
	}

	if resp == nil {
		return false, fmt.Errorf("empty response")
	}

	status := requestLog.HttpCode

	if resp.Error() != nil {
		// resp 存在、有错误、但状态码为 0：客户端中断，不计入失败
		if status == 0 {
			relayDebugf("[INFO] Provider %s 响应错误但状态码为0，判定为客户端中断\n", provider.Name)
			return false, fmt.Errorf("%w: %v", errClientAbort, resp.Error())
		}
		// 优先使用 extractUpstreamError 提取完整错误（覆盖 SSE 空 body 场景）
		errMsg := strings.TrimSpace(resp.Error().Error())
		if errMsg == "" {
			if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
				errMsg = upstreamBody
			}
		}
		if errMsg != "" {
			return false, fmt.Errorf("upstream status %d: %s", status, errMsg)
		}
		return false, fmt.Errorf("upstream status %d", status)
	}

	// 状态码为 0 且无错误：当作成功处理
	if status == 0 {
		infra.LogWarn(fmt.Sprintf("Provider %s 返回状态码 0，但无错误，当作成功处理", provider.Name))
		copyErr := prs.copyRelayExecutionResponse(c, resp, execution, &requestLog)
		if copyErr != nil {
			return prs.judgeResponseCopyFailure(c, provider, execution, copyErr)
		}
		return true, nil
	}

	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		copyErr := prs.copyRelayExecutionResponse(c, resp, execution, &requestLog)
		if copyErr != nil {
			return prs.judgeResponseCopyFailure(c, provider, execution, copyErr)
		}
		return true, nil
	}

	// 尝试从响应体提取供应商原始错误信息
	if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
		return false, fmt.Errorf("upstream status %d: %s", status, upstreamBody)
	}
	return false, fmt.Errorf("upstream status %d", status)
}

// extractUpstreamError 从供应商响应中提取原始错误信息（最多 512 字节）
func extractUpstreamError(resp *xrequest.Response) string {
	if resp == nil {
		return ""
	}
	// 优先尝试 String()（会自动解压 gzip 等）
	body := resp.String()
	// SSE 流式响应时 String() 返回空，回退到直接读取 RawResponse.Body（带超时防御）
	if body == "" && resp.RawResponse != nil && resp.RawResponse.Body != nil {
		// 这里的 Body 只用于提取错误摘要，读完即弃。
		// 无论读取成功、失败还是超时都必须关闭：这是失败降级路径，
		// 上游持续 429/5xx 时每次失败都会走到这里，不关闭会持续泄漏连接和
		// http transport 的后台读循环 goroutine。
		defer resp.RawResponse.Body.Close()

		done := make(chan []byte, 1)
		go func() {
			raw, err := io.ReadAll(io.LimitReader(resp.RawResponse.Body, 512))
			if err == nil {
				done <- raw
			} else {
				done <- nil
			}
		}()
		select {
		case raw := <-done:
			if raw != nil {
				body = string(raw)
			}
		case <-time.After(500 * time.Millisecond):
			// 超时放弃，交给 defer 关闭 Body 以中断后台读取
		}
	}
	if body == "" {
		return ""
	}
	// 截断过长的错误信息
	if len(body) > 512 {
		body = body[:512] + "..."
	}
	return body
}

func cloneHeaders(header http.Header) map[string]string {
	cloned := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			cloned[key] = values[len(values)-1]
		}
	}
	return cloned
}

func cloneMap(m map[string]string) map[string]string {
	cloned := make(map[string]string, len(m))
	for k, v := range m {
		cloned[k] = v
	}
	return cloned
}

func flattenQuery(values map[string][]string) map[string]string {
	query := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) > 0 {
			query[key] = items[len(items)-1]
		}
	}
	return query
}

func joinURL(base string, endpoint string) string {
	base = strings.TrimSuffix(base, "/")
	endpoint = "/" + strings.TrimPrefix(endpoint, "/")
	return base + endpoint
}

func RequestLogHook(c *gin.Context, kind string, usage *services.RequestLog) func(data []byte) (bool, []byte) { // SSE 钩子：累计字节和解析 token 用量
	return func(data []byte) (bool, []byte) {
		payload := strings.TrimSpace(string(data))

		parserFn := ClaudeCodeParseTokenUsageFromResponse
		switch kind {
		case "codex":
			parserFn = CodexParseTokenUsageFromResponse
		case "gemini":
			parserFn = GeminiParseTokenUsageFromResponse
		case "reasonix":
			parserFn = ReasonixParseTokenUsageFromResponse
		}
		parseEventPayload(payload, parserFn, usage)

		return true, data
	}
}

func parseEventPayload(payload string, parser func(string, *services.RequestLog), usage *services.RequestLog) {
	hasData := false
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			hasData = true
			// SSE 规范允许 "data:xxx" 和 "data: xxx",统一剥掉 "data:" 再 trim 空格
			parser(strings.TrimSpace(strings.TrimPrefix(line, "data:")), usage)
		}
	}
	// 非流式响应(无 data: 前缀)直接把 payload 当完整 JSON 喂给 parser
	if !hasData {
		if body := strings.TrimSpace(payload); body != "" {
			parser(body, usage)
		}
	}
}

// claude code usage parser
// 覆盖三种场景:
//  1. SSE message_start.message.usage (input/cache 一次性)
//  2. SSE message_delta.usage (output/cache cumulative,按事件累积上报同一请求的最终值)
//  3. 非流式根级 usage (单次完整 snapshot)
//
// 对每个字段取 max,既兼容 message_delta 的累计语义,也兼容多事件重复出现的字段,避免重复计费。
// 参考 https://docs.anthropic.com/en/api/messages-streaming
func ClaudeCodeParseTokenUsageFromResponse(data string, usage *services.RequestLog) {
	if !strings.Contains(data, `"usage"`) {
		return
	}
	collectAnthropicUsage(data, "message.usage", usage)
	collectAnthropicUsage(data, "usage", usage)
	clampCacheEphemerals(usage)
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// collectAnthropicUsage 从指定前缀(message.usage 或 usage)提取 Anthropic 字段,取 max 避免 += 累计导致的翻倍。
func collectAnthropicUsage(data, prefix string, usage *services.RequestLog) {
	maxIntInto(&usage.InputTokens, int(gjson.Get(data, prefix+".input_tokens").Int()))
	maxIntInto(&usage.OutputTokens, int(gjson.Get(data, prefix+".output_tokens").Int()))
	maxIntInto(&usage.CacheCreateTokens, int(gjson.Get(data, prefix+".cache_creation_input_tokens").Int()))
	maxIntInto(&usage.CacheReadTokens, int(gjson.Get(data, prefix+".cache_read_input_tokens").Int()))
	maxIntInto(&usage.Ephemeral5mTokens, int(gjson.Get(data, prefix+".cache_creation.ephemeral_5m_input_tokens").Int()))
	maxIntInto(&usage.Ephemeral1hTokens, int(gjson.Get(data, prefix+".cache_creation.ephemeral_1h_input_tokens").Int()))
	if rawTier := gjson.Get(data, prefix+".service_tier").String(); strings.TrimSpace(rawTier) != "" {
		usage.ServiceTier = string(modelpricing.NormalizeObservedServiceTier(rawTier, warnUnknownTier))
	}
}

// maxIntInto 把 candidate 大于 *dst 时写回,用于流式 cumulative 字段合并。
func maxIntInto(dst *int, candidate int) {
	if candidate > *dst {
		*dst = candidate
	}
}

// codex usage parser(OpenAI Responses API)
func CodexParseTokenUsageFromResponse(data string, usage *services.RequestLog) {
	for _, prefix := range []string{"response.usage", "usage"} {
		maxIntInto(&usage.InputTokens, int(gjson.Get(data, prefix+".input_tokens").Int()))
		maxIntInto(&usage.OutputTokens, int(gjson.Get(data, prefix+".output_tokens").Int()))
		maxIntInto(&usage.CacheReadTokens, int(gjson.Get(data, prefix+".input_tokens_details.cached_tokens").Int()))
		maxIntInto(&usage.ReasoningTokens, int(gjson.Get(data, prefix+".output_tokens_details.reasoning_tokens").Int()))
	}
	// service_tier 可能在 response.service_tier 或 response.usage.service_tier,两路径都尝试
	for _, path := range []string{"response.service_tier", "response.usage.service_tier", "service_tier", "usage.service_tier"} {
		if rawTier := gjson.Get(data, path).String(); strings.TrimSpace(rawTier) != "" {
			usage.ServiceTier = string(modelpricing.NormalizeObservedServiceTier(rawTier, warnUnknownTier))
			break
		}
	}
}

// reasonix usage parser(DeepSeek OpenAI-compatible Chat Completions API)
func ReasonixParseTokenUsageFromResponse(data string, usage *services.RequestLog) {
	maxIntInto(&usage.InputTokens, int(gjson.Get(data, "usage.prompt_tokens").Int()))
	maxIntInto(&usage.OutputTokens, int(gjson.Get(data, "usage.completion_tokens").Int()))
	maxIntInto(&usage.CacheReadTokens, int(gjson.Get(data, "usage.prompt_cache_hit_tokens").Int()))
	maxIntInto(&usage.ReasoningTokens, int(gjson.Get(data, "usage.completion_tokens_details.reasoning_tokens").Int()))
}

// clampCacheEphemerals 兜底 Anthropic ephemeral 拆分的异常情况:
// 若 5m+1h > total,打印一次警告并截断到 total(保留 5m 优先级,1h 截掉超出部分)。
// 若 split 非零但 total 为 0,把 total 回填为 split 之和,避免 total 被漏传导致 create cost 计 0。
func clampCacheEphemerals(usage *services.RequestLog) {
	if usage == nil {
		return
	}
	split := usage.Ephemeral5mTokens + usage.Ephemeral1hTokens
	if split == 0 {
		return
	}
	if usage.CacheCreateTokens == 0 {
		usage.CacheCreateTokens = split
		return
	}
	if split > usage.CacheCreateTokens {
		fmt.Printf("⚠️  ephemeral split(%d)>total(%d),截断 1h=%d 到可用额度\n",
			split, usage.CacheCreateTokens, usage.Ephemeral1hTokens)
		overflow := split - usage.CacheCreateTokens
		if usage.Ephemeral1hTokens >= overflow {
			usage.Ephemeral1hTokens -= overflow
			return
		}
		// 1h 截到 0 还不够,再从 5m 截剩余
		remaining := overflow - usage.Ephemeral1hTokens
		usage.Ephemeral1hTokens = 0
		if usage.Ephemeral5mTokens >= remaining {
			usage.Ephemeral5mTokens -= remaining
		} else {
			usage.Ephemeral5mTokens = 0
		}
	}
}

// gemini usage parser (流式响应专用)
// Gemini SSE 流中每个 chunk 都会携带完整的 usageMetadata，需取最大值而非累加
func GeminiParseTokenUsageFromResponse(data string, usage *services.RequestLog) {
	usageResult := gjson.Get(data, "usageMetadata")
	if !usageResult.Exists() {
		return
	}
	mergeGeminiUsageMetadata(usageResult, usage)
}

// mergeGeminiUsageMetadata 合并 Gemini usageMetadata 到 RequestLog（取最大值去重）
// Gemini 流式响应特点：每个 chunk 包含截止当前的累计用量，因此取最大值即可
func mergeGeminiUsageMetadata(usage gjson.Result, reqLog *services.RequestLog) {
	if !usage.Exists() || reqLog == nil {
		return
	}

	// 取最大值（流式响应中后续 chunk 包含前面的累计值）
	if v := int(usage.Get("promptTokenCount").Int()); v > reqLog.InputTokens {
		reqLog.InputTokens = v
	}
	if v := int(usage.Get("candidatesTokenCount").Int()); v > reqLog.OutputTokens {
		reqLog.OutputTokens = v
	}
	if v := int(usage.Get("cachedContentTokenCount").Int()); v > reqLog.CacheReadTokens {
		reqLog.CacheReadTokens = v
	}
	// Gemini thinking/reasoning tokens (thoughtsTokenCount)
	// 参考: https://ai.google.dev/gemini-api/docs/thinking
	if v := int(usage.Get("thoughtsTokenCount").Int()); v > reqLog.ReasoningTokens {
		reqLog.ReasoningTokens = v
	}

	// 若仅提供 totalTokenCount，按 total - input 估算输出 token
	total := usage.Get("totalTokenCount").Int()
	if total > 0 && reqLog.OutputTokens == 0 && reqLog.InputTokens > 0 && reqLog.InputTokens < int(total) {
		reqLog.OutputTokens = int(total) - reqLog.InputTokens
	}
}

// streamGeminiResponseWithHook 流式传输 Gemini 响应并通过 Hook 提取 token 用量
// 【修复】维护跨 chunk 缓冲，确保完整 SSE 事件解析
// Gemini SSE 格式: "data: {json}\n\n" 或 "data: [DONE]\n\n"
func streamGeminiResponseWithHook(body io.Reader, writer io.Writer, requestLog *services.RequestLog) error {
	buf := make([]byte, 8192)   // 增大缓冲区减少系统调用
	var lineBuf strings.Builder // 跨 chunk 行缓冲

	for {
		n, err := body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			// 写入客户端（优先保证数据传输）
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				return writeErr
			}
			// 如果是 http.Flusher，立即刷新
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			// 解析 SSE 数据提取 token 用量（使用缓冲处理跨 chunk 情况）
			parseGeminiSSEWithBuffer(string(chunk), &lineBuf, requestLog)
		}
		if err != nil {
			// 处理缓冲区残留数据
			if lineBuf.Len() > 0 {
				parseGeminiSSELine(lineBuf.String(), requestLog)
				lineBuf.Reset()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// parseGeminiSSEWithBuffer 使用缓冲处理跨 chunk 的 SSE 事件
// 【修复】解决 JSON 被 TCP 分割到多个 chunk 导致解析失败的问题
func parseGeminiSSEWithBuffer(chunk string, lineBuf *strings.Builder, requestLog *services.RequestLog) {
	// 将当前 chunk 追加到缓冲
	lineBuf.WriteString(chunk)
	content := lineBuf.String()

	// 按双换行符分割完整的 SSE 事件
	// SSE 格式: "data: {...}\n\n" 或 "data: {...}\r\n\r\n"
	for {
		// 查找事件分隔符（双换行）
		idx := strings.Index(content, "\n\n")
		if idx == -1 {
			// 尝试 \r\n\r\n 分隔符
			idx = strings.Index(content, "\r\n\r\n")
			if idx == -1 {
				break // 没有完整事件，等待更多数据
			}
			idx += 4 // \r\n\r\n 长度
		} else {
			idx += 2 // \n\n 长度
		}

		// 提取完整事件
		event := content[:idx]
		content = content[idx:]

		// 解析事件中的 data 行
		parseGeminiSSELine(event, requestLog)
	}

	// 更新缓冲区为未处理的残留数据
	lineBuf.Reset()
	lineBuf.WriteString(content)
}

// parseGeminiSSELine 解析单个 SSE 事件提取 usageMetadata
// 【优化】只在包含 usageMetadata 时才调用 gjson 解析
func parseGeminiSSELine(event string, requestLog *services.RequestLog) {
	lines := strings.Split(event, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" || data == "" {
			continue
		}
		// 【优化】快速检查是否包含 usageMetadata，避免无效解析
		if !strings.Contains(data, "usageMetadata") {
			continue
		}
		GeminiParseTokenUsageFromResponse(data, requestLog)
	}
}

// geminiProxyHandler 处理 Gemini API 请求（支持 Level 分组降级和黑名单）
func (prs *ProviderRelayService) geminiProxyHandler(apiVersion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取完整路径（例如 /v1beta/models/gemini-2.5-pro:generateContent）
		fullPath := c.Param("any")
		endpoint := apiVersion + fullPath

		// 保留查询参数（如 ?alt=sse, ?key= 等）
		query := c.Request.URL.RawQuery
		if query != "" {
			endpoint = endpoint + "?" + query
		}

		relayDebugf("[Gemini] 收到请求: %s\n", endpoint)

		// 读取请求体
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 判断是否为流式请求
		isStream := strings.Contains(endpoint, ":streamGenerateContent") || strings.Contains(query, "alt=sse")
		requestedModel := extractGeminiModelFromEndpoint(endpoint)
		telemetry := beginRelayTelemetry(c, "gemini", relayprotocol.GeminiNative, requestedModel, isStream, prs.pricingService, "")
		defer telemetry.finish(c)

		// 加载 Gemini providers（已并入 provider 表，与其他平台同一类型）
		providers, err := prs.providerService.LoadProviders("gemini")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load gemini providers"})
			return
		}
		if len(providers) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "no gemini providers configured"})
			return
		}

		// 1. 过滤可用的 providers（启用 + BaseURL 配置 + 未被拉黑）
		var activeProviders []services.Provider
		for _, p := range providers {
			if !p.Enabled || p.APIURL == "" {
				continue
			}
			// 检查黑名单
			if isBlacklisted, until := services.BlacklistedFor(prs.blacklistService, services.BlacklistTargetFor("gemini", p)); isBlacklisted {
				fmt.Printf("[Gemini] ⛔ Provider %s 已拉黑，过期时间: %v\n", p.Name, until.Format("15:04:05"))
				continue
			}
			// Level 默认值处理
			if p.Level <= 0 {
				p.Level = 1
			}
			activeProviders = append(activeProviders, p)
		}

		if len(activeProviders) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "no active gemini provider (all disabled or blacklisted)"})
			return
		}

		result := prs.dispatchWithFailover(c, dispatchRequest{
			Scope:     "gemini",
			Providers: activeProviders,
			LogPrefix: "Gemini",
			// Gemini 原先没有切换通知，保持不变
			Notify: false,
			Forward: func(provider services.Provider) (bool, error) {
				// Gemini 不做模型映射：模型名在 endpoint 里，不在请求体
				ok, errMsg, responseWritten := prs.forwardGeminiAttempt(
					c, provider, endpoint, bodyBytes, isStream)
				if ok {
					return true, nil
				}
				// 把 (errMsg, responseWritten) 适配成统一错误语义：
				// responseWritten 对应 errResponseCommitted——响应已发出，
				// 不能再换 provider，但失败仍要记账。
				if responseWritten {
					return false, fmt.Errorf("%w: %s", errResponseCommitted, errMsg)
				}
				return false, errors.New(errMsg)
			},
		})

		switch result.Outcome {
		case dispatchSucceeded, dispatchStopped:
			return
		case dispatchClientRejected:
			// Gemini 路径不做协议转换，实际走不到；留着是为了穷尽分支
			c.JSON(http.StatusBadRequest, gin.H{"error": result.ErrorMessage()})
			return
		}

		if result.FixedMode {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s",
					result.LastProvider, result.ErrorMessage()),
				"lastProvider":  result.LastProvider,
				"totalAttempts": result.TotalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}
		// 降级模式的响应形状与另两个 handler 不同，这是 Gemini 客户端在用的
		// 既有形状，不跟着统一
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "all gemini providers failed",
			"details": result.ErrorMessage(),
		})
	}
}

// extractGeminiModelFromEndpoint 从 Gemini API endpoint 中提取模型名
// 例如 "/v1beta/models/gemini-2.5-pro:generateContent?alt=sse" -> "gemini-2.5-pro"
func extractGeminiModelFromEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	// 移除查询参数
	if qIdx := strings.Index(endpoint, "?"); qIdx >= 0 {
		endpoint = endpoint[:qIdx]
	}
	// 查找 models/ 后面的部分
	idx := strings.Index(endpoint, "models/")
	if idx == -1 {
		return ""
	}
	rest := endpoint[idx+len("models/"):]
	if rest == "" {
		return ""
	}
	// 移除动作部分（如 :generateContent, :streamGenerateContent）
	if colonIdx := strings.Index(rest, ":"); colonIdx >= 0 {
		rest = rest[:colonIdx]
	}
	return strings.TrimSpace(rest)
}

// forwardGeminiRequest 转发 Gemini 请求到指定 provider
// 返回 (成功, 错误信息, 是否已写入响应)
// 【重要】当 responseWritten=true 时，调用方不得重试或降级，因为响应头/数据已发送给客户端
func (prs *ProviderRelayService) forwardGeminiRequest(
	c *gin.Context,
	provider services.Provider,
	endpoint string,
	bodyBytes []byte,
	isStream bool,
	requestLog *services.RequestLog,
) (success bool, errMsg string, responseWritten bool) {
	providerStart := time.Now()

	// 构建目标 URL
	targetURL := strings.TrimSuffix(provider.APIURL, "/") + endpoint

	// 预先填充日志，保证失败也能记录 provider 和模型
	requestLog.Provider = provider.Name
	// 【修复】每次尝试开始前重置 HttpCode，避免重试时沿用上一次的状态码
	requestLog.HttpCode = 0
	// 优先从 endpoint 提取模型名（如 gemini-2.5-pro），否则回退到 provider 配置的默认模型
	if extractedModel := extractGeminiModelFromEndpoint(endpoint); extractedModel != "" {
		requestLog.Model = extractedModel
	} else {
		requestLog.Model = provider.GeminiDefaultModel()
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Sprintf("创建请求失败: %v", err), false
	}

	// 复制请求头
	for key, values := range c.Request.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// 设置 API Key
	if provider.APIKey != "" {
		req.Header.Set("x-goog-api-key", provider.APIKey)
	}

	// 发送请求
	proxyConfig := services.ProxyConfig{}
	if provider.ProxyEnabled {
		if prs.appSettings == nil {
			return false, "代理配置服务未初始化", false
		}
		proxyConfig, err = prs.appSettings.GetProviderProxyConfig(true)
		if err != nil {
			return false, fmt.Sprintf("读取代理配置失败: %v", err), false
		}
	}
	client, err := services.NewHTTPClientWithProxy(300*time.Second, nil, proxyConfig)
	if err != nil {
		return false, fmt.Sprintf("创建代理客户端失败: %v", err), false
	}
	resp, err := client.Do(req)
	providerDuration := time.Since(providerStart).Seconds()

	if err != nil {
		friendly := services.DescribeProxyTransportError(err, proxyConfig)
		fmt.Printf("[Gemini]   ✗ 失败: %s | 错误: %v | 耗时: %.2fs\n", provider.Name, err, providerDuration)
		return false, fmt.Sprintf("请求失败: %s", friendly), false
	}
	defer resp.Body.Close()

	// 先记录上游状态码，失败场景也能落库
	requestLog.HttpCode = resp.StatusCode

	// 检查响应状态
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("[Gemini]   ✗ 失败: %s | HTTP %d | 耗时: %.2fs\n", provider.Name, resp.StatusCode, providerDuration)
		return false, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(errorBody)), false
	}

	fmt.Printf("[Gemini]   ✓ 连接成功: %s | HTTP %d | 耗时: %.2fs\n", provider.Name, resp.StatusCode, providerDuration)

	// 处理响应
	if isStream {
		// 流式模式：先写 header 再流式传输
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}
		c.Status(resp.StatusCode)
		c.Writer.Flush()
		// 【重要】从 Flush() 开始，响应头已写入客户端，任何失败都不能重试
		copyErr := streamGeminiResponseWithHook(resp.Body, c.Writer, requestLog)
		if copyErr != nil {
			fmt.Printf("[Gemini]   ⚠️ 流式传输中断: %s | 错误: %v\n", provider.Name, copyErr)
			// 流式传输中断：已写入部分响应，客户端会收到不完整数据
			return false, fmt.Sprintf("流式传输中断: %v", copyErr), true
		}
	} else {
		// 非流式模式：先读完 body 再写 header（允许读取失败时重试）
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			fmt.Printf("[Gemini]   ⚠️ 读取响应失败: %s | 错误: %v\n", provider.Name, readErr)
			// 【修复】此时 header 尚未写入客户端，可以重试/降级
			return false, fmt.Sprintf("读取响应失败: %v", readErr), false
		}
		// 解析 Gemini 用量数据
		parseGeminiUsageMetadata(body, requestLog)
		// 读取成功后再写 header 和 body
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	return true, "", true
}

func (prs *ProviderRelayService) forwardGeminiAttempt(
	c *gin.Context,
	provider services.Provider,
	endpoint string,
	bodyBytes []byte,
	isStream bool,
) (success bool, errMsg string, responseWritten bool) {
	requestLog := &services.RequestLog{Platform: "gemini", IsStream: isStream}
	started := time.Now()
	success, errMsg, responseWritten = prs.forwardGeminiRequest(c, provider, endpoint, bodyBytes, isStream, requestLog)
	var attemptErr error
	if !success {
		attemptErr = errors.New(errMsg)
	}
	if telemetry := relayTelemetryFromContext(c); telemetry != nil {
		telemetry.recordGeminiAttempt(provider, *requestLog, started, success, attemptErr)
	}
	return success, errMsg, responseWritten
}

// parseGeminiUsageMetadata 从 Gemini 非流式响应中提取用量，填充 request_log
// 复用 mergeGeminiUsageMetadata 统一解析逻辑
func parseGeminiUsageMetadata(body []byte, reqLog *services.RequestLog) {
	if len(body) == 0 || reqLog == nil {
		return
	}
	usage := gjson.GetBytes(body, "usageMetadata")
	if !usage.Exists() {
		return
	}
	mergeGeminiUsageMetadata(usage, reqLog)
}

// customCliProxyHandler 处理自定义 CLI 工具的 API 请求
// 路由格式: /custom/:toolId/v1/messages
// toolId 用于区分不同的 CLI 工具，对应 provider kind 为 "custom:{toolId}"
func (prs *ProviderRelayService) customCliProxyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 URL 参数提取 toolId
		toolId := c.Param("toolId")
		if toolId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "toolId is required"})
			return
		}

		// 构建 provider kind（格式: "custom:{toolId}"）
		kind := "custom:" + toolId
		endpoint := "/v1/messages"

		relayDebugf("[CustomCLI] 收到请求: toolId=%s, kind=%s\n", toolId, kind)

		// 读取请求体
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		isStream := gjson.GetBytes(bodyBytes, "stream").Bool()
		requestedModel := gjson.GetBytes(bodyBytes, "model").String()
		telemetry := beginRelayTelemetry(c, kind, relayprotocol.AnthropicMessages, requestedModel, isStream, prs.pricingService, "")
		defer telemetry.finish(c)

		if requestedModel == "" {
			fmt.Printf("[CustomCLI][WARN] 请求未指定模型名，无法执行模型智能降级\n")
		}

		// 加载该 CLI 工具的 providers
		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load providers for %s: %v", kind, err)})
			return
		}

		// 过滤可用的 providers
		active := make([]services.Provider, 0, len(providers))
		skippedCount := 0
		for _, provider := range providers {
			if !services.ProviderEligibleForRelay(provider, kind) {
				continue
			}

			if errs := provider.CachedValidationErrors(); len(errs) > 0 {
				fmt.Printf("[CustomCLI][WARN] Provider %s 配置验证失败，已自动跳过: %v\n", provider.Name, errs)
				skippedCount++
				continue
			}

			if requestedModel != "" && !provider.IsModelSupported(requestedModel) {
				fmt.Printf("[CustomCLI][INFO] Provider %s 不支持模型 %s，已跳过\n", provider.Name, requestedModel)
				skippedCount++
				continue
			}

			// 黑名单检查
			if isBlacklisted, until := services.BlacklistedFor(prs.blacklistService, services.BlacklistTargetFor(kind, provider)); isBlacklisted {
				fmt.Printf("[CustomCLI] ⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
				skippedCount++
				continue
			}

			active = append(active, provider)
		}

		if len(active) == 0 {
			if requestedModel != "" {
				c.JSON(http.StatusNotFound, gin.H{
					"error": fmt.Sprintf("没有可用的 provider 支持模型 '%s'（已跳过 %d 个不兼容的 provider）", requestedModel, skippedCount),
				})
			} else {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no providers available for %s", kind)})
			}
			return
		}

		fmt.Printf("[CustomCLI][INFO] 找到 %d 个可用的 provider（已过滤 %d 个）：", len(active), skippedCount)
		for _, p := range active {
			fmt.Printf("%s ", p.Name)
		}
		fmt.Println()

		query := flattenQuery(c.Request.URL.Query())
		clientHeaders := cloneHeaders(c.Request.Header)

		result := prs.dispatchWithFailover(c, dispatchRequest{
			Scope:     kind,
			Providers: active,
			LogPrefix: "CustomCLI",
			Notify:    true,
			Forward: func(provider services.Provider) (bool, error) {
				effectiveModel := provider.GetEffectiveModel(requestedModel)
				currentBodyBytes := bodyBytes
				if effectiveModel != requestedModel && requestedModel != "" {
					modifiedBody, err := services.ReplaceModelInRequestBody(bodyBytes, effectiveModel)
					if err != nil {
						// 映射失败是配置问题，不是 provider 不可靠：跳过但不记失败
						return false, fmt.Errorf("%w: 模型映射失败: %v", errSkipProvider, err)
					}
					currentBodyBytes = modifiedBody
				}
				effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)
				return prs.forwardRequest(c, kind, provider, effectiveEndpoint,
					query, clientHeaders, currentBodyBytes, isStream, effectiveModel)
			},
		})

		switch result.Outcome {
		case dispatchSucceeded, dispatchStopped:
			return
		case dispatchClientRejected:
			// 这份 handler 原先没有这个分支（三套里只有它缺）：
			// 跨协议转换被拒会被当成 provider 失败并降级，换 provider 也是同样结果。
			// 走统一调度后自动获得正确行为。
			message := result.ErrorMessage()
			c.JSON(http.StatusBadRequest, gin.H{
				"type":    "error",
				"error":   map[string]string{"type": "invalid_request_error", "message": message},
				"message": message,
			})
			return
		}

		if result.FixedMode {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s",
					result.LastProvider, result.ErrorMessage()),
				"lastProvider":  result.LastProvider,
				"totalAttempts": result.TotalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"error": fmt.Sprintf("所有 %d 个 provider 均失败，最后错误: %s",
				result.TotalAttempts, result.ErrorMessage()),
			"last_provider":  result.LastProvider,
			"last_duration":  fmt.Sprintf("%.2fs", result.LastDuration.Seconds()),
			"total_attempts": result.TotalAttempts,
		})
	}
}

// forwardModelsRequest 共享的 /v1/models 请求转发逻辑
// 返回 (selectedProvider, error)
func (prs *ProviderRelayService) forwardModelsRequest(
	c *gin.Context,
	kind string,
	logPrefix string,
) error {
	fmt.Printf("[%s] 收到 /v1/models 请求, kind=%s\n", logPrefix, kind)

	// 加载 providers
	providers, err := prs.providerService.LoadProviders(kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
		return fmt.Errorf("failed to load providers: %w", err)
	}

	// 过滤可用的 providers（启用 + URL + 与认证策略匹配的凭据）
	var activeProviders []services.Provider
	for _, provider := range providers {
		if !services.ProviderEligibleForRelay(provider, kind) {
			continue
		}

		// 黑名单检查：跳过已拉黑的 provider
		if isBlacklisted, until := services.BlacklistedFor(prs.blacklistService, services.BlacklistTargetFor(kind, provider)); isBlacklisted {
			fmt.Printf("[%s] ⛔ Provider %s 已拉黑，过期时间: %v\n", logPrefix, provider.Name, until.Format("15:04:05"))
			continue
		}

		activeProviders = append(activeProviders, provider)
	}

	if len(activeProviders) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no providers available"})
		return fmt.Errorf("no providers available")
	}

	// 按 Level 分组并排序
	levelGroups := make(map[int][]services.Provider)
	for _, provider := range activeProviders {
		level := provider.Level
		if level <= 0 {
			level = 1
		}
		levelGroups[level] = append(levelGroups[level], provider)
	}

	levels := make([]int, 0, len(levelGroups))
	for level := range levelGroups {
		levels = append(levels, level)
	}
	sort.Ints(levels)

	// 尝试第一个可用的 provider（按 Level 升序）
	var selectedProvider *services.Provider
	for _, level := range levels {
		if len(levelGroups[level]) > 0 {
			p := levelGroups[level][0]
			selectedProvider = &p
			break
		}
	}

	if selectedProvider == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no providers available"})
		return fmt.Errorf("no providers available after filtering")
	}

	fmt.Printf("[%s] 使用 Provider: %s | URL: %s\n", logPrefix, selectedProvider.Name, selectedProvider.APIURL)

	// 构建目标 URL（拼接 provider 的 APIURL 和 /v1/models）
	targetURL := joinURL(selectedProvider.APIURL, "/v1/models")

	// 创建 HTTP 请求
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("创建请求失败: %v", err)})
		return fmt.Errorf("failed to create request: %w", err)
	}

	headers, err := services.BuildUpstreamHeaders(*selectedProvider, kind, cloneHeaders(c.Request.Header), services.ResolveProviderUpstreamProtocol(kind, *selectedProvider, "/v1/models"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// 发送请求
	proxyConfig := services.ProxyConfig{}
	if selectedProvider.ProxyEnabled {
		if prs.appSettings == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "代理配置服务未初始化"})
			return fmt.Errorf("proxy settings service not initialized")
		}
		proxyConfig, err = prs.appSettings.GetProviderProxyConfig(true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取代理配置失败: %v", err)})
			return fmt.Errorf("failed to load proxy config: %w", err)
		}
	}
	client, err := services.NewHTTPClientWithProxy(30*time.Second, nil, proxyConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("创建代理客户端失败: %v", err)})
		return fmt.Errorf("failed to create proxy client: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		friendly := services.DescribeProxyTransportError(err, proxyConfig)
		fmt.Printf("[%s] ✗ 请求失败: %s | 错误: %v\n", logPrefix, selectedProvider.Name, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("请求失败: %s", friendly)})
		return fmt.Errorf("request failed: %s", friendly)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("[%s] ✗ 读取响应失败: %s | 错误: %v\n", logPrefix, selectedProvider.Name, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("读取响应失败: %v", err)})
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	fmt.Printf("[%s] ✓ 成功: %s | HTTP %d\n", logPrefix, selectedProvider.Name, resp.StatusCode)

	// 返回响应
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	return nil
}

// modelsHandler 处理 /v1/models 请求（OpenAI-compatible API）
// 将请求转发到第一个可用的 provider 并注入 API Key
func (prs *ProviderRelayService) modelsHandler(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = prs.forwardModelsRequest(c, kind, "Models")
	}
}

// grokModelsHandler returns the stable model contract consumed by Grok Build.
// Upstream model names remain provider-owned and are never exposed through the
// client-facing Grok endpoint.
func (prs *ProviderRelayService) grokModelsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data": []gin.H{{
			"id":         "grok-build",
			"object":     "model",
			"owned_by":   "code-switch-r",
			"created":    time.Now().Unix(),
			"permission": []string{},
		}},
	})
}

// customModelsHandler 处理自定义 CLI 工具的 /v1/models 请求
// 路由格式: /custom/:toolId/v1/models
func (prs *ProviderRelayService) customModelsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 URL 参数提取 toolId
		toolId := c.Param("toolId")
		if toolId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "toolId is required"})
			return
		}

		// 构建 provider kind（格式: "custom:{toolId}"）
		kind := "custom:" + toolId

		_ = prs.forwardModelsRequest(c, kind, "CustomModels")
	}
}
