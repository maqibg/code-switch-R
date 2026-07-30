# Grok Build 与 Grok Build OAuth 支持设计

状态: 已实现，待真实账号手动验收

日期: 2026-07-30

## 1. 目标

为 code-switch-R 增加两个彼此独立的产品平台:

- **Grok Build**: 管理自定义 Provider，通过独立 Relay 路由转发 Grok Build 请求，并复用现有 Level、轮询、降级、黑名单、协议转换、日志和统计能力。
- **Grok Build OAuth**: 管理 xAI 官方 OAuth 多账号、手动账号切换和额度查询。官方请求由 Grok Build 直接访问 xAI，不经过 Relay。

两个平台可以同时保存数据，但同一个 Grok Build 配置只能处于一种运行模式:

```text
Grok Build Relay <-> Grok Build OAuth <-> 未接管
```

模式切换必须原子化。失败时回滚本次已经完成的文件和状态修改，不能让两个页面同时显示为已应用。

## 2. 分析基线

本设计基于以下源码版本完成:

| 项目 | Commit | 主要参考点 |
|---|---|---|
| code-switch-R | `4eda520045609984a8e8796f2f3c39f517e07973` | Provider、Relay、协议矩阵、Pi 托管、日志统计 |
| 1parado/grok-build-switch | `fe282821c90eeba56bc668a87f36547d6cfafb6c` | CPA/Grok auth 解析、批量目录导入、Profile 配置切换 |
| farion1231/cc-switch | `56fb46c09310ff52dabefd2b32f0e799e8357d9e` | 独立 Grok Build 平台、单一当前 Provider、官方与自定义配置互斥 |
| coulsontl/ai-toolbox | `f62a7d72f6678966547bc9fef66e49efd4fd41d8` | Provider/Common/runtime 字段所有权、官方账号切换、额度解析 |

参考结论:

- `ai-toolbox` 的字段所有权模型最适合作为配置接管主参考。
- `cc-switch` 证明官方与自定义运行配置最终只能选择一个当前状态。
- `grok-build-switch` 的凭据解析和批量导入有参考价值，但自动账号池、会话哈希选号、轮询账号和源文件删除不符合本设计。

## 3. 非目标

首版不实现:

- OAuth 账号池、自动轮换、自动故障切换或会话粘滞选号。
- Grok MCP、Skills、Prompt、插件、会话工作台和生图能力。
- OAuth 请求级日志、Token 统计或成本统计。
- 目录监控和自动热导入。
- Responses compact 到 Chat 或 Anthropic 的协议转换。
- 多个稳定入站模型、通配符模型映射或每个 Provider 多目标模型。
- 系统密钥库、凭据加密或机器绑定存储。

## 4. 平台与持久化边界

### 4.1 Grok Build

Grok Build 使用独立 Provider 存储和内部平台 ID `grok`。Provider 继续复用现有 `Provider` 数据结构，包括:

- URL、API Key、认证方式和自定义 Header。
- `UpstreamProtocol`，支持 `openai_responses`、`openai_chat` 和 `anthropic`。
- `Enabled`、`Level`、轮询、黑名单和模型白名单。
- `ModelMapping`。

Grok Provider 不得写入 WorkBuddy 的 `codex` Provider 文件，也不得共享 `codex` 黑名单目标。

### 4.2 Grok Build OAuth

OAuth 账号使用独立便携式 JSON 存储，位于可执行文件同级 `.code-switch-R` 数据目录。存储包含账号元数据、OAuth 凭据、额度快照、刷新状态和唯一已应用账号标识。

OAuth 账号不是 Provider，不进入 Provider 表单、Level、轮询或黑名单。批量导入只新增或更新账号记录，不自动应用账号。

### 4.3 运行模式状态

运行模式至少表达以下状态:

```text
unmanaged
grok_relay
grok_oauth(account_id)
```

Provider 的 `Enabled` 只表示该 Provider 在 Grok Relay 模式中可参与调度，不表示 Grok Relay 当前已经应用到 Grok Build。页面必须区分“Provider 已启用”和“平台已应用”。

## 5. Relay 契约

### 5.1 路由

首版显式注册以下路由，不增加 `/grok/v1/*` catch-all:

| Method | Route | 行为 |
|---|---|---|
| POST | `/grok/v1/responses` | Grok Responses 入站，进入 Provider 调度和协议矩阵 |
| POST | `/grok/v1/responses/compact` | 仅选择原生 Responses Provider，协议语义原样透传 |
| GET | `/grok/v1/models` | 返回本地合成的稳定模型目录，不请求上游 |

未知 Grok 路径必须返回明确的未注册错误，不能未经验证自动透传。

### 5.2 本地模型目录

`GET /grok/v1/models` 固定返回 `grok-build`:

```json
{
  "object": "list",
  "data": [
    {
      "id": "grok-build",
      "object": "model",
      "owned_by": "code-switch-r"
    }
  ]
}
```

模型目录请求不计入对话用量、Provider 成功率或成本统计。

### 5.3 模型映射

Grok CLI 始终向 Relay 发送稳定模型 `grok-build`。每个 Grok Provider 必须显式保存一个精确映射:

```text
grok-build -> provider-real-model
```

Provider 页面使用单一“上游模型”字段，内部继续复用 `ModelMapping`。`SupportedModels` 保存映射后的真实上游模型，用于现有配置校验。即使上游模型也叫 `grok-build`，仍保存显式自映射。

没有上游模型映射的 Provider 不能启用，不能把稳定模型名直接试探性发送给上游。

### 5.4 Provider 调度

`/responses` 复用现有调度顺序:

1. Grok 平台 Provider 存储。
2. 启用状态和必填配置校验。
3. 稳定模型支持检查。
4. Grok 范围黑名单检查。
5. Level 分组。
6. 同 Level 轮询。
7. 请求失败降级或固定黑名单模式重试。

模型映射在 Provider 选中后、协议转换前完成。映射失败属于配置错误，应跳过该 Provider，但不将其记为上游故障。

### 5.5 协议处理

Grok 入站协议固定为 OpenAI Responses。上游处理规则:

| 上游 | 行为 |
|---|---|
| OpenAI Responses | 保持 Responses 语义，应用认证、Header 和模型映射后转发 |
| OpenAI Chat | 使用现有 Responses -> Chat 转换和流式转换 |
| Anthropic Messages | 使用现有 Responses -> Anthropic 转换和流式转换 |

无法保留工具调用、reasoning 或流式事件语义时必须显式拒绝，不能静默丢字段。流式响应一旦提交，不得切换 Provider 或追加普通 JSON 错误。

`/responses/compact` 不进入跨协议转换。调度前排除 Chat 和 Anthropic Provider，这种排除不计失败，也不触发黑名单。

## 6. 日志、统计和黑名单

Grok Relay 请求使用独立 `platform=grok` 范围:

- `request_log` 和 `relay_attempt` 记录 Grok 请求和 Provider 尝试。
- `provider_blacklist` 使用 Grok 平台目标，不能影响同名 WorkBuddy Provider。
- 日志页和统计页支持 Grok 平台筛选。
- 成功仍定义为 HTTP 2xx 且 `error_type` 为空。
- Token 和成本解析复用现有 Responses、Chat、Anthropic 解析器。

OAuth 模式不经过 Relay，因此不生成请求日志、Token、成本、Provider 成功率或黑名单数据。OAuth 页面只展示账号额度快照，不能用额度差值推算并伪装成精确 Token 用量。

## 7. Grok 配置路径

路径解析优先级:

1. 应用设置中显式选择的 Grok 配置目录，同时决定 `config.toml` 和 `auth.json`。
2. 未显式设置时，`GROK_CONFIG` 决定 `config.toml` 完整路径。
3. `GROK_HOME` 决定 `auth.json`，并在没有 `GROK_CONFIG` 时决定 `config.toml`。
4. 默认使用 `~/.grok/config.toml` 和 `~/.grok/auth.json`。

页面必须显示解析后的两个完整路径。应用不修改系统或用户环境变量。

接管状态记录实际文件路径。接管期间解析路径发生变化时，停止自动操作并要求先处理旧路径状态，不能在新路径上静默建立第二份接管状态。

目录不存在时可以按用户操作创建。现有文件无法解析时必须报错并保持原文件不变。

## 8. config.toml 字段级接管

### 8.1 Relay 模式注入

Relay 模式只注入:

```toml
[models]
default = "code-switch-r"

[model.code-switch-r]
model = "grok-build"
base_url = "http://127.0.0.1:18100/grok/v1"
api_key = "<local-relay-key>"
api_backend = "responses"
```

实际地址使用当前 Relay 监听地址，不能硬编码假设所有实例都使用 `18100`。

### 8.2 受管字段

所有权范围仅为:

- `[models].default`
- 整个 `[model.code-switch-r]`

首次接管记录:

- 目标 `config.toml` 路径。
- 两个受管字段原来是否存在。
- 原始值。
- 本次注入值及其规范化哈希。
- 当前运行模式。

用户其他模型、MCP、插件、subagents、注释和未知字段必须保留。禁止通过 TOML 结构体完整反序列化再序列化造成无关重排或注释丢失。

### 8.3 外部修改冲突

每次切换 Provider、进入 OAuth、停用接管或重新注入前，比较当前受管字段与最后注入哈希:

- 只有无关字段变化: 正常继续并保留变化。
- 受管字段变化: 停止操作并报告冲突。
- 首次接管已经存在 `[model.code-switch-r]`: 直接报告命名冲突。

冲突处理只能由用户显式选择:

- **重新接管**: 先创建备份，再按当前应用状态覆盖受管字段。
- **放弃接管**: 保留当前文件，清除应用接管状态，不恢复旧值。

不提供自动整份备份恢复。

### 8.4 OAuth 官方态

进入 OAuth 模式时:

1. 校验 Relay 模式受管字段没有外部冲突。
2. 删除应用注入的 `[model.code-switch-r]`。
3. 删除 `[models].default`，让 Grok Build 回落到内置官方模型。
4. 保留所有其他 `[model.*]` 和非受管字段。
5. 将选中账号合并到 `auth.json`。

OAuth 模式期间，“`models.default` 不存在”属于受管状态。外部重新添加该字段时视为冲突。

从 OAuth 切回 Relay 时重新注入 `default = "code-switch-r"`。只有进入未接管模式时，才恢复首次接管前保存的原始 `models.default` 和 `model.code-switch-r` 存在状态。

## 9. OAuth 账号管理

### 9.1 账号来源

支持:

- xAI Device Code OAuth 登录。
- 从当前 Grok `auth.json` 导入已有官方账号。
- 一次选择多个 CPA `xai-*.json`。
- 一次性递归扫描用户选择目录中的 JSON 文件。

不实现目录监控。导入过程只读源文件，删除应用内账号时也不得修改、移动或删除源文件。

### 9.2 导入和身份识别

单个文件最大读取大小应有明确限制，超限、读取失败、JSON 解析失败和不支持格式都必须进入逐文件结果，不能静默跳过。

账号身份优先使用稳定的 issuer、client ID 和 subject；缺少 subject 时可使用邮箱等可解释的回退标识。重复导入同一身份时更新凭据和元数据，不新增重复账号。无法可靠识别时应显示来源文件和独立导入结果。

导入账号后不自动应用，不自动触发账号切换。

### 9.3 便携式凭据存储

凭据以明文 JSON 保存到 `.code-switch-R` 数据目录。该决定服务于便携性，不宣称抵御本地磁盘访问。

仍需满足:

- 原子写入和进程内串行更新。
- Token 不进入日志、普通错误、前端列表 DTO、请求日志或统计。
- 前端只获取 `configured`、过期时间、邮箱、账号状态等脱敏字段。
- 移动整个便携目录时账号数据随应用迁移。

### 9.4 手动账号切换

任意时刻最多有一个已应用账号。账号应用流程:

1. 读取账号记录。
2. 如果该账号当前已应用，先从 live `auth.json` 同步 Grok CLI 可能更新的同身份 Token 字段。
3. Token 剩余不足 30 分钟时刷新。
4. 字段级合并选中账号的 xAI OAuth scope，保留 `auth.json` 中其他 scope 和未知字段。
5. 更新唯一已应用账号标识。

账号切换失败时保留原已应用账号和 live 配置，不能产生无账号的半切换状态。失败不自动尝试其他账号，也不自动切换到 Relay。

### 9.5 Token 刷新

按需刷新触发点:

- 应用账号。
- 查询账号额度。
- 用户手动刷新。

不为了保活而后台周期性刷新所有未应用账号。刷新成功时保存旋转后的 access token、refresh token 和过期时间。`invalid_grant` 等永久失败将账号标记为需要重新登录，但保留账号记录和可操作错误。

### 9.6 额度

当前参考实现使用以下 xAI Grok CLI 接口:

```text
GET https://cli-chat-proxy.grok.com/v1/billing?format=credits
GET https://cli-chat-proxy.grok.com/v1/billing
GET https://cli-chat-proxy.grok.com/v1/user?include=subscription
```

这些不是本设计可依赖的稳定公开协议，解析器必须允许字段缺失和结构变化。

展示字段:

- 套餐类型。
- 周额度剩余百分比和重置时间。
- 月额度剩余百分比和重置时间，仅在接口明确返回有效月额度时展示。
- 最近成功刷新时间、数据时间和最近错误。

刷新规则:

- 进入 OAuth 页面时只自动刷新当前已应用账号。
- 缓存未超过 10 分钟时直接使用。
- 支持单账号手动刷新。
- 支持刷新全部，并发上限为 2，单账号失败不影响其他账号。
- 不在应用后台周期性刷新全部账号。
- 字段缺失显示“暂无数据”，不能推断为 `0%`。
- 请求失败或结构变化时保留上次成功数据，并显示最新错误和数据时间。

## 10. 前端范围

### 10.1 首页 Grok Build 平台

Grok Build 是首页一级 Provider Tab，不保留独立 `/grok` 路由或侧边栏入口。它复用首页的 Provider 卡片、排序、Level、统计、黑名单和 CRUD，但不提供单 Provider 直连应用。至少包含:

- Provider 列表、启用状态、Level 和当前平台运行状态。
- 专用 Provider 表单: URL、API Key、认证、Header、Responses/Chat/Anthropic 上游协议和单一上游模型；模型可手填或从上游单选拉取。
- Relay 启用、停用、重新接管和冲突处理。启用 Relay 前必须存在至少一个有效的启用 Provider。
- 与 OAuth 模式切换时的明确确认；切换失败保留当前模式。

### 10.2 首页 Grok Build OAuth 平台

Grok Build OAuth 是首页一级账号管理 Tab，不是 Provider 平台，不进入 Relay、日志、统计、成本或黑名单。至少包含:

- Device Code 登录。
- 当前 `auth.json` 导入、多文件导入和目录导入。
- 账号列表、当前已应用账号和需要重新登录状态。
- 套餐、周/月额度、重置时间、刷新时间和错误。
- 单账号刷新、刷新全部、应用账号和移除账号操作。

- OAuth 工具栏位于首页平台操作区，提供 Device Code 登录、导入菜单和刷新全部额度；账号行内提供应用、刷新和删除操作。
- 仅在用户进入 OAuth Tab 时刷新当前已应用账号，且遵守 10 分钟额度缓存。

## 11. 错误和回滚原则

- 配置或凭据解析失败时拒绝操作，不使用空配置继续。
- 文件写入、状态写入和模式切换必须保留具体路径与操作上下文。
- 模式切换采用可回滚步骤，只有所有目标文件和持久状态成功后才更新前端已应用状态。
- 响应已经提交后不得执行 Provider 降级。
- compact 跨协议不支持属于客户端能力限制，不记 Provider 失败。
- OAuth 刷新失败属于账号状态，不触发 Provider 黑名单。
- 不添加 mock 成功、静默 fallback 或伪造额度与统计。

## 12. 验收标准

### 12.1 自动化测试

- 路径优先级: 应用目录、`GROK_CONFIG`、`GROK_HOME` 和默认路径。
- TOML 字段级注入、正常恢复、外部冲突、同名表冲突和原子回滚。
- Relay、OAuth、未接管三种模式互斥。
- CPA、Grok auth、多文件、多账号、重复账号和逐文件错误。
- 导入与删除不会修改源文件。
- Token 按需刷新、Token 轮换、永久失效和禁止自动换号。
- 三个额度接口的正常、缺失、变体和失败响应。
- `/grok/v1/responses` 对 Responses、Chat、Anthropic 的非流式和流式转发。
- `/grok/v1/responses/compact` 只选择原生 Responses Provider。
- `/grok/v1/models` 只返回稳定模型且不进入 Provider 统计。
- `grok-build` 到真实上游模型的精确映射。
- Grok Provider、黑名单、日志和统计与其他平台隔离。
- OAuth 模式不生成请求级日志或虚假 Token/成本统计。
- API、日志和错误不会输出 OAuth Token 原文。

### 12.2 构建验证

- 运行相关 Go 定向测试和 `go test ./...`。
- Go 导出签名变化后运行 `wails3 task common:generate:bindings`。
- 运行前端 TypeScript/Vue 构建。
- 完成 Windows amd64、`CGO_ENABLED=0` 的单文件测试构建。

### 12.3 真实账号手动验证

- Device Code 登录。
- 多个第三方凭据文件和目录导入。
- 两个 OAuth 账号来回切换。
- 套餐和周/月额度查询。
- Grok CLI 在 Relay、OAuth、未接管三种模式下各完成一次真实请求。
- 人工修改受管字段后，确认冲突会阻止切换和恢复。

自动化测试不调用真实 xAI，也不能使用伪造成功结果替代上述手动验证。

## 13. 建议实施顺序

1. 注册 `grok` 平台、独立 Provider 存储和日志/黑名单范围。
2. 增加稳定模型、`/models`、`/responses` 和 compact 路由及测试。
3. 接入三协议转换、Level、轮询和降级。
4. 实现路径解析、字段级 TOML 接管、模式状态和冲突处理。
5. 实现 OAuth 账号存储、Device Code、批量导入和手动切换。
6. 实现 Token 按需刷新和额度解析。
7. 完成两个独立前端平台和 bindings。
8. 执行完整自动化、构建和真实账号手动验收。
