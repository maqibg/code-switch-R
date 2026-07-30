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
