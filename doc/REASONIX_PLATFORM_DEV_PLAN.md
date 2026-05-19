# Reasonix 内置平台开发计划

## 概述

为 Code Switch R 新增 Reasonix 作为内置代理平台。Reasonix 是基于 DeepSeek API 的终端 AI 编程代理，使用 OpenAI 兼容的 `/chat/completions` 协议，认证方式为 Bearer Token。

---

## 一、协议与架构适配分析

### 1.1 协议对比

| 维度 | Reasonix | 代理层处理方式 |
|------|----------|---------------|
| API 端点 | `POST {baseUrl}/chat/completions` | 路由 `/reasonix/chat/completions` → 透传 |
| 认证 | `Authorization: Bearer {key}` | 默认 bearer 模式，无需特殊处理 |
| 流式传输 | SSE + `[DONE]` 终止符 | 与 Codex 一致，已支持 |
| 请求格式 | OpenAI Chat Completions（messages/tools/stream） | 直接透传，不做协议转换 |
| 响应格式 | `choices[].delta.content` + `usage` | 需新增 token 解析器 |

### 1.2 Token 用量字段映射

DeepSeek API 响应的 usage 结构：
```json
{
  "usage": {
    "prompt_tokens": 100,
    "completion_tokens": 50,
    "total_tokens": 150,
    "prompt_cache_hit_tokens": 80,
    "prompt_cache_miss_tokens": 20
  }
}
```

映射到 `ReqeustLog`：

| DeepSeek 字段 | ReqeustLog 字段 | 说明 |
|--------------|----------------|------|
| `usage.prompt_tokens` | `InputTokens` | 输入 token |
| `usage.completion_tokens` | `OutputTokens` | 输出 token |
| `usage.prompt_cache_hit_tokens` | `CacheReadTokens` | 缓存命中 |
| `choices[].delta.reasoning_content` 存在时 | `ReasoningTokens` | 需从 usage 中提取（如有） |

### 1.3 与现有 Codex 解析器的差异

现有 `CodexParseTokenUsageFromResponse` 解析路径：
- `response.usage.input_tokens` — Codex 特有的嵌套结构
- `response.usage.input_tokens_details.cached_tokens`
- `response.usage.output_tokens_details.reasoning_tokens`

DeepSeek/Reasonix 的路径不同：
- `usage.prompt_tokens` — 标准 OpenAI 顶层
- `usage.prompt_cache_hit_tokens` — DeepSeek 扩展字段
- `usage.completion_tokens_details.reasoning_tokens`（v4 模型可能有）

**结论**：需要新增 `ReasonixParseTokenUsageFromResponse` 解析函数。

---

## 二、配置文件处理

### 2.1 Reasonix 配置结构

文件路径：`~/.reasonix/config.json`

```json
{
  "apiKey": "sk-xxx",
  "baseUrl": "https://api.deepseek.com",
  "preset": "auto",
  "editMode": "review",
  "reasoningEffort": "max",
  ...
}
```

特征：
- **扁平 JSON 结构**（非嵌套 env 对象）
- `baseUrl` 和 `apiKey` 是顶层字段
- 优先级：环境变量 `DEEPSEEK_BASE_URL` > config.json `baseUrl` > 默认值
- 优先级：环境变量 `DEEPSEEK_API_KEY` > config.json `apiKey`

### 2.2 代理启用/禁用策略

**EnableProxy 流程：**
1. 读取 `~/.reasonix/config.json`（不存在则创建）
2. 保存 ProxyState（记录原始 baseUrl 和 apiKey）
3. 备份原始文件到 `~/.reasonix/cc-studio.back.config.json`
4. 修改 `baseUrl` → `http://127.0.0.1:18100/reasonix`
5. 修改 `apiKey` → `code-switch-r`（占位符，代理层替换为真实 key）
6. 写回文件（保留其他字段不变）

**DisableProxy 流程：**
1. 从 ProxyState 读取原始值
2. 恢复 `baseUrl` 和 `apiKey`（surgical restore，不覆盖用户其他修改）
3. 清理 ProxyState

**ProxyStatus 判断：**
- 读取 config.json 的 `baseUrl` 字段
- 比较是否等于 `http://127.0.0.1:18100/reasonix`（忽略尾部斜杠）

### 2.3 与其他平台的对比

| 平台 | 配置路径 | 修改方式 | 格式 |
|------|---------|---------|------|
| Claude | `~/.claude/settings.json` | `env.ANTHROPIC_BASE_URL` | 嵌套 env |
| Codex | `~/.codex/config.toml` | `base_url` | TOML |
| DeepSeekCode | `~/.deepseek-code/settings.json` | `env.DEEPSEEK_BASE_URL` | 嵌套 env |
| **Reasonix** | **`~/.reasonix/config.json`** | **顶层 `baseUrl`** | **扁平 JSON** |

---

## 三、默认值设计

### 3.1 认证方式

```
默认 ConnectivityAuthType: "bearer"
```

原因：Reasonix/DeepSeek API 使用标准 `Authorization: Bearer {key}` 认证。

### 3.2 默认端点

```
默认 apiEndpoint: "/chat/completions"
```

注意：DeepSeek API 路径是 `{baseUrl}/chat/completions`（无 `/v1` 前缀）。

### 3.3 默认测试模型

```
连通性测试模型: "deepseek-v4-flash"
备选: ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-chat"]
```

### 3.4 预设供应商卡片（Automation Cards）

```typescript
{
  id: 301,
  name: "DeepSeek 官方",
  apiUrl: "https://api.deepseek.com",
  apiKey: "",
  officialSite: "https://platform.deepseek.com",
  icon: "deepseek",
  tint: "#4D6BFE",
  accent: "#4D6BFE",
  enabled: true,
  connectivityAuthType: "bearer",
  upstreamProtocol: "openai_chat",
}
```

---

## 四、路由设计

### 4.1 代理路由

```go
// Reasonix 使用 OpenAI 兼容格式
router.POST("/reasonix/chat/completions", prs.proxyHandler("reasonix", "/chat/completions"))
router.GET("/reasonix/models", prs.modelsHandler("reasonix"))
```

### 4.2 URL 拼接验证

Reasonix 客户端行为：`${baseUrl}/chat/completions`

设置 `baseUrl = http://127.0.0.1:18100/reasonix` 后：
```
请求: POST http://127.0.0.1:18100/reasonix/chat/completions
路由匹配: /reasonix/chat/completions → proxyHandler("reasonix", "/chat/completions")
转发: https://api.deepseek.com/chat/completions (供应商 apiUrl + endpoint)
```

### 4.3 模型列表端点

Reasonix 客户端也会请求 `${baseUrl}/models`：
```
请求: GET http://127.0.0.1:18100/reasonix/models
路由匹配: /reasonix/models → modelsHandler("reasonix")
```

---

## 五、Token 解析器实现

### 5.1 新增解析函数

```go
func ReasonixParseTokenUsageFromResponse(data string, usage *ReqeustLog) {
    // 标准 OpenAI 格式（非流式或流式最终 chunk）
    maxIntInto(&usage.InputTokens, int(gjson.Get(data, "usage.prompt_tokens").Int()))
    maxIntInto(&usage.OutputTokens, int(gjson.Get(data, "usage.completion_tokens").Int()))
    // DeepSeek 扩展：缓存命中
    maxIntInto(&usage.CacheReadTokens, int(gjson.Get(data, "usage.prompt_cache_hit_tokens").Int()))
    // DeepSeek v4 推理 token（如有）
    maxIntInto(&usage.ReasoningTokens, int(gjson.Get(data, "usage.completion_tokens_details.reasoning_tokens").Int()))
}
```

### 5.2 注册到 ReqeustLogHook

```go
func ReqeustLogHook(c *gin.Context, kind string, usage *ReqeustLog) func(data []byte) (bool, []byte) {
    return func(data []byte) (bool, []byte) {
        payload := strings.TrimSpace(string(data))
        parserFn := ClaudeCodeParseTokenUsageFromResponse
        switch kind {
        case "codex":
            parserFn = CodexParseTokenUsageFromResponse
        case "gemini":
            parserFn = GeminiParseTokenUsageFromResponse
        case "reasonix":                              // 新增
            parserFn = ReasonixParseTokenUsageFromResponse
        }
        parseEventPayload(payload, parserFn, usage)
        return true, data
    }
}
```

---

## 六、日志与统计

### 6.1 日志记录

`platform` 字段值：`"reasonix"`

所有日志字段均适用，无需修改 schema：
- `input_tokens` ← `usage.prompt_tokens`
- `output_tokens` ← `usage.completion_tokens`
- `cache_read_tokens` ← `usage.prompt_cache_hit_tokens`
- `reasoning_tokens` ← `usage.completion_tokens_details.reasoning_tokens`
- `cache_create_tokens` ← 0（DeepSeek 无此概念）
- `ephemeral_5m_tokens` / `ephemeral_1h_tokens` ← 0
- `service_tier` ← ""（DeepSeek 无此概念）

### 6.2 统计页面

`useStatsDashboard.ts` 修改：
```typescript
const PLATFORM_ORDER: LogPlatform[] = ['claude', 'codex', 'gemini', 'deepseekcode', 'reasonix']

const platformStats = reactive<Record<LogPlatform, LogStats>>({
  claude: emptyStats(),
  codex: emptyStats(),
  gemini: emptyStats(),
  deepseekcode: emptyStats(),
  reasonix: emptyStats(),  // 新增
})
```

### 6.3 日志筛选

`Logs/Index.vue` 下拉框新增：
```html
<option value="reasonix">Reasonix</option>
```

### 6.4 模型定价

在 `resources/model-pricing/model_prices_and_context_window.json` 中添加：
```json
"deepseek-v4-flash": {
  "input_cost_per_token": 1e-07,
  "output_cost_per_token": 4e-07,
  "cache_read_input_token_cost": 1e-08,
  "max_input_tokens": 131072,
  "max_output_tokens": 8192
},
"deepseek-v4-pro": {
  "input_cost_per_token": 1.2e-06,
  "output_cost_per_token": 4.8e-06,
  "cache_read_input_token_cost": 1.2e-07,
  "max_input_tokens": 131072,
  "max_output_tokens": 8192
}
```

> 注：具体价格需以 DeepSeek 官方定价为准，上线前需核实。

---

## 七、MCP 服务管理

### 7.1 MCPServer 结构扩展

```go
type MCPServer struct {
    // ... 现有字段 ...
    EnabledInReasonix bool `json:"enabled_in_reasonix"`  // 新增
}
```

### 7.2 平台常量

```go
const platReasonix = "reasonix"
```

### 7.3 MCP 配置同步

Reasonix 的 MCP 配置存储在 `~/.reasonix/config.json` 的 `mcpServers` 字段：
```json
{
  "mcpServers": {
    "server-name": {
      "command": "npx",
      "args": ["-y", "some-mcp-server"],
      "env": {}
    }
  }
}
```

需实现 `syncReasonixServers()` 方法，读写该字段。

---

## 八、深链（Deep Link）支持

### 8.1 URL 格式

```
ccswitch://v1/import?resource=provider&app=reasonix&name=...&endpoint=...&apiKey=...
```

### 8.2 Config 合并逻辑

Reasonix 配置键映射：
- `apiKey` → Provider.APIKey
- `baseUrl` → Provider.Endpoint（提取路径部分）

### 8.3 验证

`deeplinkservice.go` 的 `ParseDeepLink()` switch 增加 `"reasonix"` case。

---

## 九、完整文件修改清单

### 后端（Go）— 新建 1 个文件，修改 10 个文件

| 文件 | 操作 | 内容 |
|------|------|------|
| `services/reasonixsettings.go` | **新建** | ReasonixSettingsService 完整实现 |
| `services/providerrelay.go` | 修改 | 1) 注册路由 2) `validateConfig()` 加平台 3) `ReqeustLogHook` 加 case 4) 新增 `ReasonixParseTokenUsageFromResponse` |
| `services/providerservice.go` | 修改 | `providerFilePath()` 加 `"reasonix"` case |
| `services/directapply_helpers.go` | 修改 | `providerFilePathNoCreate()` 加 case |
| `services/healthcheckservice.go` | 修改 | 平台列表 + `getEffectiveTestModel` + `getDefaultTestEndpoint` |
| `services/connectivitytestservice.go` | 修改 | 平台列表 + 默认测试模型 |
| `services/logservice.go` | 修改 | 平台列表 |
| `services/deeplinkservice.go` | 修改 | 验证 + switch case |
| `services/mcpservice.go` | 修改 | 常量 + MCPServer 字段 + sync 方法 |
| `main.go` | 修改 | 实例化 + 注册服务 |
| `services/networkservice.go` | 修改 | 如有平台列表需同步 |

### 前端（Vue/TypeScript）— 修改约 10 个文件

| 文件 | 内容 |
|------|------|
| `frontend/src/services/claudeSettings.ts` | Platform 类型 + serviceNames 映射 |
| `frontend/src/components/Main/Index.vue` | Tab、状态、默认模型/端点/认证、处理函数 |
| `frontend/src/components/Logs/Index.vue` | 平台筛选下拉框 |
| `frontend/src/components/EnvCheck/Index.vue` | 环境检测平台列表 |
| `frontend/src/components/common/CLIConfigEditor.vue` | 平台名称映射 |
| `frontend/src/composables/useStatsDashboard.ts` | PLATFORM_ORDER + platformStats |
| `frontend/src/services/endpointSync.ts` | SyncedEndpoint source 类型 + 加载逻辑 |
| `frontend/src/services/connectivity.ts` | 平台连通性测试 |
| `frontend/src/services/mcp.ts` | MCP 平台同步 |
| `frontend/src/data/cards.ts` | 预设供应商卡片 |
| `frontend/src/locales/zh.json` | 中文翻译 |
| `frontend/src/locales/en.json` | 英文翻译 |

### 资源文件

| 文件 | 内容 |
|------|------|
| `resources/model-pricing/model_prices_and_context_window.json` | DeepSeek v4 模型定价 |

---

## 十、实施步骤（建议顺序）

### Phase 1：后端核心（预计 1 天）

1. **新建 `reasonixsettings.go`**
   - 实现 ProxyStatus / EnableProxy / DisableProxy / ApplySingleProvider
   - 处理扁平 JSON 结构的读写
   - ProxyState 管理

2. **修改 `providerrelay.go`**
   - 注册路由
   - 新增 `ReasonixParseTokenUsageFromResponse`
   - `ReqeustLogHook` 加 case
   - `validateConfig()` 加平台

3. **修改 `providerservice.go` + `directapply_helpers.go`**
   - 文件路径映射

4. **修改 `main.go`**
   - 实例化并注册

### Phase 2：后端辅助服务（预计 0.5 天）

5. **修改 `healthcheckservice.go`** — 默认模型/端点
6. **修改 `connectivitytestservice.go`** — 默认模型
7. **修改 `logservice.go`** — 平台列表
8. **修改 `deeplinkservice.go`** — 深链支持
9. **修改 `mcpservice.go`** — MCP 同步（可后续迭代）

### Phase 3：前端集成（预计 1 天）

10. **修改 `claudeSettings.ts`** — 类型 + 服务映射
11. **修改 `Main/Index.vue`** — Tab + 状态 + 默认值 + 处理函数
12. **修改 `Logs/Index.vue`** — 筛选下拉
13. **修改 `useStatsDashboard.ts`** — 统计
14. **修改 `endpointSync.ts`** — 端点同步
15. **修改 i18n 文件** — 翻译

### Phase 4：收尾（预计 0.5 天）

16. **添加模型定价数据**
17. **预设供应商卡片**
18. **环境检测页面**
19. **重新生成 Wails bindings**：`wails3 task common:generate:bindings`
20. **集成测试**

---

## 十一、风险与注意事项

### 11.1 Token 解析准确性

DeepSeek v4 模型的 SSE 流中，`usage` 字段可能只在最后一个 chunk 出现。需确认：
- 流式响应中 `usage` 是否在每个 chunk 都有（大概率只在最终 chunk）
- 使用 `maxIntInto()` 而非 `+=` 来避免重复计数

### 11.2 配置文件并发安全

Reasonix CLI 运行时可能同时读写 `config.json`。Code Switch R 修改配置时应：
- 使用原子写入（写临时文件 → rename）
- 只修改目标字段，保留其他字段不变

### 11.3 环境变量优先级

Reasonix 的 `DEEPSEEK_BASE_URL` 环境变量优先于 config.json。如果用户设置了该环境变量，代理可能不生效。`ProxyStatus` 检测时应同时检查环境变量，并在 UI 中提示。

### 11.4 与 DeepSeekCode 的区分

两者都连接 DeepSeek API，但：
- DeepSeekCode 走 Anthropic 协议（`/v1/messages`），认证用 `x-api-key`
- Reasonix 走 OpenAI 协议（`/chat/completions`），认证用 `bearer`
- 供应商配置独立存储（`deepseekcode.json` vs `reasonix.json`）
- 统计/日志独立计算

### 11.5 模型定价需核实

DeepSeek v4 系列模型（`deepseek-v4-flash`、`deepseek-v4-pro`）的定价需在上线前从官方渠道确认。文档中的数值为估算。

---

## 十二、验收标准

- [ ] 代理启用后，Reasonix CLI 请求正确路由到配置的供应商
- [ ] 多供应商优先级降级正常工作
- [ ] Token 用量正确记录（input/output/cache_hit/reasoning）
- [ ] 统计页面正确显示 Reasonix 平台数据
- [ ] 日志页面可按 Reasonix 平台筛选
- [ ] 连通性测试正常工作
- [ ] 代理禁用后 Reasonix 配置正确恢复
- [ ] 不影响其他平台的正常运行
