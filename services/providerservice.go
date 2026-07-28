package services

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AvailabilityConfig 仅用于兼容读取已删除的可用性配置。
type AvailabilityConfig struct {
	TestModel    string `json:"testModel,omitempty"`
	TestEndpoint string `json:"testEndpoint,omitempty"`
	Timeout      int    `json:"timeout,omitempty"`
}

type Provider struct {
	ID           int64  `json:"id"` // 修复：使用 int64 支持大 ID 值
	Name         string `json:"name"`
	APIURL       string `json:"apiUrl"`
	APIKey       string `json:"apiKey"`
	Site         string `json:"officialSite"`
	Icon         string `json:"icon"`
	Tint         string `json:"tint"`
	Accent       string `json:"accent"`
	Enabled      bool   `json:"enabled"`
	ProxyEnabled bool   `json:"proxyEnabled,omitempty"`

	// API 端点路径（可选）- 覆盖平台默认端点
	// 如：GLM 模型需要使用 /v1/chat/completions 而非 /v1/messages
	// 留空则使用平台默认（claude: /v1/messages, codex: /responses）
	APIEndpoint string `json:"apiEndpoint,omitempty"`

	// 模型白名单 - Provider 原生支持的模型名
	// 使用 map 实现 O(1) 查找，向后兼容（omitempty）
	SupportedModels map[string]bool `json:"supportedModels,omitempty"`

	// 模型映射 - 外部模型名 -> Provider 内部模型名
	// 支持精确匹配和通配符（如 "claude-*" -> "anthropic/claude-*"）
	ModelMapping map[string]string `json:"modelMapping,omitempty"`

	// 优先级分组 - 数字越小优先级越高（1-10，默认 1）
	// 使用 omitempty 确保零值不序列化，向后兼容
	Level int `json:"level,omitempty"`

	// 已删除的可用性功能字段，仅用于兼容旧 Provider JSON，不再影响运行时。
	AvailabilityMonitorEnabled bool                `json:"availabilityMonitorEnabled,omitempty"`
	ConnectivityAutoBlacklist  bool                `json:"connectivityAutoBlacklist,omitempty"`
	AvailabilityConfig         *AvailabilityConfig `json:"availabilityConfig,omitempty"`

	// 认证方式 - bearer / x-api-key / 自定义 Header 名
	// 空值时使用平台默认 bearer。
	ConnectivityAuthType string `json:"connectivityAuthType,omitempty"`

	// 上游协议类型 - anthropic / openai_chat / auto
	// anthropic: 上游使用 Anthropic Messages API（默认）
	// openai_chat: 上游使用 OpenAI Chat Completions API，自动转换请求/响应格式
	// auto: 根据 APIEndpoint 自动检测（包含 /chat/completions 则为 openai_chat）
	UpstreamProtocol string `json:"upstreamProtocol,omitempty"`

	// AuthScheme is the relay authentication policy. ConnectivityAuthType remains a
	// read-compatible legacy fallback because older provider files already use it.
	AuthScheme string `json:"authScheme,omitempty"`
	AuthHeader string `json:"authHeader,omitempty"`

	// Headers contains non-authentication upstream headers. Dangerous transport and
	// credential headers are rejected before requests are sent.
	Headers map[string]string `json:"headers,omitempty"`

	// UserAgentPreset selects a known compatibility identity. "custom" uses CustomUserAgent;
	// an empty value keeps the inbound runtime User-Agent.
	UserAgentPreset string `json:"userAgentPreset,omitempty"`
	CustomUserAgent string `json:"customUserAgent,omitempty"`
	// RequestIdentity is the post-Pi relay identity applied to the real upstream.
	// ModelRequestIdentities may replace it for an individual mapped upstream model.
	RequestIdentity        *ProviderRequestIdentity           `json:"requestIdentity,omitempty"`
	ModelRequestIdentities map[string]ProviderRequestIdentity `json:"modelRequestIdentities,omitempty"`

	// ModelsEndpoint overrides model discovery only and does not affect inference requests.
	ModelsEndpoint string `json:"modelsEndpoint,omitempty"`

	// PiModels stores the complete Pi model definitions exposed by this upstream
	// provider. Empty keeps the legacy supportedModels-derived gateway behavior.
	PiModels []PiModelEntry `json:"piModels,omitempty"`
	// PiModelOverrides are merged into matching generated models before writing
	// the custom code-switch-r provider. Pi cannot apply overrides to unknown custom models itself.
	PiModelOverrides map[string]PiModelOverride `json:"piModelOverrides,omitempty"`
	// PiPlatform identifies the models.json Provider key that owns this Pi supplier.
	PiPlatform string `json:"piPlatform,omitempty"`
	// PiTemplate is the legacy name used by earlier development builds. It is
	// migrated to PiPlatform on load and is no longer written by new saves.
	PiTemplate     string `json:"piTemplate,omitempty"`
	MetadataUserID string `json:"metadataUserId,omitempty"`

	// Codex reasoning 自动续写 - 仅对 Codex 原生 Responses 流式请求生效
	CodexReasoningContinueEnabled bool `json:"codexReasoningContinueEnabled,omitempty"`

	// Codex reasoning 自动续写控制台日志 - nil 时保持兼容，默认输出日志
	CodexReasoningContinueLogEnabled *bool `json:"codexReasoningContinueLogEnabled,omitempty"`

	// 已删除的旧连通性功能字段，仅用于兼容旧 Provider JSON。
	ConnectivityCheck        bool   `json:"connectivityCheck,omitempty"`
	ConnectivityTestModel    string `json:"connectivityTestModel,omitempty"`
	ConnectivityTestEndpoint string `json:"connectivityTestEndpoint,omitempty"`

	// 内部字段：配置验证错误（不持久化）
	configErrors    []string `json:"-"`
	configValidated bool     `json:"-"`

	// gemini 承载 Gemini provider 的专有数据（A1 第 5 步）。
	// 非导出因此不进 Wails 绑定：对外仍通过 GeminiProvider 暴露，
	// 前端无需改动。持久化时进 config_json 的 gemini 子对象。
	gemini *geminiConfigPayload `json:"-"`
}

type providerEnvelope struct {
	Providers []Provider `json:"providers"`
}

func defaultConnectivityAuthType(_ string) string {
	return "bearer"
}

func (p Provider) piPlatformKey() string {
	if value := strings.TrimSpace(p.PiPlatform); value != "" {
		return value
	}
	return strings.TrimSpace(p.PiTemplate)
}

type ProviderService struct {
	mu            sync.Mutex
	piGatewaySync func([]Provider) error
	cacheMu       sync.Mutex
	fileCache     map[string]providerFileCache
}

type providerFileCache struct {
	modTime   time.Time
	size      int64
	providers []Provider
}

func NewProviderService() *ProviderService {
	return &ProviderService{}
}

func (ps *ProviderService) Start() error { return nil }
func (ps *ProviderService) Stop() error  { return nil }

// providerFilePath 返回 provider 配置文件路径，必要时创建目录。
// kind → 文件名的映射统一由 platform_registry.go 的 providerFileNameFor 提供。
func providerFilePath(kind string) (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	filename, customToolID, err := providerFileNameFor(kind)
	if err != nil {
		return "", err
	}
	if customToolID != "" {
		// 自定义 CLI 的供应商存放在 providers/ 子目录
		providersDir := filepath.Join(dir, "providers")
		if err := os.MkdirAll(providersDir, 0o755); err != nil {
			return "", err
		}
		return filepath.Join(providersDir, filename), nil
	}
	return filepath.Join(dir, filename), nil
}

func (ps *ProviderService) SaveProviders(kind string, providers []Provider) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.saveProvidersLocked(kind, providers)
}

// providerSavePlan 是校验与规范化后的待保存结果。
// data 字段（序列化好的 JSON）已随主数据入库移除。
type providerSavePlan struct {
	providers        []Provider
	aliasPlatform    string
	aliasEnabled     bool
	deletedProviders []deletedProvider
}

func (ps *ProviderService) setPiGatewaySync(syncGateway func([]Provider) error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.piGatewaySync = syncGateway
}

// loadProvidersRaw 读取 provider 列表但不做配置校验。
// 用于内部只需要当前存储内容的场景（如 name 变更检测）。
func (ps *ProviderService) loadProvidersRaw(kind string) ([]Provider, error) {
	scope, err := scopeForKind(kind)
	if err != nil {
		return nil, err
	}
	return loadProvidersFromDB(context.Background(), scope)
}

// saveProvidersLocked 内部保存方法，调用方必须已持有锁。
//
// 写入落到 provider 表的单个事务里。原实现是「先写 JSON → 再提交 DB 事务 →
// 失败时补偿回写文件」，进程在文件写完、事务提交前崩溃就永久不一致，
// 且补偿本身可能失败（只能打 CRITICAL 日志）。现在不存在这个窗口，
// 补偿逻辑（rollbackProviderFile）随之不再需要。
func (ps *ProviderService) saveProvidersLocked(kind string, providers []Provider) error {
	scope, err := scopeForKind(kind)
	if err != nil {
		return err
	}

	existingProviders, err := ps.loadProvidersRaw(kind)
	if err != nil {
		return err
	}
	plan, err := prepareProviderSave(kind, providers, existingProviders, 0)
	if err != nil {
		return err
	}

	ctx := context.Background()
	deleted, err := replaceProvidersInDB(ctx, scope, plan.providers)
	if err != nil {
		return err
	}

	if strings.EqualFold(strings.TrimSpace(kind), "pi") && ps.piGatewaySync != nil {
		if err := ps.piGatewaySync(plan.providers); err != nil {
			// Pi 网关同步失败：把 provider 表恢复成保存前的内容。
			// 这里仍需回滚，因为 models.json 是本进程之外的文件，
			// 无法与数据库事务原子提交。
			if _, restoreErr := replaceProvidersInDB(ctx, scope, existingProviders); restoreErr != nil {
				return fmt.Errorf("同步 Pi models.json 失败: %w; 回滚 provider 失败: %v", err, restoreErr)
			}
			return fmt.Errorf("同步 Pi models.json 失败: %w", err)
		}
	}

	if len(deleted) == 0 {
		return nil
	}
	// 清理被删除 provider 的关联数据（黑名单等）。
	// 日志表按 provider_id 关联且允许 NULL，删除后历史统计仍可用。
	if err := cleanupDeletedProviders(plan.aliasPlatform, deleted); err != nil {
		logError("清理已删除 provider 的关联数据失败", "platform", plan.aliasPlatform, "error", err)
	}
	return nil
}

func prepareProviderSave(kind string, providers, existingProviders []Provider, allowedRenameID int64) (providerSavePlan, error) {
	preparedProviders := append([]Provider(nil), providers...)
	nameByID := make(map[int64]string, len(existingProviders))
	for _, provider := range existingProviders {
		nameByID[provider.ID] = provider.Name
	}

	aliasPlatform, aliasErr := resolvePlatform(kind)
	plan := providerSavePlan{
		providers:     preparedProviders,
		aliasPlatform: aliasPlatform,
		aliasEnabled:  aliasErr == nil,
	}
	if plan.aliasEnabled {
		plan.deletedProviders = diffDeletedProviders(existingProviders, preparedProviders)
	}

	validationErrors := make([]string, 0)
	for i := range preparedProviders {
		p := &preparedProviders[i]
		if strings.EqualFold(strings.TrimSpace(kind), "pi") &&
			(strings.TrimSpace(p.Name) == "" || strings.Contains(p.Name, "/")) {
			validationErrors = append(validationErrors, fmt.Sprintf("[%s] Pi Provider 名称不能为空且不能包含 '/'", p.Name))
		}
		if strings.EqualFold(strings.TrimSpace(kind), "pi") {
			syncPiModelsToSupportedModels(p)
			for _, errMsg := range validatePiProviderConfiguration(*p) {
				validationErrors = append(validationErrors, fmt.Sprintf("[%s] %s", p.Name, errMsg))
			}
		}

		// 规则：name 不可修改（走独立 RenameProvider 路径,SaveProviders 只允许既有 name）
		if oldName, ok := nameByID[p.ID]; ok && oldName != p.Name && p.ID != allowedRenameID {
			return providerSavePlan{}, fmt.Errorf("provider id %d 的 name 不可修改(请使用 RenameProvider)", p.ID)
		}

		// 原先这里禁止"新建的 provider 用了别人 48h 内的旧名"，
		// 因为 alias resolver 会把它静默归并到那个 provider 的历史里。
		// 日志与黑名单改按 provider_id 关联后不存在这种归并，限制随之取消。

		// 验证模型配置
		p.configValidated = false
		p.configErrors = nil
		if errs := p.ValidateConfiguration(); len(errs) > 0 {
			for _, errMsg := range errs {
				validationErrors = append(validationErrors, fmt.Sprintf("[%s] %s", p.Name, errMsg))
			}
		}

		p.clearLegacyPiFields()
	}
	if strings.EqualFold(strings.TrimSpace(kind), "pi") {
		validationErrors = append(validationErrors, validatePiSupplierURLUniqueness(preparedProviders)...)
	}

	// 如果有验证错误，返回汇总错误
	if len(validationErrors) > 0 {
		return providerSavePlan{}, fmt.Errorf("配置验证失败：\n  - %s", strings.Join(validationErrors, "\n  - "))
	}
	for i := range preparedProviders {
		if err := canonicalizeProviderHeaderMaps(&preparedProviders[i]); err != nil {
			return providerSavePlan{}, fmt.Errorf("[%s] Header 规范化失败: %w", preparedProviders[i].Name, err)
		}
	}

	// 主数据入库后不再序列化 JSON：写入直接走 provider 表。
	// providerEnvelope 只保留给迁移导入与导出使用。
	return plan, nil
}

func validatePiSupplierURLUniqueness(providers []Provider) []string {
	seen := make(map[string]string, len(providers))
	errors := make([]string, 0)
	for _, provider := range providers {
		canonicalURL, err := canonicalPiSupplierURL(provider.APIURL)
		if err != nil || canonicalURL == "" {
			continue
		}
		platformID := strings.ToLower(provider.piPlatformKey())
		if platformID == "" {
			platformID = string(resolveProviderUpstreamProtocol("pi", provider, provider.GetEffectiveEndpoint("/v1/chat/completions")))
		}
		key := platformID + "\x00" + canonicalURL
		if existingName, exists := seen[key]; exists {
			errors = append(errors, fmt.Sprintf("[%s] 与供应商 %q 使用相同协议模板和 API URL，请合并为一个供应商", provider.Name, existingName))
			continue
		}
		seen[key] = provider.Name
	}
	return errors
}

func canonicalPiSupplierURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("无效 URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

// LoadProviders 读取指定平台的 provider 列表。
//
// 数据源是 provider 表（A1）。此前是读 JSON 文件并按 mtime+size 做缓存，
// 那个缓存本身有竞态：Windows 上 mtime 精度约 1-2 秒，同一秒内
// "写入 → 读取 → 再写入同长度内容" 会返回陈旧结果。改读数据库后
// 不再需要这层缓存。
func (ps *ProviderService) LoadProviders(kind string) ([]Provider, error) {
	scope, err := scopeForKind(kind)
	if err != nil {
		return nil, err
	}
	providers, err := loadProvidersFromDB(context.Background(), scope)
	if err != nil {
		return nil, err
	}
	// 保持既有契约：没有任何 provider 时返回 nil（原实现在文件不存在时如此）
	if len(providers) == 0 {
		return nil, nil
	}
	// 与原实现一致：读出后填充配置校验结果，供 UI 显示与 relay 过滤使用
	for i := range providers {
		providers[i].ValidateConfiguration()
	}
	return providers, nil
}

func (ps *ProviderService) invalidateProviderCache(path string) {
	ps.cacheMu.Lock()
	delete(ps.fileCache, path)
	ps.cacheMu.Unlock()
}

func cloneProviderList(source []Provider) []Provider {
	cloned := append([]Provider(nil), source...)
	for i := range cloned {
		cloned[i].SupportedModels = cloneProviderBoolMap(source[i].SupportedModels)
		cloned[i].ModelMapping = cloneProviderStringMap(source[i].ModelMapping)
		cloned[i].Headers = cloneProviderStringMap(source[i].Headers)
		cloned[i].configErrors = append([]string(nil), source[i].configErrors...)
	}
	return cloned
}

func cloneProviderBoolMap(source map[string]bool) map[string]bool {
	if source == nil {
		return nil
	}
	cloned := make(map[string]bool, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneProviderStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// loadProvidersNoLock 内部加载方法，在持有锁的情况下调用（避免递归加锁）。
// 仅在已持有 ps.mu 锁的上下文中调用（如 DuplicateProvider）。
//
// 与 loadProvidersRaw 一样读 provider 表：此前读 JSON 文件，
// 主数据入库后那样会读到陈旧数据。
func (ps *ProviderService) loadProvidersNoLock(kind string) ([]Provider, error) {
	scope, err := scopeForKind(kind)
	if err != nil {
		return nil, err
	}
	return loadProvidersFromDB(context.Background(), scope)
}

// clearLegacyPiFields 只迁移仍在使用的 Pi 平台字段。
func (p *Provider) clearLegacyPiFields() {
	if strings.TrimSpace(p.PiPlatform) == "" {
		p.PiPlatform = strings.TrimSpace(p.PiTemplate)
	}
	p.PiTemplate = ""
}

// DuplicateProvider 复制供应商配置，生成新的副本
// 返回新创建的 Provider 对象
func (ps *ProviderService) DuplicateProvider(kind string, sourceID int64) (*Provider, error) {
	// 1. 先加锁，避免并发修改
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// 2. 加载现有配置（在锁内完成，确保数据一致性）
	// 使用不加锁的内部加载逻辑，避免在持锁时再次加锁。
	providers, err := ps.loadProvidersNoLock(kind)
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}

	// 3. 查找源供应商
	var source *Provider
	for i := range providers {
		if providers[i].ID == sourceID {
			source = &providers[i]
			break
		}
	}
	if source == nil {
		return nil, fmt.Errorf("未找到 ID 为 %d 的供应商", sourceID)
	}

	// 4. 生成新 ID（当前最大 ID + 1）
	maxID := int64(0)
	for _, p := range providers {
		if p.ID > maxID {
			maxID = p.ID
		}
	}
	newID := maxID + 1

	// 5. 克隆配置（深拷贝）
	cloned := &Provider{
		ID:                   newID,
		Name:                 source.Name + " (副本)",
		APIURL:               source.APIURL,
		APIKey:               source.APIKey,
		Site:                 source.Site,
		Icon:                 source.Icon,
		Tint:                 source.Tint,
		Accent:               source.Accent,
		Enabled:              false, // 默认禁用，避免与源供应商冲突
		ProxyEnabled:         source.ProxyEnabled,
		Level:                source.Level,
		APIEndpoint:          source.APIEndpoint,          // 复制端点配置
		UpstreamProtocol:     source.UpstreamProtocol,     // 复制上游协议配置
		ConnectivityAuthType: source.ConnectivityAuthType, // 复制认证方式
		AuthScheme:           source.AuthScheme,
		AuthHeader:           source.AuthHeader,
		UserAgentPreset:      source.UserAgentPreset,
		CustomUserAgent:      source.CustomUserAgent,
		RequestIdentity:      nil,
		ModelsEndpoint:       source.ModelsEndpoint,
		PiPlatform:           source.piPlatformKey(),
		MetadataUserID:       source.MetadataUserID,
	}

	// 6. 深拷贝 map（避免共享引用）
	if source.SupportedModels != nil {
		cloned.SupportedModels = make(map[string]bool, len(source.SupportedModels))
		for k, v := range source.SupportedModels {
			cloned.SupportedModels[k] = v
		}
	}

	if source.ModelMapping != nil {
		cloned.ModelMapping = make(map[string]string, len(source.ModelMapping))
		for k, v := range source.ModelMapping {
			cloned.ModelMapping[k] = v
		}
	}
	if source.Headers != nil {
		cloned.Headers = make(map[string]string, len(source.Headers))
		for k, v := range source.Headers {
			cloned.Headers[k] = v
		}
	}
	if source.RequestIdentity != nil {
		identity := cloneProviderRequestIdentity(*source.RequestIdentity)
		cloned.RequestIdentity = &identity
	}
	cloned.ModelRequestIdentities = cloneProviderRequestIdentityMap(source.ModelRequestIdentities)
	cloned.PiModels = clonePiModelEntries(source.PiModels)
	cloned.PiModelOverrides = clonePiModelOverrides(source.PiModelOverrides)

	// 7. 添加到列表并保存（使用内部方法避免死锁）
	providers = append(providers, *cloned)
	if err := ps.saveProvidersLocked(kind, providers); err != nil {
		return nil, fmt.Errorf("保存副本失败: %w", err)
	}

	return cloned, nil
}

// IsModelSupported 检查 provider 是否支持指定的模型
// 支持条件：1) 模型在 SupportedModels 中（精确或通配符匹配）
//  2. 模型在 ModelMapping 的 key 中（精确或通配符匹配）
func (p *Provider) IsModelSupported(modelName string) bool {
	// 向后兼容：如果未配置白名单和映射，假设支持所有模型
	if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
		(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
		return true
	}

	// 场景 A：Provider 原生支持该模型（精确匹配）
	if p.SupportedModels != nil && p.SupportedModels[modelName] {
		return true
	}

	// 场景 A+：Provider 原生支持该模型（通配符匹配）
	if p.SupportedModels != nil {
		for supportedModel := range p.SupportedModels {
			if matchWildcard(supportedModel, modelName) {
				return true
			}
		}
	}

	// 场景 B：Provider 通过映射支持该模型（精确匹配）
	if p.ModelMapping != nil {
		if _, exists := p.ModelMapping[modelName]; exists {
			return true
		}

		// 场景 B+：通过通配符映射支持
		for pattern := range p.ModelMapping {
			if matchWildcard(pattern, modelName) {
				return true
			}
		}
	}

	// 场景 C：不支持
	return false
}

// GetEffectiveModel 获取实际应该使用的模型名
// 如果存在映射（精确或通配符），返回映射后的模型名；否则返回原模型名
func (p *Provider) GetEffectiveModel(requestedModel string) string {
	if p.ModelMapping == nil || len(p.ModelMapping) == 0 {
		return requestedModel
	}

	// 优先查找精确映射
	if mappedModel, exists := p.ModelMapping[requestedModel]; exists {
		return mappedModel
	}

	// 查找通配符映射
	for pattern, replacement := range p.ModelMapping {
		if matchWildcard(pattern, requestedModel) {
			return applyWildcardMapping(pattern, replacement, requestedModel)
		}
	}

	// 无映射，返回原模型名
	return requestedModel
}

// GetEffectiveEndpoint 获取有效的 API 端点
// 优先使用用户配置的端点，否则使用平台默认
func (p *Provider) GetEffectiveEndpoint(defaultEndpoint string) string {
	ep := strings.TrimSpace(p.APIEndpoint)
	if ep == "" {
		return defaultEndpoint
	}

	// 校验：必须是相对路径，不能是完整 URL
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
		log.Printf("[Provider] 警告: apiEndpoint 应该是相对路径（如 /v1/chat/completions），而非完整 URL: %s，使用默认端点", ep)
		return defaultEndpoint
	}

	// 确保以 / 开头
	if !strings.HasPrefix(ep, "/") {
		ep = "/" + ep
	}

	return ep
}

// UpstreamProtocolType 上游协议类型
type UpstreamProtocolType string

const (
	// UpstreamProtocolAnthropic Anthropic Messages API（默认）
	UpstreamProtocolAnthropic UpstreamProtocolType = "anthropic"
	// UpstreamProtocolOpenAIChat OpenAI Chat Completions API
	UpstreamProtocolOpenAIChat UpstreamProtocolType = "openai_chat"
	// UpstreamProtocolOpenAIResponses OpenAI Responses API
	UpstreamProtocolOpenAIResponses UpstreamProtocolType = "openai_responses"
	// UpstreamProtocolGoogle Google Generative AI 原生协议
	UpstreamProtocolGoogle UpstreamProtocolType = "google"
	// UpstreamProtocolAuto 自动检测
	UpstreamProtocolAuto UpstreamProtocolType = "auto"
)

// GetUpstreamProtocol 获取上游协议类型
// 空值或无效值默认返回 anthropic
func (p *Provider) GetUpstreamProtocol() UpstreamProtocolType {
	protocol := strings.TrimSpace(strings.ToLower(p.UpstreamProtocol))
	switch protocol {
	case "openai_chat", "openai-chat", "openai":
		return UpstreamProtocolOpenAIChat
	case "openai_responses", "openai-responses", "responses":
		return UpstreamProtocolOpenAIResponses
	case "google", "google-generative-ai", "gemini", "gemini_native":
		return UpstreamProtocolGoogle
	case "auto":
		return UpstreamProtocolAuto
	default:
		return UpstreamProtocolAnthropic
	}
}

// DetectUpstreamProtocol 根据端点自动检测上游协议
// 用于 auto 模式的启发式判断
func DetectUpstreamProtocol(endpoint string) UpstreamProtocolType {
	ep := strings.ToLower(endpoint)
	// 检测 OpenAI Chat Completions 端点
	if strings.Contains(ep, "/chat/completions") {
		return UpstreamProtocolOpenAIChat
	}
	if strings.Contains(ep, "/responses") {
		return UpstreamProtocolOpenAIResponses
	}
	// 默认 Anthropic
	return UpstreamProtocolAnthropic
}

// ResolveUpstreamProtocol 解析最终的上游协议
// 如果是 auto 模式，根据端点自动检测
func (p *Provider) ResolveUpstreamProtocol(effectiveEndpoint string) UpstreamProtocolType {
	protocol := p.GetUpstreamProtocol()
	if protocol == UpstreamProtocolAuto {
		return DetectUpstreamProtocol(effectiveEndpoint)
	}
	return protocol
}

func resolveProviderUpstreamProtocol(platform string, provider Provider, effectiveEndpoint string) UpstreamProtocolType {
	if strings.TrimSpace(provider.UpstreamProtocol) != "" {
		return provider.ResolveUpstreamProtocol(effectiveEndpoint)
	}
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "codex":
		return UpstreamProtocolOpenAIResponses
	case "reasonix", "pi":
		return UpstreamProtocolOpenAIChat
	default:
		return UpstreamProtocolAnthropic
	}
}

// ValidateConfiguration 验证 provider 的模型配置
// 返回验证错误列表（空则表示验证通过）
func (p *Provider) ValidateConfiguration() []string {
	errors := make([]string, 0)

	// 规则 1：ModelMapping 的 value 必须在 SupportedModels 中
	// 仅当两者都有实际内容时才校验（空 map 不触发校验）
	if len(p.ModelMapping) > 0 && len(p.SupportedModels) > 0 {
		for externalModel, internalModel := range p.ModelMapping {
			// 检查是否为通配符映射
			if strings.Contains(internalModel, "*") {
				// 通配符映射暂不验证（需要具体请求才能展开）
				continue
			}

			// 精确映射需要验证
			supported := false
			if p.SupportedModels[internalModel] {
				supported = true
			} else {
				// 检查通配符白名单
				for supportedPattern := range p.SupportedModels {
					if matchWildcard(supportedPattern, internalModel) {
						supported = true
						break
					}
				}
			}

			if !supported {
				errors = append(errors, fmt.Sprintf(
					"模型映射无效：'%s' -> '%s'，目标模型 '%s' 不在 supportedModels 中",
					externalModel, internalModel, internalModel,
				))
			}
		}
	}

	// 允许仅配置 modelMapping（无 supportedModels 时不阻塞保存）
	// 用户可能只想映射模型名，不需要白名单过滤

	// 规则 3 移除：自映射不会破坏功能，最多是无效配置，不阻塞保存

	if scheme := strings.ToLower(strings.TrimSpace(p.AuthScheme)); scheme != "" {
		switch scheme {
		case "bearer", "x-api-key", "custom", "none":
		default:
			errors = append(errors, fmt.Sprintf("不支持的认证方式: %s", p.AuthScheme))
		}
		if scheme == "custom" && strings.TrimSpace(p.AuthHeader) == "" {
			errors = append(errors, "自定义认证方式必须填写 authHeader")
		}
	}
	if strings.TrimSpace(p.AuthHeader) != "" {
		if err := validateHeaderNameAndValue(p.AuthHeader, p.APIKey); err != nil {
			errors = append(errors, err.Error())
		}
	}
	normalizedHeaders, err := canonicalizeHeaderMap(p.Headers)
	if err != nil {
		errors = append(errors, err.Error())
	}
	for key, value := range normalizedHeaders {
		if err := validateAdditionalHeader(key, value); err != nil {
			errors = append(errors, err.Error())
		}
	}
	if err := applyUserAgentPolicy(map[string]string{}, *p); err != nil {
		errors = append(errors, err.Error())
	}
	actualProtocol := string(resolveProviderUpstreamProtocol("pi", *p, p.GetEffectiveEndpoint("/v1/messages")))
	if p.RequestIdentity != nil {
		for _, message := range validateProviderRequestIdentity(*p.RequestIdentity, actualProtocol) {
			errors = append(errors, "requestIdentity: "+message)
		}
	}
	for model, identity := range p.ModelRequestIdentities {
		trimmedModel := strings.TrimSpace(model)
		if trimmedModel == "" || strings.Contains(trimmedModel, "*") {
			errors = append(errors, fmt.Sprintf("modelRequestIdentities.%s 的模型 ID 不能为空且不能包含 '*'", model))
			continue
		}
		for _, message := range validateProviderRequestIdentity(identity, actualProtocol) {
			errors = append(errors, fmt.Sprintf("modelRequestIdentities.%s: %s", model, message))
		}
	}

	p.configErrors = errors
	p.configValidated = true
	return append([]string(nil), errors...)
}

func (p *Provider) CachedValidationErrors() []string {
	if p.configValidated {
		return append([]string(nil), p.configErrors...)
	}
	return p.ValidateConfiguration()
}

func canonicalizeProviderHeaderMaps(provider *Provider) error {
	if provider == nil {
		return nil
	}
	var err error
	provider.Headers, err = canonicalizeHeaderMap(provider.Headers)
	if err != nil {
		return err
	}
	if err := canonicalizeProviderRequestIdentity(provider.RequestIdentity); err != nil {
		return fmt.Errorf("requestIdentity.headers: %w", err)
	}
	for model, identity := range provider.ModelRequestIdentities {
		if err := canonicalizeProviderRequestIdentity(&identity); err != nil {
			return fmt.Errorf("modelRequestIdentities.%s.headers: %w", model, err)
		}
		provider.ModelRequestIdentities[model] = identity
	}
	for index := range provider.PiModels {
		provider.PiModels[index].Headers, err = canonicalizeHeaderMap(provider.PiModels[index].Headers)
		if err != nil {
			return fmt.Errorf("piModels[%d].headers: %w", index, err)
		}
	}
	for modelID, override := range provider.PiModelOverrides {
		override.Headers, err = canonicalizeHeaderMap(override.Headers)
		if err != nil {
			return fmt.Errorf("piModelOverrides.%s.headers: %w", modelID, err)
		}
		provider.PiModelOverrides[modelID] = override
	}
	return nil
}

// matchWildcard 通配符匹配函数
// 支持 * 通配符，如 "claude-*" 匹配 "claude-sonnet-4"
func matchWildcard(pattern, text string) bool {
	// 如果没有通配符，使用精确匹配
	if !strings.Contains(pattern, "*") {
		return pattern == text
	}

	// 简化实现：只支持单个 * 通配符
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		// 前缀 + * 或 * + 后缀
		prefix, suffix := parts[0], parts[1]
		return strings.HasPrefix(text, prefix) && strings.HasSuffix(text, suffix)
	}

	// 多个 * 的情况（更复杂，暂不支持）
	return false
}

// applyWildcardMapping 应用通配符映射
// 将 pattern 中的 * 匹配部分替换到 replacement 的 * 位置
// 示例: pattern="claude-*", replacement="anthropic/claude-*", input="claude-sonnet-4"
//
//	输出: "anthropic/claude-sonnet-4"
func applyWildcardMapping(pattern, replacement, input string) string {
	// 如果 pattern 或 replacement 没有通配符，直接返回 replacement
	if !strings.Contains(pattern, "*") || !strings.Contains(replacement, "*") {
		return replacement
	}

	// 提取通配符匹配的部分
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return replacement // 不支持多个通配符
	}

	prefix, suffix := parts[0], parts[1]

	// 验证 input 确实匹配 pattern
	if !strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return replacement
	}

	// 提取中间部分
	wildcardPart := input[len(prefix) : len(input)-len(suffix)]

	// 替换 replacement 中的 *
	return strings.Replace(replacement, "*", wildcardPart, 1)
}
