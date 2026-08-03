# code-switch-R Provider Management

本上下文描述 code-switch-R 中 AI CLI 平台、供应商、本地转发和托管配置之间的领域边界。它用于统一产品和代码讨论中的术语，避免把账号、供应商、平台启用状态和请求调度混为一谈。

## 平台与运行模式

**Grok Build 平台**:
管理自定义上游供应商，并通过本地 Relay 为 Grok Build 提供请求转发的产品区域。
_Avoid_: Grok OAuth、Grok 账号平台、官方模式

**Grok Build OAuth 平台**:
管理 xAI 官方 OAuth 账号、账号切换和额度信息的产品区域。它不拥有自定义供应商，也不参与供应商调度。
_Avoid_: Grok Build Provider、OAuth Provider、账号池

**Grok 运行模式**:
Grok Build 当前实际使用的连接方式，只能是自定义 Relay、官方 OAuth 或未接管三者之一。
_Avoid_: Tab 状态、账号状态、Provider Enabled

**未接管模式**:
code-switch-R 不再拥有 Grok Build 当前连接字段，并恢复用户在首次接管前的配置语义。
_Avoid_: 官方模式、停用账号

## 供应商与模型

**Grok Provider**:
Grok Build 平台中的一个自定义上游连接定义，包含认证、协议、真实模型和调度属性。
_Avoid_: OAuth 账号、Grok Profile、账号渠道

**稳定入站模型**:
Grok Build 向本地 Relay 请求时使用的固定模型标识 `grok-build`，不随上游供应商变化。
_Avoid_: 默认上游模型、真实模型

**真实上游模型**:
某个 Grok Provider 实际接收的模型标识，由稳定入站模型显式映射得到。
_Avoid_: `grok-build`、模型别名

## OAuth 账号

**OAuth 账号**:
一份可独立刷新和查询额度的 xAI 官方身份凭据。多个账号可以保存，但不会形成自动调度集合。
_Avoid_: Provider、账号池成员、Relay 节点

**已应用账号**:
当前被写入 Grok Build 官方认证配置、供 Grok Build 直接访问 xAI 的唯一 OAuth 账号。
_Avoid_: 默认账号、轮询账号、当前 Provider

**账号切换**:
由用户显式选择另一个 OAuth 账号替换已应用账号的操作，不包含自动重试、轮询或故障转移。
_Avoid_: 账号轮换、账号池降级

**额度快照**:
一次 OAuth 额度查询得到的套餐、剩余比例、重置时间和采集状态。它不是精确的请求日志或 Token 用量。
_Avoid_: 用量日志、成本统计、请求统计

## 配置所有权

**受管字段**:
code-switch-R 在 Grok 配置中明确拥有并可恢复的最小字段集合。其他字段始终属于用户或其他工具。
_Avoid_: 整份配置、配置模板

**接管状态**:
记录受管字段原始值、注入值和目标路径的持久状态，用于模式切换、冲突检测和恢复。
_Avoid_: 配置备份、当前 Provider

**外部修改冲突**:
受管字段的当前值不再等于 code-switch-R 最后注入值，说明其他主体在接管期间修改了同一所有权范围。
_Avoid_: TOML 解析错误、无关字段变化

## Gemini 领域

**Gemini CLI 账号**:
Gemini CLI / Code Assist 的 OAuth 身份及其可刷新生命周期，负责账号切换、令牌刷新和官方配额快照。它不是 Gemini Native Provider，也不参与 Native Relay 路由。
_Avoid_: Native Credential、Provider、模型目录

**Gemini Native Provider**:
可被 Native Relay 选择的上游连接定义，包含协议、完整端点、模型能力策略和路由属性。它必须显式关联认证凭据，不根据名称或 Key 前缀猜认证类型。
_Avoid_: Gemini CLI 账号、OAuth 账号、模型条目

**Credential**:
与 Provider 显式关联的认证对象，类型由配置声明，可为 API Key、Native Bearer OAuth、Vertex 凭据或第三方网关凭据。Credential 不承载模型目录和路由规则。
_Avoid_: selectedType、Key 前缀、Provider 名称

**Gemini 模型目录**:
按 Provider 管理的模型能力快照，区分远程发现、内置回退和用户覆盖，并记录来源与刷新时间。它描述模型能力，不代表账号额度。
_Avoid_: 全局猜测模型表、账号配额

**Gemini 路由**:
把入站协议和模型匹配规则映射到有序 Provider 候选集的规则。候选集才执行优先级、轮询、重试、降级、黑名单和冷却；未知模型不静默遍历所有 Provider。
_Avoid_: 按供应商名称匹配、按 Key 前缀选认证

**Gemini Native Relay**:
对 Gemini 原生 API 提供模型目录、模型详情、生成、流式生成和 `countTokens` 等端点，并统一执行认证、模型路由、上游转发、用量记录和故障处理的本地服务。
_Avoid_: 只有 `/generateContent` 的透明 HTTP 转发

**协议转换**:
在 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 和 Gemini Native 之间，通过统一中间表示转换请求、响应、工具调用、多模态内容、推理状态、usage 和流式事件。不能保持语义时必须显式拒绝。
_Avoid_: 静默丢弃工具参数、图片、文档、推理签名或流式事件
