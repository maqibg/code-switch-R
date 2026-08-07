# code-switch-R 协作指南

本文件是当前仓库的共享协作说明，适用于 Codex、Claude Code 以及其他能读取 `AGENTS.md` 的代码代理。若本文件与系统或开发者消息冲突，优先遵循更高优先级指令；若与 `CLAUDE.md`、README 或历史文档冲突，先读取最新代码确认事实，再按当前实现处理。

## 项目定位

`code-switch-R` 是一个仅发布 Windows amd64 便携版的 Wails 3 桌面应用，用 Go 后端、Vue 3 前端和本地 HTTP Relay 管理 Claude Code、Codex、Gemini CLI、Reasonix、Pi 与 Grok Build 的供应商配置。Relay 使用随机 Token，仅允许 localhost 或当前 WSL 宿主机地址，按供应商优先级、模型白名单和黑名单状态执行降级转发，并记录请求、Token 和成本数据。

当前主要技术栈：

| 层级 | 当前实现 |
|---|---|
| 桌面框架 | Wails 3，Go module 为 `codeswitch` |
| 后端 | Go 1.24、Gin、`modernc.org/sqlite`、`xdb`、`xrequest` |
| 前端 | Vue 3、TypeScript、Vite、Tailwind CSS 4、Vue Router、Vue I18n |
| 持久化 | 可执行文件同级 `.code-switch-R` 目录中的 JSON 与 SQLite |
| 打包 | Wails task，仅构建 Windows amd64 便携 exe |

## 目录与职责

- `main.go`：Wails 入口、服务注册、窗口和系统托盘、启动/关闭顺序、`frontend/dist` 嵌入。
- `version_service.go`：应用内版本常量 `AppVersion` 和更新策略查询。做发布或自动更新相关改动时必须核对它是否与 tag、Release Notes 和构建资产一致。
- `services/`：后端核心。新增前端可调用能力时，在这里实现并通过 `application.NewService` 注册。
- `frontend/src/`：Vue 应用。页面按功能放在 `components/`，服务封装在 `services/`，路由在 `router/index.ts`。
- `frontend/bindings/`：Wails 自动生成的 TypeScript 绑定，禁止手改。Go 导出方法签名变化后运行 `wails3 task common:generate:bindings`。
- `resources/model-pricing/`：模型价格、上下文窗口、成本计算和对应测试。
- `build/`：Wails 通用配置与 Windows 便携构建 Taskfile。
- `.github/workflows/release.yml`：tag 触发的 Windows amd64 便携版 Release 构建与资产发布。
- `doc/`、根目录历史方案文档：只作参考，不代表当前真实实现；结论必须用代码复核。
- `scripts/`：调试和发布辅助脚本，很多脚本面向特定历史问题，执行前先读内容和影响范围。

## 启动链路

`main.go` 的启动顺序不能随意调整：

1. `services.InitDatabase()`：创建可执行文件同级 `.code-switch-R`，打开 `app.db?cache=shared&mode=rwc`，设置 `PRAGMA busy_timeout = 30000` 和 WAL。
2. 清理已经移除的 DeepSeekCode 与自定义 CLI 托管数据，并加载随机 Relay Token 和网络设置。
3. 构造 Provider、Settings、AppSettings、Blacklist、Gemini、ProviderRelay、CLI 配置、日志、MCP、Prompt、Import、DeepLink、Connectivity、Update、Network、Grok、Pi 与 FrontendPreferences 等服务。
4. 升级已托管 CLI 的旧 Relay 凭据，同步启动 `ProviderRelayService`；监听失败时应用终止启动。
5. 后台启动黑名单过期恢复定时器。
6. 创建 Wails 应用、主窗口、托盘菜单，注册服务并运行。

关闭时必须停止黑名单定时器、Grok 服务和 Relay。

## 持久化位置

当前代码的项目数据目录是可执行文件目录下的 `.code-switch-R`，由 `internal/infra/userhome.go` 的 `GetAppConfigDir()` 决定。不要沿用旧文档中的 `~/.code-switch` 或 `%USERPROFILE%\.code-switch` 作为当前数据目录，除非代码里明确在做旧数据导入或迁移。

重要文件和数据：

| 文件或目录 | 用途 |
|---|---|
| `.code-switch-R/app.db` | SQLite 主库：`provider`（供应商主数据，含 Gemini）、`request_log`、`relay_attempt`、`provider_blacklist`、`app_settings`、`schema_version` |
| `.code-switch-R/suidemo.db` | 快捷键数据库，旧路径来自用户配置目录下的 `SuiNest` |
| `.code-switch-R/claude-code.json` | Claude Code provider 配置 |
| `.code-switch-R/codex.json` | Codex provider 配置 |
| `.code-switch-R/reasonix.json` | Reasonix provider 配置 |
| `.code-switch-R/pi.json` | Pi 应用侧供应商、`piTemplate` 模板标识、完整模型定义、模型覆盖、请求头和 `metadataUserId` 配置 |
| `.code-switch-R/pi-provider-templates.json` | Pi 网关协议模板；模板只提供协议与模型元数据，不包含供应商 URL；文件不存在时加载内置 Anthropic、OpenAI Chat 与 OpenAI Codex / Responses 模板 |
| `.code-switch-R/provider-request-templates.json` | 用户保存的 Provider 请求头与 `metadataUserId` 模板；内置模板不写入该文件 |
| `.code-switch-R/app.json` | 应用设置、预算周期、自动更新、轮询、通知、全局代理等 |
| `.code-switch-R/network.json` | localhost/WSL 监听设置与内部 Relay Token；Token 不向前端暴露 |
| `.code-switch-R/mcp-{platform}.json` 与旧 `mcp.json` | MCP 配置，按平台拆分后仍兼容旧共享文件 |
| `.code-switch-R/mcp-managed-{platform}.json` | 应用实际写入外部 CLI 的 MCP 条目指纹 |
| `.code-switch-R/prompts.json` | 自定义提示词索引和状态 |
| `.code-switch-R/frontend-preferences.json` | 主题、语言、侧边栏、已忽略更新等前端偏好 |
| `.code-switch-R/skill.json` | 已移除 Skill 功能留下的用户数据，只保留，不再读取或修改 |
| `.code-switch-R/grok-runtime.json`、`grok-oauth-accounts.json` | Grok 运行模式、字段 ownership 和 OAuth 账号 |
| `.code-switch-R/proxy-state/{platform}.json` | CLI 代理启用前后的备份状态 |
| `.code-switch-R/download_state.json`、`pending_apply.json`、`dismissed_version.txt` | 自动更新状态 |

## 核心后端模块

- `ProviderRelayService`：代理核心，在 `internal/relay/providerrelay.go`。负责 Token 验证、路由注册、Provider 选择、Level 分组、轮询、失败降级、黑名单集成、请求转发、日志写入和模型列表代理。
- `ProviderService`：Provider JSON 文件读写、迁移、验证、复制、删除清理和改名，并维护 Pi 协议模板 CRUD。单独改名走 `RenameProvider()`；编辑页需要同时保存字段和改名时走 `SaveProvidersWithRename()`，由后端统一回滚配置文件、数据库和 Pi 网关。Pi 模板 ID 创建后不可修改，仍被供应商引用的模板不得删除；同一协议模板下标准化后的相同 API URL 只能对应一个供应商。
- `PiSettingsService`：只自动检测当前用户的 `~/.pi/agent`。目录存在但 `models.json` 缺失或没有 Provider 时写入脱敏默认平台；已有 Provider 原样解析为 Pi 平台。每个平台可独立托管：开启时备份该 Provider 及同名 `auth.json` 条目，导入原始 URL 和有效 API Key 为平台首个网关供应商，再把该平台改到 `/pi/providers/{provider}`；关闭时只恢复该平台。`ModelsCatalog()` 返回脱敏目录、托管状态与外部修改冲突，不得暴露 API Key 或请求头。
- `provider_delete.go`：删除 provider 时清理 `request_log`、`relay_attempt`、`provider_blacklist`。主数据已入 SQLite，保存失败没有 JSON 补偿窗口；仅 Pi 网关同步失败时才回滚 provider 表，避免配置和数据库状态分裂。
- `provider_rename.go`：`RenameProvider()` 在单个 IMMEDIATE 事务内保存并改名，同时更新历史表 name 列（仅展示）。日志、黑名单、请求都按 `provider_id` 关联，改名瞬间的 in-flight 写入靠 ID 落到同一行，因此不再写 `provider_alias`、不再禁止链式改名。
- `Protocol Adapter`：`internal/relay/protocol_matrix_adapter.go`、`protocol_matrix_stream.go` 与 `protocol_matrix_gemini.go` 负责 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 和 Gemini Native 的双向矩阵转换（协议枚举在 `services/protocol` 子包）。无法保持 reasoning、工具选择或流式事件语义时必须显式失败；流式响应一旦提交就不得再切换 Provider 或追加 JSON 错误。
- `BlacklistService`：管理请求失败拉黑和过期自动恢复。
- `LogService` 与 `resources/model-pricing`：读取请求日志、聚合统计、热力图、成本估算、模型定价匹配。
- `MCPService`：管理 Claude、Codex、Gemini、Reasonix 的 MCP 配置；只同步应用拥有的条目，外部修改冲突时拒绝覆盖。
- `PromptService`：管理各平台提示词，写入平台原始提示词文件前会检测外部修改。
- `NetworkService`：只管理 localhost 与 WSL 监听，拒绝 `0.0.0.0` 和任意自定义地址，能写入 WSL 内的 Claude/Codex/Gemini 配置。
- `UpdateService`：优先读取 GitHub Release 的 `latest.json`，再回退 GitHub API；下载 URL 有白名单校验，只支持 Windows 便携 exe 确认更新。

## 代理路由与协议

`ProviderRelayService.registerRoutes()` 当前注册的路由如下：

```text
POST /v1/messages                  -> Claude Code Anthropic Messages
POST /responses                    -> Codex Responses
GET  /v1/models                    -> Claude/Codex compatible models
POST /gemini/v1beta/*any           -> Gemini v1beta
POST /gemini/v1/*any               -> Gemini v1
POST /reasonix/chat/completions    -> Reasonix OpenAI Chat Completions
GET  /reasonix/models              -> Reasonix models
POST /pi/providers/:provider/*any  -> Pi 平台级协议网关
POST /grok/v1/responses            -> Grok Build Responses
GET  /grok/v1/models               -> Grok Build models
```

Provider 选择顺序是：启用状态、URL/API Key、配置校验、模型支持、黑名单状态、Level 分组、同 Level 轮询设置。黑名单固定模式开启时，同一 provider 会按阈值重试直到拉黑再切换；未开启时每个 provider 单次失败后降级到下一个。

认证默认值由 `defaultConnectivityAuthType(platform)` 决定，当前平台默认使用 `bearer`。显式填写 `Provider.AuthScheme` 或兼容字段 `Provider.ConnectivityAuthType` 时以配置为准，协议类型只控制 `anthropic-version` 等协议专属头，不得把显式 `x-api-key` 改写成 Bearer。转发前会删除客户端传来的认证头，避免占位 Key 污染上游请求。

Pi 的一个 `models.json.providers.<id>` 就是页面中的一个平台；应用侧 `pi.json` 可在该平台下保存多个网关供应商，但这些供应商不得作为额外 Pi Provider 写回 `models.json`。托管只改平台级 `baseUrl`、`apiKey` 和同名 `auth.json` 条目，保留 `api`、`headers`、`compat`、`models`、`modelOverrides` 及未知字段；模型级 `baseUrl` 会绕过平台网关，因此有此配置的平台不得开启托管。关闭托管前必须校验注入内容哈希，外部修改时显式报告冲突，禁止静默覆盖。导入的 Pi 配置值按 Pi 语义解析 `$ENV`、`${ENV}`、`!command`、`$$` 和 `$!`，`auth.json` 的 `api_key` 凭证优先于 `models.json.apiKey`，暂不支持无损导入 OAuth 条目。

Provider 自定义 Header 在兼容预设之后应用，可以覆盖 User-Agent 等默认值，但认证头和危险 transport Header 仍由代理控制。`metadataUserId` 只允许最终上游协议为 Anthropic Messages，并在协议转换完成后写入请求体 `metadata.user_id`；不得把设备、账号或会话标识硬编码进内置模板。

统计成功统一定义为 HTTP 2xx 且 `error_type` 为空；失败数必须是该条件的补集。所有非 2xx 响应都按失败处理；固定黑名单模式下同一 Provider 重试到拉黑后再切换。

## 前端结构

前端使用 hash 路由，主要页面：

| 路由 | 页面 |
|---|---|
| `/` | Provider 管理主界面，包含 Claude、Codex、Gemini、Reasonix、Grok Build 与 Grok OAuth Tab |
| `/pi` | 实时解析 `models.json` 平台和模型，管理平台 CRUD、平台级独立托管、平台下网关供应商、模型抓取/映射及完整 JSON 预览 |
| `/stats` | 用量统计、趋势和请求排行 |
| `/logs` | 请求日志、筛选、成本展示 |
| `/mcp` | MCP Server 管理 |
| `/env` | 环境变量冲突检查 |
| `/prompts` | 提示词管理 |
| `/console` | 应用内控制台日志 |
| `/settings` | 通用设置、黑名单、预算、代理、导入导出 |

前端调用后端有两种方式：优先使用 `frontend/bindings/` 生成的函数；部分动态服务用 `@wailsio/runtime` 的 `Call.ByName`。新增或改名 Go 导出方法后，必须同步更新 bindings 和对应 `frontend/src/services/*.ts` 或组件调用。

## 构建、运行与测试

开发时按改动范围选择最短反馈链路，不把桌面集成、完整构建和发布门禁套到每次修改：

- 只修改前端时使用 Vite HMR 和定向 Vitest；不启动 Wails，不构建便携 exe。
- 只修改 Go 内部实现时先运行所属 package 的定向测试；不重建前端，不生成 bindings。
- 只有 Go 导出服务方法、参数、返回值或前端可见模型变化时才重新生成 bindings，并在生成后构建前端。
- 只有 `build/config.yml` 的应用信息或文件关联变化时才运行 `wails3 task common:update:build-assets`；只有图标源文件变化时才运行图标生成任务。
- 只有需要验证窗口、托盘、WebView、进程生命周期、Relay 监听或真实前后端调用时才运行桌面开发实例。
- `go mod tidy` 只在 Go 依赖或 `go.mod` / `go.sum` 确实变化时运行；不得把它当作每次修改后的通用检查。
- `npm install` 只用于首次安装或依赖声明变化；已有依赖且 lockfile 未变时不要重复安装。CI、发版和干净环境使用 `npm ci`。
- `wails3 task build` 与 `wails3 task package` 属于集成/发布构建，不是普通源码修改后的默认验证。
- 前端构建和嵌入 `frontend/dist` 的 Go 构建必须串行，禁止并行读写构建产物。

常用命令按用途选择：

```powershell
# 终端 A：前端开发（持续运行）
Set-Location frontend
npm run dev
```

```powershell
# 终端 B：前端定向测试
Set-Location frontend
npm test -- --run <test-file>
Set-Location ..

# Go 定向测试
go test ./services -run '<TestNameRegex>' -timeout 60s
```

```powershell
# Go 导出契约变化后
wails3 task common:generate:bindings
Set-Location frontend
npm run build
Set-Location ..

# 真实桌面联调、Windows 集成构建、发布构建
wails3 task dev
wails3 task build
wails3 task package
```

同一次验证只运行满足当前风险的最小命令；后续更高层验证已覆盖前一步时，不机械重复等价检查。命令失败时先处理当前失败，不通过追加更大范围命令掩盖根因。

### Windows 桌面开发实例

需要与本机已安装的正式版并行测试时，开发实例固定使用 `code-switch` 进程名、独立单实例 ID 和 `18101` Relay 端口。先完成前端构建和 Windows amd64 Go 构建，将程序输出为 `bin\dev\code-switch.exe`，再设置以下环境变量启动：

```powershell
$ErrorActionPreference = 'Stop'
$env:CODE_SWITCH_APP_NAME = 'code-switch'
$env:CODE_SWITCH_INSTANCE_ID = 'com.rogers-f.code-switch-r.dev'
$env:CODE_SWITCH_RELAY_ADDR = '127.0.0.1:18101'
Start-Process -FilePath 'D:\Code\codeSpace\code-switch-R\bin\dev\code-switch.exe' `
    -WorkingDirectory 'D:\Code\codeSpace\code-switch-R\bin\dev'
```

代码代理启动可见桌面窗口时必须申请沙盒外执行，并为该调用显式指定 `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`；这是当前 Windows 用户会话中已验证的 GUI 启动路径。普通构建、测试和文本处理仍优先使用 PowerShell 7。启动命令返回后不能只看退出码，必须同时确认进程路径和 Relay 监听：

```powershell
Get-Process -Name 'code-switch' | Select-Object Id, ProcessName, Path, StartTime
Get-NetTCPConnection -State Listen -LocalPort 18101 | Select-Object LocalAddress, LocalPort, OwningProcess
```

两条结果中的 PID 必须一致。重启时只停止路径为本仓库 `bin\dev\code-switch.exe` 的进程，不得终止用户安装的正式版或其他同名进程。

### 桌面联调与发布前构建

上面的桌面实例只在需要真实窗口或 Relay 验证时使用。Windows 单文件构建必须先生成 `frontend/dist`，再 Go build；`main.go` 会嵌入该目录，构建步骤必须串行。当前环境必须显式设置 `GOOS=windows`、`GOARCH=amd64` 和 `CGO_ENABLED=0`，不要依赖默认架构。

```powershell
Set-Location frontend
npm ci
npm run build
Set-Location ..
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o bin/code-switch-R-test.exe
```

发版前或跨模块改动需要完整信心时，按以下顺序执行一次即可：

```powershell
Set-Location frontend
npm ci
npm test -- --run
npm run build
Set-Location ..
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go test ./... -timeout 300s
wails3 task package
```

完整 `go test ./...` 不是日常编辑后的默认命令；它包含数据库、Relay、迁移和跨服务测试，可能明显慢于定向测试。发版前应在接近 CI 的干净环境中执行，并确认 `frontend/dist`、bindings 和便携 exe 都由当前提交生成。

## 发布流程

GitHub Release 由 `.github/workflows/release.yml` 在推送 `v*` tag 时触发。Workflow 只构建 Windows amd64 便携版，生成并发布 `codeSwitchR.exe`、`codeSwitchR.exe.sha256` 与 `latest.json`。

发版前至少核对：

- `version_service.go` 的 `AppVersion`。
- `RELEASE_NOTES.md` 或 Release body 需要展示的版本说明。
- `build/config.yml`、`build/windows/info.json` 与 `version_service.go` 版本是否一致。
- `frontend/bindings/` 是否与 Go 导出服务签名一致。
- 先运行前端测试和生产构建，再运行 `go test ./... -timeout 300s`；最后按顺序构建 Windows amd64 便携 exe。
- 在接近 CI 的干净环境验证一次，不能因为本机残留的 `frontend/dist`、bindings 或 Go/npm 缓存而跳过真实步骤。

需要用 `gh` 查询 Actions 或 Release 时，先检查当前进程是否有 `GH_TOKEN`，不得打印 token。若未继承但 Windows User/Machine 环境变量存在，可在单条 PowerShell 命令中临时注入再执行 `gh auth status` 或 `gh api user --jq .login`。

## 修改规则

- 修改前先查当前实现，不只看 README、历史方案文档或 `CLAUDE.md`。
- 只改当前任务需要的文件，不顺手重构、重排格式或更新无关文档。
- Go 文件修改后运行 `gofmt`；Vue/TypeScript 保持现有组合式 API 和服务封装风格。
- 数据库写入没有队列：WAL + DSN 级 busy_timeout + 同步短事务（见 `internal/dbcore/dbwrite.go`）。读改写用 `BEGIN IMMEDIATE` 事务，相关一组写入用 `ExecStatements` 原子提交；不要重新引入队列或后台批量缓冲。
- SQLite 查询和命令必须参数化，禁止字符串拼接用户输入。只有受控表名等内部白名单场景才可用显式校验后的拼接。
- JSON、TOML、ENV 和用户 CLI 配置写入必须走现有原子写入或专用服务方法，避免破坏用户配置。
- 代理配置启用/停用必须保留用户原有配置和备份状态，禁止用模板整体覆盖用户文件。
- Provider 单独改名走 `RenameProvider()`；同一次编辑同时改名和保存其他字段时走 `SaveProvidersWithRename()`。删除 provider 必须保留删除清理与回滚语义。
- 失败应暴露为错误、日志或测试失败，不添加 mock 成功、静默 fallback 或吞异常。
- 涉及 API Key、auth.json、settings.json、环境变量和 Release token 时，不输出密钥原文，不写入源码。

## 验证策略

根据改动范围选择最小有效验证：

| 改动范围 | 建议验证 |
|---|---|
| Provider 模型、映射、认证、Level、删除、改名 | `go test ./services -run "TestProvider|TestModels|TestRename|TestSaveProviders|TestDefaultConnectivityAuthType" -timeout 60s` |
| Pi 模型、网关、请求头模板、metadata 与预览 | `go test ./services -run "TestPi|TestBuildPiGateway|TestValidatePiProviderMetadata|TestRequestHeaderTemplate|TestApplyProviderRequestBodyPolicy" -timeout 120s` |
| Relay 转发、协议转换、日志解析 | `go test ./internal/relay ./services/protocol -timeout 120s` |
| Gemini provider 或 `.env` 解析 | `go test ./services -run TestGemini -timeout 60s` |
| 日志、统计、时区、成本 | `go test ./services -run "Test.*Range|Test.*Stats|Test.*Timezone" -timeout 60s` 和 `go test ./resources/model-pricing -timeout 60s` |
| Go 导出服务签名 | `wails3 task common:generate:bindings`，再 `npm run build` |
| 前端页面或服务封装 | 先运行对应 Vitest；需要类型或产物确认时，在 `frontend` 目录运行 `npm run build` |
| 配置迁移、备份、MCP、网络与安全边界 | `go test ./services -run 'Test(Migration|Config|Backup|MCP|Network|Relay|Removed)' -timeout 120s` |
| 构建/发布/资源嵌入 | 先 `npm test -- --run` 和前端 build，再按顺序运行 `go test ./... -timeout 300s` 与 `wails3 task package` |

无法运行完整验证时，说明具体失败命令、失败原因和剩余风险，不把未执行的测试写成已通过。

## Windows 与编码

在 Windows 上优先使用 PowerShell 7。先用 `Get-Command pwsh` 查找路径，不要硬编码 WindowsApps 版本目录。命令脚本首部使用 `$ErrorActionPreference = 'Stop'`，读写文本显式指定 `-Encoding UTF8`，避免 `Get-Content`、`Set-Content`、`Out-File` 走默认编码。

执行 shell 命令时注意 PowerShell 与 Bash 语法不同：`rg` 正则包含 `|`、`(`、`)`、`\` 等字符时用单引号包住；复杂脚本拆成单个清晰命令。不要用 `cmd.exe`、`>` 或 `>>` 写文件。

## Git 边界

仓库可能有用户本地改动。开始修改前查看 `git status --short`，提交前查看 `git diff` 和 `git diff --cached --name-only`。未经明确要求不主动 commit、push、tag 或创建 Release。需要提交时只暂存本任务文件，不夹带生成物、历史文档清理或无关格式化。

当前 `AGENTS.md` 是已跟踪文件，不是被 `.gitignore` 忽略的临时说明文件；修改后应通过 `git diff -- AGENTS.md` 复核实际差异。
