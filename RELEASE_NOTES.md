# Code Switch v2.6.56

## 更新亮点

- 重构 Relay 转发与故障降级流程，完善响应已提交、客户端中断和上游断流的失败记账。
- 将数据库、基础设施和 Relay 内部实现拆分到独立包，收敛服务层依赖边界。
- 优化 Provider、黑名单、日志、定价和 Pi 配置的数据访问与缓存路径。
- 拆分首页平台状态与交互逻辑，统一前端服务调用和生成绑定。
- 清理历史调试脚本，并补充并发、迁移、故障降级和性能回归测试。

# Code Switch v2.6.55

## 更新亮点
- **移除 DeepSeekCode 平台支持**：下线 DeepSeekCode 的供应商管理页签、本地代理路由（`/deepseekcode/v1/*`）、MCP、提示词、环境检查与 WSL 写入入口。

## 修复
- 启动时自动检测旧 DeepSeekCode 托管状态：仅恢复代理注入的 `DEEPSEEK_BASE_URL`、`DEEPSEEK_API_KEY` 字段并清理残留状态文件，不改动用户 `settings.json` 中的其他内容。

---

# Code Switch v2.6.54

## 更新亮点
- **代理请求链路全面提速**：缓存应用设置与代理 Transport，减少 SSE、Chat chunk、协议转换和 Codex reasoning 续写中的重复解析、复制与内存分配，并让重试等待及时响应客户端取消。
- **日志与统计改为数据库聚合**：总量、趋势和排行下推到 SQLite，日志列表改为后端分页；10 万条合成日志的统计基准由约 1.67 秒降至 0.45 秒。
- **前端按需加载与按需轮询**：路由和模型图标改为延迟加载，页面轮询仅在当前页面与窗口可见时运行，控制台通过环形缓冲增量读取日志。

## 修复
- 成本回填改为后台分批执行，新增默认关闭的日志保留策略，避免启动和大批量清理阻塞应用。
- 为定价、Pi 内置模型与 AppSettings 增加文件缓存和失败负缓存，减少稳定配置被反复读取或请求。
- 限制 Codex Chat 历史缓存总内存，并优化 Responses continuation 和 Pi JSON 过滤路径，降低长会话的内存增长。

---

# Code Switch v2.6.53

## 更新亮点
- **新增模型定价工作台**：集中查看和更新只读 LiteLLM 内置价格，支持搜索、筛选、原始数据详情，以及按优先级生效的自定义正则与分段价格。
- **统一 Pi 与全局计费回退**：Pi 请求优先使用 Pi 自定义模型、模型覆盖和内置价格，未命中时再回退到全局自定义规则与内置价格，且不会改写 Pi 配置。
- **请求成本可追溯**：Relay 在请求完成时记录价格来源、版本与成本快照，价格规则后续变化不会改写历史日志成本；旧日志仍可按当前价格补算。

## 修复
- 内置价格更新增加模型数量、保留比例、负价格与 SHA256 校验，并通过原子快照切换避免损坏当前可用价格表。
- 完善模型定价页与新建规则弹窗的加载、空状态、筛选、字段校验、键盘焦点、暗色模式和窄窗口布局。

---

# Code Switch v2.6.52

## 更新亮点
- **发布构建收敛到 Windows**：Release Action 暂停 macOS 和 Linux 编译，仅构建并发布 Windows 安装器与便携版，缩短当前版本构建链路。
- **移除测速与可用性页面**：删除侧栏测速、可用性监控页面及后台健康检查服务，保留供应商编辑页的手动连接测试。

## 修复
- 清理可用性相关前端入口、Wails bindings 和运行时注册，避免已删除功能继续参与后台轮询或发布产物。

---

# Code Switch v2.6.51

## 更新亮点
- **统一协议层与 Codex Chat bridge**：重构协议规划与转发执行路径，Codex 供应商在选择 OpenAI Chat 上游时可走 Responses→Chat Completions bridge，支持文本、function tool call / tool result 回传闭环，以及 `previous_response_id` 历史续聊。
- **流式 bridge 完成态对齐**：Chat SSE 转换在 usage 迟到时延迟发出 `response.completed`，并正确回填 output text、tool_calls 与历史消息，避免半截响应。

## 修复
- 明确 `function_call` / `function_call_output` 到 Chat messages 的转换边界；不支持的 input 类型继续显式失败，不静默降级。
- 补齐 bridge 与协议转发相关单测，覆盖工具调用往返、历史存储和完成事件时序。

---

# Code Switch v2.6.50

## 更新亮点
- **Codex reasoning 自动续写控制台日志**：自动续写触发后会在应用控制台输出带 `trace` 的结构化日志，展示触发供应商、模型、每轮 reasoning token、截断判断、续写动作和最终停止原因。

## 修复
- 收紧 Codex reasoning 自动续写诊断日志的输出边界，只记录状态码、轮次和布尔诊断信息，不输出请求体、响应体、headers、API Key 或 encrypted reasoning 内容。

---

# Code Switch v2.6.49

## 更新亮点
- **Codex reasoning 自动续写**：Codex 供应商新增可选开关，代理在检测到原生 Responses 流式 reasoning 截断时，会基于加密 reasoning 状态继续请求同一供应商，并把多轮 SSE 折叠为一次完整响应。

## 修复
- 避免将 Codex reasoning 续写应用到 `openai_chat` 上游协议，防止把 Responses 的加密 reasoning 状态错误回放到 Chat Completions provider。
- 续写过程中遇到上游中断或 EOF 时返回明确的 `response.incomplete`，不静默伪装为成功完成。

---

# Code Switch v2.6.31

## 更新亮点
- **项目内 GitHub 图标链接修正**：首页工具栏里的 GitHub 图标现在直接打开 `https://github.com/maqibg/code-switch-R`。

## 修复
- 修复项目内 GitHub 图标仍指向旧仓库 / release 页面，而不是当前仓库主页的问题。

---

# Code Switch v2.6.30

## 更新亮点
- **日志与统计统一改为中国北京时间**：`今日`、`过去7天`、`本月`、趋势图、最近活动和日志时间显示统一按 `Asia/Shanghai` 计算与展示，不再依赖运行机器本地时区。

## 修复
- 修复 UTC 写入的 `request_log.created_at` 在凌晨被错误排除，导致首页、统计页、日志页“今日”显示 0 的问题。
- 修复日志页与统计页对无时区时间字符串按本地时间误解析，造成时间显示慢 8 小时的问题。

---

# Code Switch v2.6.29

## 更新亮点
- **MCP 工具栏按钮重新对齐**：刷新列表按钮移到第二行左侧，与导出按钮成对对齐，避免顶部工具栏在桌面端出现悬空空位。

## 修复
- 修复 MCP 页面顶部工具栏按钮自动换行后布局不整齐的问题。

---

# Code Switch v2.6.28

## 更新亮点
- **发布资产文件名统一**：GitHub Release 产物统一改为 `codeSwitchR` 前缀，并去除文件名中的版本号。
- **发版流程重新梳理**：先推送最新本地仓库，再用 tag 触发 Actions 编译发版，避免先发旧代码再补修。

## 修复
- 修复 Release 资产仍混用 `CodeSwitch` / `code-switch-R-v{version}` 命名的问题。
- 修复应用内更新与 `latest.json` 对发布资产文件名的匹配逻辑，使其适配新的无版本文件名。

---

# Code Switch v2.6.27

## 更新亮点
- **正式恢复 `code-switch-R` 命名**：应用名、进程名、单实例标识、构建脚本、GitHub Actions 产物名已统一回到 `code-switch-R`。
- **项目数据目录统一到程序目录**：所有当前项目配置与数据统一落在程序所在目录的 `.code-switch-R`，不再混用旧路径。
- **当前项目导入导出补齐前端偏好**：主题、语言、侧边栏折叠、已访问页面、忽略更新版本会随当前项目备份一起导入导出；同时兼容恢复旧 `code-switch-test` 的 WebView 本地偏好。
- **MCP 管理页继续完善**：三类 CLI 独立管理保持不变，并修复亮色模式下顶部 tab 对比度过低的问题。
- **默认外观调整为暗色模式**：新安装或无历史偏好的情况下默认启用暗色，Windows 原生标题栏同步切为暗色风格。

## 修复
- 修复当前项目导入 `bin/.code-switch-test` 后主题设置没有正确恢复的问题。
- 修复直接 `go build` 测试产物易遗漏命名与配置目录回改后的运行时细节问题。

---

# Code Switch v2.6.14

## 新功能
- **自适应热力图**：新增供应商使用热力图功能，直观展示各供应商的请求分布情况，支持按时间范围筛选
- **图标搜索**：新增供应商图标搜索功能，方便快速查找和选择合适的图标

## 修复
- 修复控制台日志递归爆炸问题

---

# Code Switch v2.0.0

## 新功能
- **自定义 CLI 工具支持（Others Tab）**：新增"托管自定义 CLI"功能，支持为任意 AI CLI 工具（如 Droid、RooCode 等）配置代理托管。用户可以自定义配置文件路径、格式（JSON/TOML/ENV）和代理注入字段，实现统一的供应商管理。
- **多配置文件编辑器**：支持为每个自定义 CLI 工具管理多个配置文件，提供实时编辑、格式校验和一键保存功能。

## 修复
- **自定义 CLI 代理路由**：修复自定义 CLI 工具的代理路由 `/custom/:toolId/v1/messages`，确保请求正确转发到供应商。
- **代理注入 URL 格式**：修复代理注入时 URL 路径拼接问题，确保生成正确的 `http://127.0.0.1:18100/custom/{toolId}` 格式。
- **Windows 路径扩展**：修复 Windows 系统下 `~\` 路径前缀的展开问题，正确识别用户主目录。
- **前端 toast 提示**：修复创建/更新 CLI 工具后的成功提示显示。
- **Claude 代理开关状态检测**：修复刷新后 Claude 代理开关显示为关闭的问题。根本原因是 Claude CLI 可能覆盖 `ANTHROPIC_AUTH_TOKEN`，现改为仅检查 `ANTHROPIC_BASE_URL` 是否指向本地代理。

## 技术改进
- 新增 `CustomCliService` 服务，提供完整的 CRUD 和代理状态管理
- 前端新增 `CustomCliConfigEditor` 组件，支持多文件编辑和格式校验
- 中英文国际化支持完善

---

# Code Switch v1.5.4

## 更新亮点
- 🔧 **彻底修复 Claude 代理开关状态问题**：根本原因是后端 `ProxyStatus` 使用 `map[string]string` 解析 `env`，当配置文件中存在非字符串值时解析失败，导致状态始终返回 `false`。现改用 `map[string]any` 宽容解析。
- 🔄 **CI 自动同步版本号**：构建时自动从 Git Tag 提取版本号，更新到所有平台配置文件。
- 📝 **Release 自动提取更新说明**：从 `RELEASE_NOTES.md` 自动提取当前版本的更新内容。

# Code Switch v1.5.3

## 更新亮点
- 🔧 **代理开关状态持久化修复**：修复了刷新页面后代理开关显示为关闭状态的问题，实际上代理仍在运行。问题根源是 Wails RPC 返回的字段名与前端读取的字段名不一致。
- ✏️ **CLI 配置预览可编辑**：配置文件预览区域新增解锁编辑功能，用户可以直接修改配置内容，支持 JSON/TOML/ENV 格式自动解析和校验。
- 🎨 **Tab 按钮左对齐**：Claude Code / Codex / Gemini 三个选择按钮现在与供应商卡片左边缘对齐，视觉更统一。
- 📖 **README 重写**：完全重写 README 文档，面向小白用户，3 步快速开始，包含常见问题解答。
