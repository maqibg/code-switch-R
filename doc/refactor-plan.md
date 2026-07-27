# 重构开发计划

依据:`doc/design-review.md`(含第二轮复核)。
前提:大预算,可破坏性改动与一次性迁移;协议矩阵、Codex 续写/bridge、Pi 托管、CLI 写入为高风险区,小步改并配测试。

## 分批与依赖

```
第 0 批(止血 + 价值观债)  ──> 第 1 批(安全网测试) ──> 第 2 批(并发→存储) ──> 第 3 批(转发收敛) ──> 第 4 批(结构治理)
        独立可并行                  解锁 A3/A6/B5/B6        A2 必须先于 A1        依赖第 1 批测试        可与日常开发并行
```

关键依赖(不可颠倒):
- **A2(去队列)先于 A1(Provider 入库)**:Provider 入库需要可靠事务,当前队列让事务用不了。
- **M6(测试)先于 A3/A6**:三套转发循环合并、四份 settings 合并没有安全网就是盲改。
- **B5/B6 的"手术式编辑"改造先于 A6**:settings 服务合并前先把写入语义修正,避免把坏语义复制进公共实现。

## 第 0 批:止血 + 根决策 4(价值观债)

| # | 项 | 改动位置 | 验证方式 |
|---|---|---|---|
| 1 | B1 `$pid` → `$targetPid` | updateservice.go:939,946 | 单测断言生成脚本不含只读变量赋值 |
| 2 | B4 busy_timeout 全连接生效 | database.go:28,46 | 先实证 DSN `_pragma` 是否透传,再改;临时测试验证后删除 |
| 3 | B3+M4 删除 health_check_history 全套 | provider_delete.go、provider_rename.go、logservice.go、configtransferservice.go + 测试 | 现有 delete/rename 测试改造后通过 |
| 4 | B2 装配 piGatewaySync | services 包内构造处 | 新增生产装配回归测试 |
| 5 | B9.1/B9.2 Body 关闭 + context 绑定 | providerrelay.go:907-930、1022-1048 | 编译 + 现有 relay 测试 |
| 6 | B5 Codex 解析失败改拒绝 + 删错误注释 | codexsettings.go:98-102 | 新增测试:坏 TOML + 已启用状态下不覆盖文件 |
| 7 | B9.8 Codex 历史未命中告警 | relay_forward_execution.go:100-103 | 编译 + 现有 codex 测试 |
| 8 | B6 Gemini .env 按行编辑 | geminiservice.go:456-558 | 新增测试:注释/export/空值键保留 |
| 9 | B7 区分上游断流与客户端写失败 | providerrelay.go:996-1011 | 新增测试:上游中断记 Failure 而非 Success |

B8(黑名单计数竞态)**不在第 0 批**:根因是队列强制读-改-写,第 2 批 A2 完成后用 `SET failure_count = failure_count + 1` 一并解决,避免现在改一版、去队列后再改一版。

## 第 1 批:安全网测试(M6)

1. 四个 settings 服务(claude/codex/reasonix/gemini):`t.Setenv` 临时 home 表驱动测试,覆盖 Enable / Disable / 重复 Enable / 坏配置拒绝 / 状态文件损坏兜底 / auth.json 手术式更新。
2. relay failover 主循环:拉黑固定模式的阈值重试、降级模式的单次失败切换、响应已提交后停止降级、`ErrClientRequestRejected` 直接 400 不切换。
3. dbqueue:并发入队、Shutdown 期间入队、批量中一条失败的降级行为、panic 重启与 Shutdown 竞态。

第 1 批的测试同时是第 2 批去队列的回归基线,必须先建立。

## 第 2 批:并发与存储(顺序敏感)

### 2.1 A2 去队列(先做)
- 决策:去掉双队列,改 WAL + DSN busy_timeout + 短事务;高频日志写入改为单条多行 INSERT 的批量事务,由调用方直接执行。
- 连带消解:B8 原子自增、B9.4/B9.5/B9.6 队列关闭与批失败、P2 遥测串行等待、settings 双行 Saga。
- 风险:所有写路径都要复核;必须有第 1 批 dbqueue 测试作为行为基线。

### 2.2 A1 Provider 主数据入 SQLite(后做)
- Provider 元数据入库,int64 ID 作关联主键;JSON 降级为导入/导出格式。
- 可删除:`provider_alias` 表 + 48h TTL、禁止链式改名限制、文件-DB 补偿 Saga、rollbackFile 路径。
- 连带:M3 引入 `schema_version` 顺序迁移机制;回填 `platform='custom:<id>'` 旧格式行后删除兼容 OR(M5);app 设置单一化(消除 blacklist_level_config 的 JSON/DB 双源)。
- 迁移要求:一次性迁移 + 失败可回滚 + 迁移前自动备份数据目录。

### 2.3 A7 JSON 落盘统一
- 所有 JSON 写入统一走 `atomicWriteFile`(app.json 当前是裸 `os.WriteFile`),锁从全局单把改为按路径分段。

### 2.4 B9.10 日志保留默认值
- 给 `LogRetentionDays` 非零默认值,但必须配合升级时显式提示(行为变更,不可静默删用户历史数据)。

## 第 3 批:转发层收敛

- A3:抽出统一 `dispatchWithFailover(scope, providers, forward)`,三套 handler(claude 通用 / gemini / customCli)收敛为一套;`GeminiProvider` 并入统一 `Provider` 模型(string ID 做迁移映射),删除 `roundRobinOrderGemini`。
- 顺带:P1 黑名单内存缓存、P3 metadata 注入改 sjson 定点、P5 请求级配置快照。
- M2:providerrelay.go 拆分(路由 / failover / DDL / usage 解析器分离);request_log 读写收敛到单一 repository。

## 第 4 批:结构治理(渐进)

- A6:`PlatformDescriptor` 注册表,一处注册各方查表;四份 settings 服务合并为通用实现 + codex TOML 分支。
- A4/A5:按域拆子包(relay/provider/pricing/logging/blacklist/pi),跨包走小接口;删 `SetPricingService` 与 LogService 三个构造变体(消除三套定价引擎并存);M1 `ReqeustLog` → `RequestLog`;M7 引入 `log/slog`。
- 前端 F1-F4:Main/Index.vue 按平台适配器拆 composable、引入状态层、`Call.ByName` 收敛到 bindings、删死代码与空目录。

## 验证策略

每批完成后:
- `go build .` + `go vet ./services`
- 相关 `go test ./services -run <过滤>`(注意:`go test ./services` 全量在本机因 Wails webview2 COM 初始化崩溃,需按 -run 过滤跑)
- 前端改动:`cd frontend && npm run build`
- Go 导出签名变化:`wails3 task common:generate:bindings` 后再构建前端

## 执行记录

见本文件末尾「进度」小节,随开发更新。

## 进度

### 已完成

**第 0 批(9 项全部完成)**

| 项 | 实现 | 测试 |
|---|---|---|
| B1 | 脚本抽成 `buildWindowsPortableUpdateScript`,`$pid`→`$targetPid` | `update_windows_script_test.go`(含只读变量黑名单扫描) |
| B4 | 新增 `database_dsn.go`,PRAGMA 进 DSN + `verifySQLitePragmas` 启动校验 | `database_dsn_test.go`(含旧写法对照测试) |
| B3+M4 | 删除 `health_check_history` 全套 DDL/DML/字段/统计/导入 | 改造 delete/rename 现有测试 |
| B2 | `NewPiSettingsService` 装配 `syncGatewayIfEnabledWithProviders` | `pi_gateway_sync_wiring_test.go` |
| B9.1 | `extractUpstreamError` 用 defer 关闭 Body | 覆盖于 relay 现有测试 |
| B9.2 | `forwardRequest` 绑定 `c.Request.Context()` | 同上 |
| B5 | 解析失败改拒绝 + 每次启用写时间戳快照(新增 `config_backup.go`) | `codexsettings_test.go` |
| B9.8 | Codex 历史未命中输出告警 | 覆盖于 codex 现有测试 |
| B7 | 新增 `relay_client_writer.go` + `relay_copy_failure.go`,用包装 writer 区分两侧;**并修正 `errResponseCommitted` 分支原先直接 return 跳过记账** | `relay_copy_failure_test.go` |

**第 0 批范围扩展(执行中发现)**

根决策 4 的"解析失败继续"模式不止 codex 一处,同一 bug 存在于 4 个拷贝(正是根决策 3 的预测):
`claudesettings.go:101`、`reasonixsettings.go:88`、`cliconfigservice.go` 的 `saveClaudeConfig` 与 `saveCodexConfig`。全部改为拒绝,claude/reasonix 同时加时间戳快照。

**第 1 批(M6 安全网,已完成)**

- `settings_proxy_lifecycle_test.go`:表驱动覆盖 claude/codex/reasonix 四条共同契约(保留无关配置、坏配置拒绝且不改写、启停往返、重复启用幂等)+ Claude env 键级验证。为 A6 合并 settings 服务准备好基线。
- `dbqueue_test.go`:并发写入、批量写入、关闭后拒绝、关闭排空、统计,以及 B9.6 的现状固定测试。
- `env_file_edit_test.go`:含与 `parseEnvFile` 的对称性测试。

### 执行中的重要发现

1. **本机 Go 工具链是 386,`go test ./services` 会在 Wails webview2 的 COM 回调注册处 panic**,与项目代码无关。跑测试必须 `$env:GOARCH='amd64'`。发布 workflow 显式设了 amd64 所以 CI 从未暴露。
2. **`database.go:27` 原注释"modernc.org/sqlite 需要显式执行 PRAGMA"是错的**。实测 DSN `_pragma=busy_timeout(30000)` 对连接池每条连接都生效;而旧写法 `db.Exec("PRAGMA busy_timeout")` 只有第 1 条连接是 30000,其余为 0。对照测试已固化在 `database_dsn_test.go`。
3. **B7 的修复不止一处**:`judgeResponseCopyFailure` 只解决了分类,`providerrelay.go` 两个 `errResponseCommitted` 分支原先在 `RecordFailure` 之前就 return,所以"流中途断开"从来没进过 provider 记账。两处都已补上。
4. **代理状态文件不在 HOME 下**(在可执行文件同级 `.code-switch-R/proxy-state/`),`t.Setenv("HOME")` 隔离不了它,测试之间会串扰。已在测试里加 `resetProxyState` helper 并注明原因。这也是 A1 存储统一时要处理的测试性问题。
5. **B9.6 已被实测确认**:同批 3 条(含 1 条违反 NOT NULL 的 SQL)提交后落库 0 行——两条正常数据被连带回滚丢弃。

### 第 3 批与第 4 批(本轮完成部分)

**A6 平台注册表 + settings 服务合并(完成)**

- `platform_registry.go` 扩展 `PlatformDefinition`,新增 `Aliases` 与 `ProviderFile`;新增 `resolvePlatformID`(别名归一)与 `providerFileNameFor`(唯一的 kind → 文件名映射)。
- 修掉一个真实缺陷:`providerFilePath` 与 `providerFilePathNoCreate` 原本各维护一份 switch 且已分叉,后者不认 pi 和 custom,返回空路径被调用方当成"无配置"静默跳过,直连应用对这两类平台失效且不报错。现在两者共用同一映射,`platform_registry_filename_test.go` 锁死一致性。
- 别名归一收进注册表:`provider_rename.go` 的 `resolvePlatform`、`mcpservice.go` 的 `normalizePlatform`。后者有个坑:它输出 `claude-code` 而非 `claude`,这个值决定 `mcp-{platform}.json` 的文件名,直接合并会让用户已有 MCP 配置失联,所以只共享别名判断、输出值各自保留(已在注释写明)。
- settings 服务合并:新增 `json_proxy_settings.go`(通用启停)、`json_proxy_direct_apply.go`(通用直连应用)、`json_proxy_platforms.go`(平台差异描述符)。claude 与 reasonix 两个服务从 746 行降到 95 行,只保留 Wails 绑定所需的方法外壳并委托给通用实现。Codex 的 TOML 路径按计划保持独立。
- 五处真实差异用描述符参数化:配置结构嵌套与否、字段键名、baseURL 路径后缀、`EnvExisted` 语义、写后是否清理空对象。
- 安全网:第 1 批的 `settings_proxy_lifecycle_test.go` 四条契约 + `TestClaudeEnableProxyOnlyTouchesProxyEnvKeys` 全部通过,确认 env 嵌套的恢复语义未被改坏。

**F4 删死代码(完成)**

删除零引用文件 `DeepLinkImportDialog.vue`、`endpointSync.ts`、`hotkeyUtils.ts`、`data/piProviderTemplates.ts`,空目录 `components/Availability/`、`components/SpeedTest/`,以及内容过期的 `services/TEST_README.md`。删前逐个确认引用数为零,删后前端构建通过。

**M7 结构化日志(完成)**

- 新增 `applog.go`:`log/slog` 自定义 handler,日志直接进控制台环形缓冲,级别是结构化字段而不是从文本里拆词猜的(旧 `classifyConsoleLogLevel` 会把消息里的 "error" 字样误判成级别)。
- 修掉实现过程中自己引入的重复投递 bug:handler 既要写环形缓冲又要写终端,若终端写的是被 `ConsoleService` 替换过的 `os.Stdout`,`readPipe` 会读回来再 `addLog` 一次,同一条日志在控制台出现两遍。解决办法是写截获前的原始 stdout,`TestAppLogWritesTerminalOnceAndBufferOnce` 锁死这一点。
- 迁移 23 处带级别前缀的 `fmt.Printf` 到 slog(providerrelay.go 19 处含 3 处多行改结构化字段,另 4 个文件 7 处)。
- 有意未迁的 54 处:`[CustomCLI]` 与 `[Gemini]` 前缀是模块名不是级别,且全部位于 A3 将要合并掉的两个重复 handler 内,现在迁等于改马上要重写的代码。留给 A3 一并处理。
- stdout 捕获保留:项目里仍有大量既有 `fmt.Printf`,以及 gin / wails 的第三方输出,这些只能从管道拿。

### M3 迁移框架(完成)

新增 `migrations.go` + `migrations_baseline.go`:`schema_version` 表、有序迁移列表、每个迁移单事务(失败整条回滚且不记版本以便重试)。`InitDatabase` 成为建表唯一入口,业务路径上的 `ensure*` 改为幂等兜底。

实现中被自己写的测试抓到一个缺陷:SQLite 不允许 `ALTER TABLE ADD COLUMN` 带非常量默认值,表内有数据时直接失败。`created_at` 因此保留在建表语句里,旧库缺该列时补一个可空版本 —— 原实现把它放在 `CREATE TABLE` 中正是这个原因。

副作用:services 测试套件耗时由约 42 秒降到约 10-20 秒(消除了每列一次 pragma 探测)。

### A1 Provider 入库(前两步完成,第三步未开工)

**已完成 · 第 1 步:`provider` 表 + 从 JSON 导入**(迁移 v2)

- 会被查询/排序的字段做列,其余约 30 个长尾配置进 `config_json`(`provider_config_json.go`)。
- 导入保留现有 int64 ID(不重编号 —— 紧接着要按这些 ID 关联日志行),`sort_order` 保留 JSON 原始顺序。
- 覆盖注册平台四个文件 + `providers/{toolId}.json`。
- 测试含一个字段覆盖检查:比对 `Provider` 与 `providerConfigPayload` 的 JSON 标签集合,将来给 Provider 加字段却忘记同步时会失败。已用临时探针验证该机制真能检出遗漏(37 字段 = 6 列 + 30 config_json + 1 个有意丢弃的 `piTemplate`)。

**已完成 · 第 2 步:日志表加 `provider_id` 并回填**(迁移 v3)

- 按 `(platform, source_id, provider 名)` 回填,匹配不上的留 NULL(早已删除的供应商)。
- `name` 列有意保留:它记录请求发生当时该供应商叫什么,这个历史事实本身有价值 —— `provider_alias` 机制其实是在勉强模拟这件事。
- 同一迁移归一化 `platform='custom:<toolId>'` 历史行。顺序是先归一 platform 再回填,否则旧格式行匹配不上。

**未开工 · 第 3 步:切换读写路径**

这是真正的切换点,也是风险集中处。范围:`LoadProviders` / `SaveProviders` / `loadProvidersRaw` / `SaveProvidersWithRename` 改走 DB,涉及 **85 处调用点(生产 42 + 测试 43)**。做完才能进第 4 步删 alias 全套。

未在本轮开工的原因:半途停下会留下"读走 DB、写走 JSON"的中间态,比不开工更糟。这一步需要一个完整会话。

**未开工 · 第 4 步:删除 alias 机制** —— `provider_alias` 表、`aliasTTL`、`ResolveProviderAlias`、`checkAliasConstraints`、`cleanupExpiredAliases`、`commitProviderRenameLocked`、`rollbackFile`、禁止链式改名限制。改名缩成 `UPDATE provider SET name=? WHERE id=?`。

**未开工 · 第 5 步:Gemini 类型统一** —— `GeminiProvider`(string ID)并入 `Provider`(int64),连带删掉 `roundRobinOrderGemini` 与 `provider_delete.go`/`provider_rename.go` 里的 gemini 特判。这一步同时是 A3 阶段 1。

**未开工 · 顺带项** —— 回填完成后删除统计 SQL 里的 `custom:` 兼容 OR(`logservice.go:197`、`logdashboardbundle.go:497`);`blacklist_level_config` 双源真相收敛;app 设置分裂定归宿。
- [ ] **A3 主体** —— 阶段 1 是 Gemini 类型归一(建议随 A1 迁移一起做),阶段 2 抽 `dispatchWithFailover`。动手前必须先补 failover 主循环的表驱动测试(对着现在的 `proxyHandler` 写,确认通过后再在下面重构)。
- [ ] **A4/A5 拆包** —— 按域拆子包 + 小接口;删 `SetPricingService` 与 `LogService` 三个构造变体(它们是"同进程三套定价引擎并存、计费口径可分叉"的原因);Pi 的 `Supplier` 改名 `Provider`。
- [ ] **前端 F1-F3** —— Main/Index.vue 4592 行拆分、引入状态层、`Call.ByName` 收敛到 bindings。

顺序警告:A1 与 A3 都会碰 `provider_delete.go`、`provider_rename.go` 和转发循环,**不要在并行分支上同时做**,冲突代价高于串行成本。
