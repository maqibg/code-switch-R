# code-switch-R 项目说明

本文件是仓库的共享说明，供 Codex、Claude Code 等代码代理和开发者阅读。已按个人 fork 精简：只保留开发需要的事实和硬约束，删掉面向多人的协作仪式。若本文件与当前代码冲突，一律以代码事实为准；发现冲突时先改这里。

## 项目是什么

仅发布 Windows amd64 便携版的 Wails 3 桌面应用。用 Go 后端、Vue 3 前端和本地 HTTP Relay 管理多个 AI CLI（Claude Code、Codex、Gemini CLI、Pi、OpenCode、Grok Build）的供应商配置。Relay 用随机 Token 鉴权、只监听 localhost，按供应商启用状态、显式模型映射、Level 分组和黑名单状态做降级转发，并记录请求、Token、成本。

| 层级 | 实现 |
|---|---|
| 桌面框架 | Wails 3，Go module `codeswitch` |
| 后端 | Go 1.24、Gin、`modernc.org/sqlite`、`xdb`、`xrequest` |
| 前端 | Vue 3、TypeScript、Vite、Tailwind CSS 4、Vue Router（hash）、Vue I18n |
| 持久化 | 可执行文件同级 `.code-switch-R/` 目录，JSON + SQLite |
| 发布 | GitHub Actions tag 触发的 Windows amd64 便携版 Release |

## 目录地图

- `main.go`：Wails 入口、服务注册、窗口/托盘、启动/关闭顺序、`frontend/dist` 嵌入。
- `version_service.go`：`AppVersion` 常量。改版本时要与 tag、`build/config.yml`、`build/windows/info.json` 三处一致。
- `services/`：后端核心。新增前端可调用能力在这里实现并经 `application.NewService` 注册。
- `internal/relay/`：Relay 域独立包，单向 import `services`。路由注册、failover 调度、协议矩阵转换、Codex 续写、遥测。
- `internal/dbcore/`：SQLite 原语（`Handle`/`Exec`/`ExecStatements`/`ExecInImmediateTx`）。
- `internal/httpx/`：代理感知出站客户端、header 工具。
- `internal/infra/`：原子写、文件工具、路径、结构化日志。
- `frontend/src/`：Vue 应用。页面在 `components/`，服务封装在 `services/`，路由在 `router/index.ts`。
- `frontend/bindings/`：Wails 自动生成的 TS 绑定，**禁止手改**。
- `resources/model-pricing/`：LiteLLM 内置价格快照 + 自定义规则 + 定价计算。
- `build/`：Wails 通用配置与 Windows 便携构建。
- `.github/workflows/release.yml`：tag 触发的 Windows amd64 便携构建与资产发布。
- `scripts/`：调试/发布辅助脚本，很多面向特定历史问题，执行前先读内容。
- `doc/`、`CONTEXT.md`、`GEMINI.md`、README：历史方案文档，不代表当前实现，结论必须用代码复核。

## 启动链路（顺序不能乱）

`main.go` 顺序执行：

1. `services.InitDatabase()`：创建 `.code-switch-R`、打开 `app.db`、PRAGMA（WAL + 30s busy_timeout 走 DSN 并校验）、跑 `schema_version` 顺序迁移（当前 14 版）。
2. `CleanupRemovedDeepSeekCodeProxy()` / `CleanupRemovedCustomCLIData()`；`LoadNetworkRuntimeSettings()` 读 Relay Token 和监听地址。
3. 构造全部服务。依赖：PiSettingsService 依赖 ProviderService；PricingService 依赖 AppSettings 和 PiSettings；ProviderRelayService 与 LogService 在构造参数接收 PricingService（无后置注入）。ProviderRelayService **不注册进 Wails**。
4. `providerRelay.Start()`；`openCodeService.Start()`；`RefreshManagedWSLRelayCredentials()`。
5. 后台启动黑名单过期恢复定时器。
6. 创建 Wails 应用、窗口、托盘、注册服务并运行。

关闭 `app.OnShutdown()` 顺序：停黑名单定时器 → `logService.StopMaintenance()` → `grokBuildService.Stop()` / `providerRelay.Stop()` / `openCodeService.Stop()` → `CloseDatabase()`（WAL checkpoint 后关闭）。

## 持久化位置

真实数据目录是可执行文件同级 `.code-switch-R`（`internal/infra/userhome.go` 的 `GetAppConfigDir()`）。不要用旧文档的 `~/.code-switch` 或 `%USERPROFILE%\.code-switch`，除非代码明确在做旧数据导入。

| 文件 / 目录 | 用途 |
|---|---|
| `.code-switch-R/app.db` | SQLite 主库：`provider`、`request_log`、`relay_attempt`、`provider_blacklist`、`app_settings`、`schema_version` |
| `.code-switch-R/*.json.migrated` | 迁移前各平台 providers JSON 备份，主存储已入库，不再读写 |
| `.code-switch-R/pi.json` | Pi 应用侧供应商、模板标识、模型定义/覆盖、请求头、`metadataUserId` |
| `.code-switch-R/pi-provider-templates.json` | Pi 网关协议模板；缺失加载内置模板 |
| `.code-switch-R/provider-request-templates.json` | 用户保存的请求头与 `metadataUserId` 模板 |
| `.code-switch-R/app.json` | 应用设置、预算、更新、通知、代理 |
| `.code-switch-R/network.json` | 监听模式、WSL/局域网配置、内部 Relay Token（Token 不向前端暴露） |
| `.code-switch-R/grok-runtime.json`、`grok-oauth-accounts.json` | Grok 运行模式、接管字段、OAuth 账号 |
| `.code-switch-R/opencode-settings.json`、`opencode-state.json` | OpenCode 配置路径与托管状态（Targets/Managed） |
| `.code-switch-R/proxy-state/{platform}.json` | CLI 代理启用前后的恢复状态 |
| `.code-switch-R/mcp-{platform}.json`、`prompts.json`、`frontend-preferences.json` | MCP / 提示词 / 前端偏好 |
| `.code-switch-R/download_state.json`、`pending_apply.json`、`dismissed_version.txt` | 自动更新状态 |

## 核心后端模块

- `Provider`：供应商主数据，存 SQLite `provider` 表。表只把会被 SQL 查询/排序的字段做成列（`id, platform, source_id, name, api_url, api_key, enabled, level, sort_order`），其余约 35 个长尾字段（ModelMapping、AuthScheme、Headers、PiModels、RequestIdentity、gemini/openCode 载荷等）打包进 `config_json`。
- `ProviderService`：全表替换式 `SaveProviders`（单事务删不在列表的行 + upsert，`sort_order`=切片下标）；`LoadProviders` 读库并回填校验。改名走 `RenameProvider`；编辑页同时改名+保存走 `SaveProvidersWithRename`。对 `"pi"` 额外触发 `setPiGatewaySync` 回调同步 `~/.pi/agent/models.json`，失败回滚 DB。删除须保留 `provider_delete.go` 的清理与回滚语义。
- `Protocol Adapter`（`internal/relay/protocol_matrix_adapter.go`、`protocol_matrix_stream.go` 等）：Anthropic Messages / OpenAI Chat / OpenAI Responses / Gemini Native 的双向矩阵转换。协议枚举在 `services/protocol` 子包。无法保持 reasoning、工具选择或流式事件语义时显式失败；流式一旦提交不得再切换 Provider。
- `BlacklistService`：失败拉黑与过期自动恢复。黑名单定位靠 `BlacklistTarget` 与 `provider_blacklist.blacklist_level`（非 Provider 上的字段）。
- `PricingService` + `resources/model-pricing`：LiteLLM 内置价格表下载/校验/原子快照、自定义正则规则、分段价格。
- `MCPService`：管理平台 MCP 配置；只同步应用拥有的条目，外部修改冲突时拒绝覆盖。
- `PromptService`：管理各平台提示词，写入前检测外部修改。
- `NetworkService`：只管理 localhost 与 WSL 监听，拒绝 `0.0.0.0` 和任意自定义地址，能写 WSL 内 CLI 配置。
- `UpdateService`：优先读 GitHub Release `latest.json`，回退 GitHub API；下载 URL 有白名单校验。

## Relay 路由

`ProviderRelayService.registerRoutes()` 当前注册：

```text
POST /v1/messages                     -> claude
POST /responses | /v1/responses | /v1/v1/responses | /codex/v1/responses     -> codex
POST /responses/compact 及同前缀变体    -> codex
POST /grok/v1/responses 及 /compact    -> grok
GET  /v1/models                       -> claude
GET  /grok/v1/models                  -> grok（固定返一个 grok-build model）
GET|POST /gemini/v1beta/*any          -> gemini
GET|POST /gemini/v1/*any              -> gemini
POST /pi/providers/:provider/*any     -> Pi 平台级协议网关
```

Provider 过滤顺序：Enabled/URL/Key 有效 → `ValidateConfiguration()` 通过 → 显式 `ModelMapping` 命中（未配置映射时不限制模型）→ 未被拉黑 → 按 Level 升序分组，同 Level 可轮询。

失败分类（`classifyDispatchFailure`）：`errSkipProvider` 跳过不记失败 → `ErrClientRequestRejected` 直接 400 不切换 → `errResponseCommitted` 停止切换但记失败 → `errClientAbort` 停止且不计失败 → 其余切除重试到下一层。

认证默认 `bearer`（`defaultConnectivityAuthType`）。显式 `AuthScheme`/`ConnectivityAuthType` 优先；协议类型只控制协议头（如 `anthropic-version`），不得把显式 `x-api-key` 改成 Bearer；转发前删除客户端认证头。`metadataUserId` 只允许最终上游协议为 Anthropic Messages。

统计成功统一定义为 HTTP 2xx 且 `error_type` 为空；失败是该条件补集。

## Pi 关键点

- `models.json.providers.<id>` = 页面里一个平台；`pi.json` 可在平台下挂多个网关供应商（这些供应商不得作为额外 Pi Provider 写回 `models.json`）。
- 托管（EnableProxy / EnablePlatformProxy）只改平台级 `baseUrl`、`apiKey` 和同名 `auth.json` 条目，保留其余字段；有模型级 `baseUrl` 的平台不得托管。
- 关闭托管前必须校验注入内容哈希，外部修改显式报冲突，禁止静默覆盖。
- 导入的 Pi 配置值按 Pi 语义解析 `$ENV`、`${ENV}`、`!command`、`$$`、`$!`；`auth.json` 的 `api_key` 优先于 `models.json.apiKey`；暂不支持无损导入 OAuth 条目。

## 构建 / 运行 / 测试

Windows 单文件构建必须**先生成 `frontend/dist`，再编 Go，两步串行**（`main.go` 嵌入 `frontend/dist`）。

```powershell
Set-Location frontend
npm ci
npm run build
Set-Location ..
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o bin/code-switch-R-test.exe
```

本机 Go 默认 GOARCH=386，跑 services 测试会在 webview2 COM 注册处 panic，务必显式设 `GOARCH=amd64`。

常用命令：

```powershell
# 前端生产构建（也是前端改动的最小验证）
Set-Location frontend; npm run build

# Go 定向测试（-run 过滤）
$env:GOARCH = "amd64"; go test ./services -run 'TestProvider|TestPi|TestRename' -timeout 60s
$env:GOARCH = "amd64"; go test ./internal/relay -timeout 120s
$env:GOARCH = "amd64"; go test ./... -timeout 300s   # 发版前全集

# Go 导出签名变化后必须重新生成 bindings，再构建前端
wails3 task common:generate:bindings
```

前端改动用 `npm run build` 验证；Go 接口改动先跑所属包定向测试；不把桌面集成、完整打包套到每次修改。

## Windows Shell / 编码

优先 PowerShell 7（`Get-Command pwsh`，不硬编码 WindowsApps 目录）。脚本首部 `$ErrorActionPreference = 'Stop'`，读写文本显式 `-Encoding UTF8`。不要用 `cmd.exe`、`>`、`>>` 写文件。启动可见桌面窗口的 GUI 进程用 `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` 并申请沙盒外执行。`rg` 正则含特殊字符用单引号包住。

## 硬约束（删了会坏，别动）

- **Wails 注册服务导出签名里出现的类型必须留在 `services` 包**：bindings 生成器按类型声明包决定模型输出路径，别名会把模型生成到 `frontend/bindings/codeswitch/internal/...`，破坏前端导入（实测）。
- **不要为给 `internal/relay` 开内部入口而在注册服务上新增导出方法**：注册服务的导出方法会全部暴露给前端（实测）。内部接缝一律走 `services/relay_seams.go` 的包级导出函数。
- **改 Go 导出签名必须重新生成 bindings**，并同步查 `frontend/src/services/*.ts` 和组件调用。
- **数据库写入没有队列**：WAL + DSN 级 busy_timeout + 同步短事务。读改写用 `BEGIN IMMEDIATE`，一组相关写用 `ExecStatements` 原子提交；不要重新引入队列或后台批量缓冲。
- **SQLite 参数化，禁止字符串拼接用户输入**；仅受控表名等内部白名单场景可显式校验后拼接。
- **CLI 配置只能手术式更新字段**，必须保留用户原有配置与备份状态（`proxy-state/`），禁止用模板整体覆盖；WSL 走 `NetworkService` 的 `wsl -d` 脚本校验。
- **不打印/写入 API Key、auth.json 凭据、Release token**；不 commit `.env`。
- **不主动 commit / push / tag / release**（个人项目，需要时明确说）。
