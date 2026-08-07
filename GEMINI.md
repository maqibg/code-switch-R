# Code Switch (code-switch-R) 项目开发与协作指南

本指南旨在为后续参与 `code-switch-R` 的 AI 助手和开发者提供统一的上下文、架构原则、开发规约和日常操作流程，以确保代码质量、架构一致性和维护效率。

---

## 一、 项目定位与核心功能

`code-switch-R` 是一个仅发布 Windows amd64 便携版的桌面应用。它采用 Wails 3 桌面框架，通过本地 HTTP Relay 管理 AI CLI 工具（如 Claude Code, Codex, Gemini CLI, Reasonix, Pi 与 Grok Build）的供应商配置。

### 核心功能体系
1. **多供应商与优先级调度**: 灵活配置多个 API 供应商，支持按优先级级别（Level 1 至 Level 10）智能降级。
2. **本地 HTTP Relay**: 代理各大 CLI 客户端的请求，执行安全 Token 验证、失败拉黑/降级转发，记录请求日志。
3. **协议双向适配 (Protocol Adapter)**: 实现了 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 和 Gemini Native API 等多协议间的无缝双向矩阵转换。
4. **用量统计与成本追踪**: 支持按官方定价计算 Token 消耗、成功率和热力图统计，完成额度快照采集。
5. **CLI & MCP 集中管理**: 可视化托管并自动同步各 AI CLI 平台的 MCP（Model Context Protocol）服务器配置、系统 Prompt 和配置变量，且在检测到外部第三方修改时安全防撞。

---

## 二、 技术栈与目录职责

### 1. 主要技术栈

| 层次 | 选型 | 备注说明 |
|---|---|---|
| **桌面框架** | **Wails v3** | Go 模块名为 `codeswitch`，负责原生窗口、托盘及 Go-TS 桥接 |
| **后端语言** | **Go 1.24** | 纯 Go 环境构建，无 CGO 依赖 |
| **Web 路由** | **Gin** | 用于本地本地 Relay 的入站 HTTP 路由注册与处理器绑定 |
| **持久化** | **SQLite (modernc.org)** | 采用纯 Go 版本的 SQLite 驱动，配置 WAL 模式保证读写吞吐 |
| **前端框架** | **Vue 3 + TS** | 使用 Vite、Tailwind CSS 4、Vue Router 和 Vue I18n |
| **任务运行** | **Taskfile.yml** | 统一管理前后端编译、绑定生成、打包及发布生命周期 |

### 2. 目录结构与职责说明

* `main.go`: 应用入口。负责 SQLite 初始化、服务注册、托盘及窗口路由绑定、以及 `frontend/dist` 静态资源嵌入。
* `version_service.go`: 定义应用版本（`AppVersion`）与更新策略（`portable`）。发布更新时须手动核对。
* `services/`: 后端核心服务，通过 `application.NewService` 暴露给前端。
  * `providerservice.go`: 供应商 CRUD、改名事务（`RenameProvider` 支持 48h 别名兼容）、模板关联。
  * `pi_settings.go` & `pi_platform_settings.go`: 管理 Pi 平台级托管与恢复，外部冲突防撞。
  * `blacklistservice.go` & `blacklist_level_config.go`: 处理拉黑冷静、恢复定时器和 Level 配置。
  * `cliconfigservice.go`: 托管各 CLI (Claude Code 等) 的原始环境配置备份与接管状态控制。
  * `logservice.go`: SQL 聚合，支撑热力图、Token 账单、以及成本估算。
  * `mcpservice.go`: 处理 MCP 跨平台同步。
* `internal/relay/`: 代理转发引擎。
  * `internal/relay/providerrelay.go`: Relay 核心，负责监听 localhost/WSL、路由分配、Token 校验和模型映射。
  * `internal/relay/protocol_matrix_adapter.go` & `protocol_matrix_stream.go`: 多协议入站/出站双向转换核心（含 Gemini 原生转换，协议枚举在 `services/protocol` 子包）。
  * `relay_dispatch.go`: 轮询、负载与 Level 重试策略调度器。
* `resources/model-pricing/`: 模型官方价格和上下文配置，以及相应的成本核算算法。
* `frontend/`: Vue 3 前端源码。
  * `src/components/`: 按功能和模块划分的 UI 视图组件。
  * `src/services/`: 对 `bindings/` 目录中 Wails 导出方法的封装层。
  * `bindings/`: **禁止手动修改**，由 Wails 3 根据 Go 代码自动生成的 TypeScript 桥接。
* `build/`: 编译与打包配置。包含 Windows 便携版图标及 `.syso` 信息生成配置。

---

## 三、 持久化与数据安全

项目的所有本地数据都必须存放于可执行文件同级目录的 `.code-switch-R` 下。该路径通过 `internal/infra/userhome.go` 中的 `GetAppConfigDir()` 统一获取。

> **警告**: 严禁在代码中直接写死诸如 `~/.code-switch` 或 `%USERPROFILE%` 等硬编码路径。

### 核心数据存储对照表
* `.code-switch-R/app.db`: SQLite 主库（含 `provider`、`request_log`、`relay_attempt`、`provider_blacklist`、`app_settings`、`schema_version` 等表）。
* `.code-switch-R/claude-code.json` / `codex.json` / `reasonix.json`: 各平台 Provider 配置。
* `.code-switch-R/pi.json` & `pi-provider-templates.json`: Pi 应用侧供应商及网关模板。
* `.code-switch-R/app.json`: 全局应用设置（自动更新、预算周期、通知、代理开关等）。
* `.code-switch-R/network.json`: Localhost/WSL 监听地址和网络 Token。**注意: Token 属于机密，绝不向前端暴露**。
* `.code-switch-R/mcp-{platform}.json` & `mcp-managed-{platform}.json`: MCP 受管条目及指纹信息。
* `.code-switch-R/proxy-state/{platform}.json`: 备份接管 CLI 前的原始状态文件。

---

## 四、 核心开发规约与设计标准

### 1. 启动与关闭链路
* **启动顺序**:
  1. `InitDatabase()`: 初始化 SQLite 数据库，并设置 `PRAGMA busy_timeout = 30000` 与 WAL。
  2. 清理已弃用的旧版迁移残留（如 `DeepSeekCode` / 旧 Skill 数据）。
  3. 读取随机 Relay Token 与网络设置，拒绝监听 `0.0.0.0`，仅限安全本地入站。
  4. 构造服务并同步启动 `ProviderRelayService`，若监听报错直接阻断启动。
  5. 后台轮询：开启黑名单过期恢复定时器。
  6. 构造 Wails App 注册所有 bindings 接口。
* **关闭逻辑**: 关闭时必须按序停止黑名单定时器、Grok 服务以及 Relay 监听，释放文件锁。

### 2. Go 后端开发指南
* **无 CGO 约定**: 项目在 Windows 环境以 CGO_ENABLED=0 进行编译，任何引入的依赖库（包括 SQLite）必须支持纯 Go 运行环境。
* **修改与删除事务**: 改名、删除、甚至字段变动均要在对应的数据库和文件系统执行级联事务回滚（见 `provider_delete.go` 与 `provider_rename.go`）。
* **Wails 绑定更新**:
  修改任何被前端调用的 Go 结构体或方法签名后，**必须**运行绑定生成任务，禁止直接在前端 `bindings/` 目录下手动魔改。
* **外部冲突控制**: 在接管外部 CLI（例如 MCP 设置或 Pi 配置文件）时，仅修改项目明确拥有所有权的**受管字段**（Managed Fields）。写入前须检查外部是否被第三方篡改。

### 3. 前端 Vue 3 开发指南
* **组件语法**: 一律使用 Composition API 配合 `<script setup lang="ts">` 语法糖，规范声明 `defineProps` 与 `defineEmits`。
* **样式框架**: 遵循 Tailwind CSS 4 设计规范，避免在组件中硬编码繁复的行内样式，保持间距、字号和圆角的系统一致性。
* **API 访问**: 严禁在组件内部直接调用 `bindings/` 下的原生 Wails 接口。所有交互均需通过 `frontend/src/services/` 下封装的干净 Service 层进行，以便于进行模拟测试或缓存优化。

### 4. 单元测试与验证要求
* **Empirical Reproduction**: 在修复 Bug 前，必须先在 `_test.go` 中编写能稳定复现此 Fail 状态的测试用例。
* **并发与安全**: 任何对 `ProviderRelay`、`Blacklist` 的改动必须通过并发安全测试（如 `blacklist_concurrency_test.go`）。
* **模型定价与计量**: 协议变动时，必须通过 `pricing_single_engine_test.go` 和 `logservice_billing_stats_test.go` 等账单相关单元测试，防止计费越界或溢出。

---

## 五、 常用维护与构建指令

项目依赖 `Task` 任务管理工具进行日常流程控制。

```bash
# 1. 整理 Go 依赖
task common:go:mod:tidy

# 2. 生成 Wails 3 前端 TypeScript 绑定 (签名更改后必执行)
task generate:bindings

# 3. 运行本地开发环境 (Wails Dev 模式，支持热重载)
task dev

# 4. 编译打包 Windows 便携版 (开发/调试包)
task build

# 5. 打包 Windows amd64 生产发布版本 (压缩, 隐藏控制台, 剥离调试符号)
task package

# 6. 后端单元测试
go test ./...

# 7. 前端 Vitest 单元测试
cd frontend && npm run test
```

---

*注：本 `GEMINI.md` 文件是项目全局开发规范。修改核心领域（如 Grok 运行模式或 Gemini Native Relay）时，请在修改完毕后同步校验本指南是否需要增补。*
