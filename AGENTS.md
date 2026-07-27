# code-switch-R 协作指南

本文件是当前仓库的共享协作说明，适用于 Codex、Claude Code 以及其他能读取 `AGENTS.md` 的代码代理。若本文件与系统或开发者消息冲突，优先遵循更高优先级指令；若与 `CLAUDE.md`、README 或历史文档冲突，先读取最新代码确认事实，再按当前实现处理。

## 项目定位

`code-switch-R` 是一个 Wails 3 桌面应用，用 Go 后端、Vue 3 前端和本地 HTTP 代理管理 AI CLI 工具的供应商配置。应用启动后在本机 `127.0.0.1:18100` 代理 Claude Code、Codex、Gemini CLI、Reasonix 和自定义 CLI 请求，按供应商优先级、模型白名单和黑名单状态执行降级转发，并记录请求、Token 和成本数据。

当前主要技术栈：

| 层级 | 当前实现 |
|---|---|
| 桌面框架 | Wails 3，Go module 为 `codeswitch` |
| 后端 | Go 1.24、Gin、`modernc.org/sqlite`、`xdb`、`xrequest` |
| 前端 | Vue 3、TypeScript、Vite、Tailwind CSS 4、Vue Router、Vue I18n |
| 持久化 | 可执行文件同级 `.code-switch-R` 目录中的 JSON 与 SQLite |
| 打包 | Wails task、Windows NSIS、macOS `.app`/zip、Linux AppImage/DEB/RPM |

## 目录与职责

- `main.go`：Wails 入口、服务注册、窗口和系统托盘、启动/关闭顺序、`frontend/dist` 嵌入。
- `version_service.go`：应用内版本常量 `AppVersion` 和更新策略查询。做发布或自动更新相关改动时必须核对它是否与 tag、Release Notes 和构建资产一致。
- `services/`：后端核心。新增前端可调用能力时，在这里实现并通过 `application.NewService` 注册。
- `frontend/src/`：Vue 应用。页面按功能放在 `components/`，服务封装在 `services/`，路由在 `router/index.ts`。
- `frontend/bindings/`：Wails 自动生成的 TypeScript 绑定，禁止手改。Go 导出方法签名变化后运行 `wails3 task common:generate:bindings`。
- `resources/model-pricing/`：模型价格、上下文窗口、成本计算和对应测试。
- `build/`：Wails 构建配置、平台 Taskfile、Windows NSIS、Linux nFPM、macOS plist。
- `.github/workflows/release.yml`：tag 触发的跨平台 Release 构建与资产发布。
- `doc/`、根目录历史方案文档：只作参考，不代表当前真实实现；结论必须用代码复核。
- `scripts/`：调试和发布辅助脚本，很多脚本面向特定历史问题，执行前先读内容和影响范围。

## 启动链路

`main.go` 的启动顺序不能随意调整：

1. `services.InitDatabase()`：创建可执行文件同级 `.code-switch-R`，打开 `app.db?cache=shared&mode=rwc`，设置 `PRAGMA busy_timeout = 30000` 和 WAL。
2. `services.InitGlobalDBQueue()`：初始化双队列数据库写入。
3. 构造所有服务：Provider、Settings、AppSettings、Blacklist、Gemini、ProviderRelay、CLI 配置、日志、MCP、Skill、Prompt、Import、DeepLink、Connectivity、Update、Network、FrontendPreferences 等。
4. 后台启动 `ProviderRelayService.Start()`，监听代理端口。
5. 后台启动黑名单过期恢复定时器。
6. 创建 Wails 应用、主窗口、托盘菜单，注册服务并运行。

关闭时必须停止黑名单定时器和代理服务器，并用 10 秒超时关闭全局 DB 写入队列。

## 持久化位置

当前代码的项目数据目录是可执行文件目录下的 `.code-switch-R`，由 `services/userhome.go` 的 `getAppConfigDir()` 决定。不要沿用旧文档中的 `~/.code-switch` 或 `%USERPROFILE%\.code-switch` 作为当前数据目录，除非代码里明确在做旧数据导入或迁移。

重要文件和数据：

| 文件或目录 | 用途 |
|---|---|
| `.code-switch-R/app.db` | `request_log`、`provider_blacklist`、`provider_alias`、`health_check_history`、`app_settings` 等 SQLite 表 |
| `.code-switch-R/suidemo.db` | 快捷键数据库，旧路径来自用户配置目录下的 `SuiNest` |
| `.code-switch-R/claude-code.json` | Claude Code provider 配置 |
| `.code-switch-R/codex.json` | Codex provider 配置 |
| `.code-switch-R/reasonix.json` | Reasonix provider 配置 |
| `.code-switch-R/pi.json` | Pi 应用侧供应商、`piTemplate` 模板标识、完整模型定义、模型覆盖、请求头和 `metadataUserId` 配置 |
| `.code-switch-R/pi-provider-templates.json` | Pi 网关协议模板；模板只提供协议与模型元数据，不包含供应商 URL；文件不存在时加载内置 Anthropic、OpenAI Chat 与 OpenAI Codex / Responses 模板 |
| `.code-switch-R/provider-request-templates.json` | 用户保存的 Provider 请求头与 `metadataUserId` 模板；内置模板不写入该文件 |
| `.code-switch-R/providers/{toolId}.json` | 自定义 CLI 工具 provider 配置 |
| `.code-switch-R/app.json` | 应用设置、预算周期、自动更新、轮询、通知、全局代理等 |
| `.code-switch-R/network.json` | 监听模式、WSL/LAN/custom 地址配置 |
| `.code-switch-R/mcp-{platform}.json` 与旧 `mcp.json` | MCP 配置，按平台拆分后仍兼容旧共享文件 |
| `.code-switch-R/prompts.json` | 自定义提示词索引和状态 |
| `.code-switch-R/frontend-preferences.json` | 主题、语言、侧边栏、已忽略更新等前端偏好 |
| `.code-switch-R/skill.json` | Skill 仓库、安装和启用状态 |
| `.code-switch-R/custom-cli.json` | 自定义 CLI 工具定义 |
| `.code-switch-R/proxy-state/{platform}.json` | CLI 代理启用前后的备份状态 |
| `.code-switch-R/download_state.json`、`pending_apply.json`、`dismissed_version.txt` | 自动更新状态 |

## 核心后端模块

- `ProviderRelayService`：代理核心，在 `services/providerrelay.go`。负责路由注册、Provider 选择、Level 分组、轮询、失败降级、黑名单集成、请求转发、日志写入、模型列表代理、Gemini 和自定义 CLI 特殊路径。
- `ProviderService`：Provider JSON 文件读写、迁移、验证、复制、删除清理和改名，并维护 Pi 协议模板 CRUD。单独改名走 `RenameProvider()`；编辑页需要同时保存字段和改名时走 `SaveProvidersWithRename()`，由后端统一回滚配置文件、数据库和 Pi 网关。Pi 模板 ID 创建后不可修改，仍被供应商引用的模板不得删除；同一协议模板下标准化后的相同 API URL 只能对应一个供应商。
- `PiSettingsService`：只自动检测当前用户的 `~/.pi/agent`。目录存在但 `models.json` 缺失或没有 Provider 时写入脱敏默认平台；已有 Provider 原样解析为 Pi 平台。每个平台可独立托管：开启时备份该 Provider 及同名 `auth.json` 条目，导入原始 URL 和有效 API Key 为平台首个网关供应商，再把该平台改到 `/pi/providers/{provider}`；关闭时只恢复该平台。`ModelsCatalog()` 返回脱敏目录、托管状态与外部修改冲突，不得暴露 API Key 或请求头。
- `provider_delete.go`：删除 provider 时清理 `request_log`、`provider_blacklist`、`health_check_history`、`provider_alias`。清理失败会回滚 provider JSON，避免配置和数据库状态分裂。
- `provider_rename.go`：改名时事务更新历史表，并写入 48 小时 `provider_alias`，用于承接仍使用旧名的 in-flight 请求。禁止 48 小时内链式改名。
- `Protocol Adapter`：`services/protocol_matrix_adapter.go` 与 `services/protocol_matrix_stream.go` 负责 Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses 的双向矩阵转换。无法保持 reasoning、工具选择或流式事件语义时必须显式失败；流式响应一旦提交就不得再切换 Provider 或追加 JSON 错误。
- `BlacklistService`：管理请求失败拉黑和过期自动恢复。
- `LogService` 与 `resources/model-pricing`：读取请求日志、聚合统计、热力图、成本估算、模型定价匹配。
- `MCPService`：管理 Claude、Codex、Gemini、Reasonix 的 MCP 配置，并同步到对应平台文件。
- `SkillService`：扫描、安装、卸载、启停用户级和项目级 Skills，支持 GitHub 仓库来源。
- `PromptService`：管理各平台提示词，写入平台原始提示词文件前会检测外部修改。
- `NetworkService`：管理本机、WSL、LAN、自定义监听地址，能写入 WSL 内的 Claude/Codex/Gemini 配置。
- `UpdateService`：优先读取 GitHub Release 的 `latest.json`，再回退 GitHub API；下载 URL 有白名单校验，支持 Windows 静默更新流程。

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
POST /custom/:toolId/v1/messages   -> 自定义 CLI Messages
GET  /custom/:toolId/v1/models     -> 自定义 CLI models
```

Provider 选择顺序是：启用状态、URL/API Key、配置校验、模型支持、黑名单状态、Level 分组、同 Level 轮询设置。黑名单固定模式开启时，同一 provider 会按阈值重试直到拉黑再切换；未开启时每个 provider 单次失败后降级到下一个。

认证默认值由 `defaultConnectivityAuthType(platform)` 决定，当前平台默认使用 `bearer`。显式填写 `Provider.AuthScheme` 或兼容字段 `Provider.ConnectivityAuthType` 时以配置为准，协议类型只控制 `anthropic-version` 等协议专属头，不得把显式 `x-api-key` 改写成 Bearer。转发前会删除客户端传来的认证头，避免占位 Key 污染上游请求。

Pi 的一个 `models.json.providers.<id>` 就是页面中的一个平台；应用侧 `pi.json` 可在该平台下保存多个网关供应商，但这些供应商不得作为额外 Pi Provider 写回 `models.json`。托管只改平台级 `baseUrl`、`apiKey` 和同名 `auth.json` 条目，保留 `api`、`headers`、`compat`、`models`、`modelOverrides` 及未知字段；模型级 `baseUrl` 会绕过平台网关，因此有此配置的平台不得开启托管。关闭托管前必须校验注入内容哈希，外部修改时显式报告冲突，禁止静默覆盖。导入的 Pi 配置值按 Pi 语义解析 `$ENV`、`${ENV}`、`!command`、`$$` 和 `$!`，`auth.json` 的 `api_key` 凭证优先于 `models.json.apiKey`，暂不支持无损导入 OAuth 条目。

Provider 自定义 Header 在兼容预设之后应用，可以覆盖 User-Agent 等默认值，但认证头和危险 transport Header 仍由代理控制。`metadataUserId` 只允许最终上游协议为 Anthropic Messages，并在协议转换完成后写入请求体 `metadata.user_id`；不得把设备、账号或会话标识硬编码进内置模板。

统计成功统一定义为 HTTP 2xx 且 `error_type` 为空；失败数必须是该条件的补集。自定义 CLI 的新日志使用 `platform=custom` 与 `source_id=<toolId>`，查询时必须按当前工具隔离，并兼容旧的 `platform=custom:<toolId>` 记录。

## 前端结构

前端使用 hash 路由，主要页面：

| 路由 | 页面 |
|---|---|
| `/` | Provider 管理主界面，Claude/Codex/Gemini/Reasonix/自定义 CLI |
| `/pi` | 实时解析 `models.json` 平台和模型，管理平台 CRUD、平台级独立托管、平台下网关供应商、模型抓取/映射及完整 JSON 预览 |
| `/stats` | 用量统计、趋势和请求排行 |
| `/logs` | 请求日志、筛选、成本展示 |
| `/mcp` | MCP Server 管理 |
| `/skill` | Skill 市场和已安装 Skills |
| `/env` | 环境变量冲突检查 |
| `/prompts` | 提示词管理 |
| `/console` | 应用内控制台日志 |
| `/settings` | 通用设置、黑名单、预算、代理、导入导出 |
| `/tray` | macOS 托盘窗口 |

前端调用后端有两种方式：优先使用 `frontend/bindings/` 生成的函数；部分动态服务用 `@wailsio/runtime` 的 `Call.ByName`。新增或改名 Go 导出方法后，必须同步更新 bindings 和对应 `frontend/src/services/*.ts` 或组件调用。

## 构建、运行与测试

优先使用仓库 Taskfile：

```powershell
wails3 task dev
wails3 task build
wails3 task package
wails3 task common:generate:bindings
wails3 task common:update:build-assets
```

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

前端单独验证：

```powershell
Set-Location frontend
npm install
npm run build
npm run build:dev
```

Go 测试：

```powershell
go test ./services -run TestGemini -timeout 60s
go test ./resources/model-pricing -timeout 60s
go test ./... -timeout 60s
```

Windows 单文件测试构建必须先生成 `frontend/dist`，再 Go build。不要并行运行前端构建和 Go 构建，因为 `main.go` 会嵌入 `frontend/dist`，并行可能造成资源文件竞争。当前环境做 Windows 发布验证时显式设置 `GOARCH=amd64` 和 `CGO_ENABLED=0`，不要依赖默认架构。

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

## 发布流程

GitHub Release 由 `.github/workflows/release.yml` 在推送 `v*` tag 时触发。Workflow 会构建 macOS arm64/amd64、Windows amd64、Linux amd64，生成 `latest.json`，并发布 Windows 安装器、便携 exe、macOS zip、Linux AppImage/DEB/RPM 及校验文件。

发版前至少核对：

- `version_service.go` 的 `AppVersion`。
- `RELEASE_NOTES.md` 或 Release body 需要展示的版本说明。
- `build/config.yml`、`build/windows/info.json`、Linux nFPM 版本是否由 workflow 或本地流程正确更新。
- `frontend/bindings/` 是否与 Go 导出服务签名一致。
- 运行必要的 Go 测试和 `frontend` 构建。

需要用 `gh` 查询 Actions 或 Release 时，先检查当前进程是否有 `GH_TOKEN`，不得打印 token。若未继承但 Windows User/Machine 环境变量存在，可在单条 PowerShell 命令中临时注入再执行 `gh auth status` 或 `gh api user --jq .login`。

## 修改规则

- 修改前先查当前实现，不只看 README、历史方案文档或 `CLAUDE.md`。
- 只改当前任务需要的文件，不顺手重构、重排格式或更新无关文档。
- Go 文件修改后运行 `gofmt`；Vue/TypeScript 保持现有组合式 API 和服务封装风格。
- 数据库写入优先使用既有队列：高频 `request_log` 插入走 `GlobalDBQueueLogs` 批量队列，其他设置、黑名单、alias 等写入走 `GlobalDBQueue`。不要在高频路径直接 `db.Exec`。
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
| Relay 转发、协议转换、日志解析 | `go test ./services -run "TestReplaceModel|TestModelMapping|TestProviderConfig|TestModels" -timeout 60s` |
| Gemini provider 或 `.env` 解析 | `go test ./services -run TestGemini -timeout 60s` |
| 日志、统计、时区、成本 | `go test ./services -run "Test.*Range|Test.*Stats|Test.*Timezone" -timeout 60s` 和 `go test ./resources/model-pricing -timeout 60s` |
| Go 导出服务签名 | `wails3 task common:generate:bindings`，再 `npm run build` |
| 前端页面或服务封装 | `cd frontend && npm run build` |
| 构建/发布/资源嵌入 | 先前端 build，再按目标平台运行 Wails 或 Go build |

无法运行完整验证时，说明具体失败命令、失败原因和剩余风险，不把未执行的测试写成已通过。

## Windows 与编码

在 Windows 上优先使用 PowerShell 7。先用 `Get-Command pwsh` 查找路径，不要硬编码 WindowsApps 版本目录。命令脚本首部使用 `$ErrorActionPreference = 'Stop'`，读写文本显式指定 `-Encoding UTF8`，避免 `Get-Content`、`Set-Content`、`Out-File` 走默认编码。

执行 shell 命令时注意 PowerShell 与 Bash 语法不同：`rg` 正则包含 `|`、`(`、`)`、`\` 等字符时用单引号包住；复杂脚本拆成单个清晰命令。不要用 `cmd.exe`、`>` 或 `>>` 写文件。

## Git 边界

仓库可能有用户本地改动。开始修改前查看 `git status --short`，提交前查看 `git diff` 和 `git diff --cached --name-only`。未经明确要求不主动 commit、push、tag 或创建 Release。需要提交时只暂存本任务文件，不夹带生成物、历史文档清理或无关格式化。

当前 `AGENTS.md` 是已跟踪文件，不是被 `.gitignore` 忽略的临时说明文件；修改后应通过 `git diff -- AGENTS.md` 复核实际差异。
