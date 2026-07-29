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

### A1 Provider 入库(已完成)

实施中发现原计划漏了两层依赖,都不是"删 alias"这一步本身能绕开的:写入侧必须先填 `provider_id`(第 3.5 步),黑名单也必须先改按 ID 定位(第 4.5 步)。原计划把"切换读写后即可删 alias"当成一步,实际是三步。

**已完成 · 第 1 步:`provider` 表 + 从 JSON 导入**(迁移 v2)

- 会被查询/排序的字段做列,其余约 30 个长尾配置进 `config_json`(`provider_config_json.go`)。
- 导入保留现有 int64 ID(不重编号 —— 紧接着要按这些 ID 关联日志行),`sort_order` 保留 JSON 原始顺序。
- 覆盖注册平台四个文件 + `providers/{toolId}.json`。
- 测试含一个字段覆盖检查:比对 `Provider` 与 `providerConfigPayload` 的 JSON 标签集合,将来给 Provider 加字段却忘记同步时会失败。已用临时探针验证该机制真能检出遗漏(37 字段 = 6 列 + 30 config_json + 1 个有意丢弃的 `piTemplate`)。

**已完成 · 第 2 步:日志表加 `provider_id` 并回填**(迁移 v3)

- 按 `(platform, source_id, provider 名)` 回填,匹配不上的留 NULL(早已删除的供应商)。
- `name` 列有意保留:它记录请求发生当时该供应商叫什么,这个历史事实本身有价值 —— `provider_alias` 机制其实是在勉强模拟这件事。
- 同一迁移归一化 `platform='custom:<toolId>'` 历史行。顺序是先归一 platform 再回填,否则旧格式行匹配不上。

**已完成 · 第 3 步:切换读写路径**

`LoadProviders` / `loadProvidersRaw` / `loadProvidersNoLock` / `loadProviderSnapshot` 改读 provider 表;`saveProvidersLocked` 与 `commitProviderRenameLocked` 改写 provider 表。JSON 不再被写入,迁移导入后原文件改名为 `*.migrated`。

删掉的补偿逻辑:原实现先写 JSON 再提交 DB 事务,失败时补偿回写文件。进程在两步之间崩溃会永久不一致,补偿本身还可能失败。现在写入是单事务。

顺带去掉 `LoadProviders` 的 mtime+size 缓存 —— 它本身有竞态:Windows 上 mtime 精度约 1-2 秒,同秒内"写→读→再写同长度内容"返回陈旧结果。

修掉三个实施中引入的缺陷:表示混用(provider 表用 `platform='custom'`+`source_id`,而 `providerDataScope.identityPlatform` 对自定义 CLI 是 `custom:toolId`);`SaveProvidersWithRename` 只写 name 列丢掉同批字段改动;Pi 同步放在事务提交之后导致失败时历史数据已落库。

**已完成 · 第 3.5 步:遥测写入 `provider_id`(计划漏项)**

做第 4 步时发现:迁移 v3 只回填了历史行,遥测的两条 INSERT 根本不写这一列,新日志行的 `provider_id` 恒为 NULL、仍只按 name 关联。alias 正是承接改名瞬间 in-flight 写入的机制,写入侧不填 ID 就删不掉它。

ID 为 0 时写 SQL NULL 而非 0:NULL 表示关联不到 provider 表(供应商已删除,或 Gemini 尚未并入),写 0 会造出指向不存在行的假外键值。

**已完成 · 第 4 步:删除 alias 全套**

先把 4 处按 name 筛选日志的读路径改为按 `provider_id` 匹配(方案 A:改名仍同步更新 name 列以保持展示一致),条件为 `(provider_id = ? OR (provider_id IS NULL AND provider = ?))`,解析不到 ID 时回退按 name。走参数绑定不拼字符串 —— 供应商名来自用户输入。

然后删除:`provider_alias` 表(迁移 v5 DROP)、`aliasTTL`、`providerAliasLookupEnabled`、`ResolveProviderAlias`、`checkAliasConstraints`、`checkNameNotOccupiedByAlias`、`cleanupExpiredAliases`、`refreshProviderAliasLookupEnabled`、`ensureProviderAliasTable`,以及从 alias 收集历史名字的删除逻辑。

随之解除两条限制:链式改名(原先 48 小时内禁止,因为 alias 是 name→name 映射,A→B→C 后用 A 查会得到已不存在的 B)、重用他人释放的旧名(原先会被 alias 静默归并)。

补上一处会丢数据的缺口:迁移 v3 原先只按当前名字回填,改名前写入的记录匹配不上而永久为 NULL,删除时按 ID 清理覆盖不到,留下孤儿数据。现在 v3 额外借 alias 表把这些行也关联上 —— v5 才删 alias,这是最后的机会。

**已完成 · 第 4.5 步:黑名单按 `provider_id` 定位(计划漏项)**

删 alias 的第二层前置,同样在实施第 4 步时才发现。`provider_blacklist` 原按 `(platform, provider_name)` 定位,改名瞬间 in-flight 的失败带旧名查不到已改名的行,于是插入第二条 —— 失败计数被拆成两份,拉黑阈值永远达不到。

迁移 v4 加 `provider_id` 与 `source_id` 并回填;新增 `blacklistTarget`,`IsBlacklistedFor` / `RecordSuccessFor` / `RecordFailureFor` 为新入口(按名字的旧方法保留为兼容委托,UI 手动解除拉黑只有名字可用);`providerrelay.go` 19 处调用点改为传 ID。

**已完成 · 第 5 步:Gemini 类型统一**(作为 A3 阶段 1 一并做掉,详见下面的 A3 小节)

原计划量的范围是"28 处 Go 引用、20 个 Wails 暴露方法、6 处前端调用,还要把 ID
从 string 改成 int64(连带改前端)"。实际做法避开了后半段:存储层已在迁移 v6
并入 provider 表,原 string ID 存进 `config_json` 的 `gemini.legacyId`,
所以对外仍暴露 `GeminiProvider` 与 string ID —— Wails 绑定与前端零改动。

`GeminiProvider` 那几个真实差异字段(`EnvConfig`、`SettingsConfig`、`WebsiteURL`、
`APIKeyURL`、`Category`、`PartnerPromotionKey`)进了 `config_json`,
`GeminiService` 仍用这个类型管 CRUD 与 CLI 配置写入;
只有转发层改用统一 `Provider`。

**已完成 · 顺带项:`blacklist_level_config` 双源真相收敛**(迁移 v7)

配置原先有两处存储:JSON 文件 `blacklist-config.json` 存全部字段,而 UI 改动的
三个字段只写 `app_settings` 的独立键(`blacklist_level_enabled`、
`blacklist_failure_threshold`、`blacklist_duration_minutes`)。读取时"先读 JSON、
再用独立键覆盖对应字段"打补丁(原注释自称【关键修复】)。反方向就丢:
`SaveBlacklistLevelConfig` 只写 JSON,存进去的开关与阈值会被下次读取时的旧键覆盖。

现在单一来源是 `app_settings` 的 `blacklist_level_config` 行。`GetBlacklistSettings`
改读配置里的 `failureThreshold` / `fallbackDurationMinutes`;
`UpdateBlacklistSettings` 与 `SetLevelBlacklistEnabled` 改为按字段更新同一行;
迁移以 JSON 为基底、三个重叠字段取独立键现值(UI 只写那里,是用户最后一次操作的结果),
折叠后删除独立键并把 JSON 文件改名 `.migrated`。

按字段更新用读-改-写(在 `BEGIN IMMEDIATE` 事务内),不用 SQL 的 `json_set`:
后者遇到非法 JSON 返回 NULL,会把整份配置静默清空。

**这一步纠正了实施中先做错的一版**:曾把 `enable_blacklist`(拉黑总开关)
当成 `enableLevelBlacklist`(等级拉黑开关)的同一概念互相同步。两者是不同语义,
`ShouldUseFixedMode` 分别读取再组合:总开关决定是否拉黑,等级开关决定用等级模式
还是固定模式。同步它们会让"关掉总开关"顺带关掉等级拉黑配置。
`TestBlacklistSwitchesAreIndependent` 锁死这一点。

顺带删掉三处两处存储时代的补丁:`recordFailureFor` 与 `GetRetryConfig` 里
"再查一次数据库、失败才回落到 levelConfig"的双读,以及 `database.go` 的
`ensureBlacklistTables`(零调用死代码,且会重新 seed 刚被 v7 删掉的键)。

**顺带项:app 设置分裂 —— 复核后决定不做**

design-review 举的例子是"`LogRetentionDays` 在 app.json,
`blacklist_duration_minutes` 在 `app_settings` 表"。后者已被迁移 v7 折叠删除,
v7 之后表里只剩 `enable_blacklist` 与 `blacklist_level_config` 两行。

复核两份清单后:`app.json` 的 30 个字段是应用偏好(UI、预算、自启、更新、通知、
轮询、全局代理、日志保留),`app_settings` 表存的是黑名单运行时配置。
这不是"同一类东西存两处",是两个域各在一处。把 30 个字段迁进 SQLite
属于高风险用户数据迁移(任一字段漏迁就是静默改配置),收益只有命名统一。

同段 design-review 提的"app.json 裸 `os.WriteFile` 半写即损坏"是真缺陷,
但已在 A7 修掉(`atomicWriteFile`)。`appsettings.go:107` 剩的那处裸写
只在目标文件不存在时执行,且读回校验 + 失败回滚,半写不会损坏既有数据。

### A3 转发层收敛(已完成)

**前置:failover 主循环基线测试(21 个用例)**

`relay_failover*_test.go`:httptest 假上游 + 真实服务构造 + gin router,按命中次数断言。
覆盖降级模式单次失败即切换、固定模式重试到阈值再切换、Level 升序、轮询交替与关闭时的顺序调度、
模型白名单过滤不触达上游、已拉黑跳过、`ErrClientRequestRejected` 直接 400、
响应已提交后停止但仍记账,以及 gemini 与 customCli 两套 handler 的同类契约。

**基线抓出两个真实缺陷(已修)**

1. **固定与等级两种模式的首次失败都不比阈值** —— 都在"首次失败"分支插入
   `failure_count=1` 后直接 return。UI 允许阈值 1,但阈值 1 在两条路径上都永远达不到:
   每次请求重试一次就切换,坏 provider 永远进不了黑名单。
   等级模式的修法是插入计数为 0 的行后交给 `applyLevelFailureLocked` 统一累加判定
   ——它的未达阈值分支用 SQL 侧自增,插入时预置 1 会变成 2。
2. **customCli 的 `errResponseCommitted` 分支不记失败** —— B7 修过的 bug 的第三个拷贝漏改。

**阶段 1:Gemini 转发循环并入统一类型**

`forwardGeminiRequest` / `forwardGeminiAttempt` 改收 `Provider`;
删除 `roundRobinOrderGemini`(与通用版逐行相同,只差类型与 key 前缀)、
`blacklistTargetForGemini`;`recordAttempt` 与 `recordGeminiAttempt` 合并到
`appendAttempt`(唯一差别是上游协议固定为 Gemini 原生);
`ProviderRelayService` 去掉 `geminiService` 字段(转发不再需要它,构造函数少一个参数)。

Gemini 有意不进平台注册表:注册表的 `ProviderFile` 驱动迁移 v2 的导入,
而 Gemini 的导入由 v6 单独负责(要处理 string ID → int64 主键映射,
且它的 JSON 是裸数组不是 envelope)。加进去会让 v2 先把文件改名 `.migrated`,
v6 再也读不到,legacyId 全丢。

**阶段 2:抽出 `dispatchWithFailover`**

新增 `relay_dispatch.go` + `_fixed.go` + `_degrade.go`。三套 handler 各自那份
Level 分组、轮询、重试、拉黑记账、降级判断收敛为一处;
留在 handler 的是真实差异:provider 过滤条件、模型映射方式、转发实现、最终错误响应形状。
`providerrelay.go` 从 2413 行降到 1738 行。

失败分类收敛到 `classifyDispatchFailure`,返回"是否记账 / 是否停止 / 是否跳过"。
两处语义在合并时必须显式定死:

- **行为变更**:客户端断开且响应未提交时统一停止。降级模式原先会继续尝试下一个
  provider(固定模式停止)。客户端已经走了,继续转发没有接收方,只是白烧上游配额。
- **新增 `errSkipProvider`**:模型映射失败是配置问题,跳过该 provider 但不记失败
  ——记成失败会让配置问题被当作 provider 不可靠,最终把它拉黑。原三套 handler
  在这里都是直接 `continue`,语义要保留。

顺带修掉:customCli 原先整个缺 `ErrClientRequestRejected` 分支(三套里只有它缺),
跨协议转换被拒会被当成 provider 失败并逐个降级。走统一调度后自动获得正确行为。

轮询会重排同 Level 的顺序,切换通知必须用重排后的切片找"下一个 provider",
用原始分组会算错。

### 定价引擎单一化(已完成,原属 A4/A5)

`SetPricingService` 那种后置注入造出一段"已构造未注入"的窗口态,
逼着下游各留一套回退:`LogService` 三个构造变体 + 一个内嵌 `pricing` 字段、
telemetry 一个 `legacyPricing` 字段。回退用 `modelpricing.DefaultService()`,
产出的 `pricing_source` 是 `embedded-v1`,与正规路径口径不同
——同一批日志里可能混着两种计费来源。

依赖顺序上 `pricingService` 本来就在 relay 与 LogService 之前构造好(main.go:129),
所以改成构造参数即可:删 `SetPricingService`、删两个 `LogService` 变体与内嵌字段、
删 telemetry 的 legacy 分支。缺失时返回空结果(不计成本),而不是换一套引擎算。

删掉后置注入后编译器强制在构造时传入 pricing,这比测试更强。
`pricing_single_engine_test.go` 另外锁住"缺失时不回退到另一套引擎"。

`ProviderRelayService` 从未注册进 Wails,所以删它的导出方法对前端零影响。

M1(`ReqeustLog` → `RequestLog`)在之前的改动里已经完成,只有文档还列为待办。

### Pi `Supplier` 改名 `Provider` —— 复核后决定不做

Pi 域里"provider"已经有三个含义:`models.json.providers.<id>` 是**平台**、
`PiGatewayProvider` 是写进 `pi.json` 的网关配置、provider 表里 `platform='pi'` 的行。
`Supplier` 指的是平台下挂的网关供应商(`PiRuntimeSupplier` 带 `PlatformID`
与 provider 表的 int64 ID),是第四个概念。改名会让"provider"再多一个含义,
而不是消除歧义。

前端也把两者当独立概念:`components/Pi/Index.vue` 里 platform 与 supplier
各有编辑器、各有保存/删除事件。改名要动 13 个 Go 文件、导出类型、
Wails 绑定和 16 个前端文件,零行为变化,换来的是更差的命名。

### A4/A5 拆包 —— 范围已量,未开工

`services` 是 128 个非测试文件、约 4 万行。拆包的真实成本在类型归属上:
以最可能干净的 pricing 为例,它对包内其余部分只有两个依赖
(`piSettings.resolveModelPricing`、`appSettings.GetGlobalProxyConfig`),
接缝很干净;但 `resolveModelPricing` 返回 `PiModelCost` —— 一个 Pi 域的类型。
pricing 独立成包后,要么把 Pi 的类型搬进 pricing(域归属错),
要么 pricing 导入 pi 包,而 pi 需要导入 pricing 拿接口,成环。

正解是先加一层共享类型包,再逐域搬。这是多轮工作,动手前应先画完整的
类型归属图,否则会在每个边界上重复遇到同一个环。

### 前端 F3:`Call.ByName` 收敛到 bindings(已完成)

`Call.ByName` 靠字符串拼服务名与方法名,Go 侧签名变化时编译期发现不了,
只在运行时报错;bindings 是生成的类型化函数,用数字方法 ID 且带参数与返回类型。

已转换 117 处中的 97 处,覆盖全部服务层与组件层:
`settings.ts`、`configImport.ts`、`mcp.ts`、`logs.ts`、`skill.ts`、
`customCliService.ts`、`cliConfig.ts`、`appSettings.ts`、`frontendPreferences.ts`、
`geminiSettings.ts`、`blacklist.ts`、`version.ts`、`claudeSettings.ts`、
`useUpdateStore.ts`,以及 `Tray/`、`General/`、`Console/`、`NetworkWslSettings.vue`、
`PiModelConfigEditor.vue`、`ModelWhitelistEditor.vue`。
顺带清掉各文件里已无用的服务名常量(`SERVICE_PATH`、`SERVICE_PREFIX` 等)。

`Main/Index.vue` 剩的 20 处留到 F1 拆分时一起做——那个文件本来就要重写。

**`claudeSettings.ts` 的动态分发器**是编译期检查的盲区:原实现是
platform → 服务名字符串,再拼 `${service}.${method}` 交给 `Call.ByName`。
改成"平台 → 绑定函数"的映射表后,漏平台被 `Record<Platform, ...>` 抓,
方法签名变化被绑定函数的类型抓。已实测:临时删掉 reasonix 条目立刻报 TS 错误。

绑定路径两个坑:根包服务(`AppService`、`VersionService`)的绑定在
`bindings/codeswitch/` 下而不是 `bindings/main/`;组件在 `components/X/` 下时
相对路径是 `../../../bindings`。

**本地类型多数比生成类型窄**(`platform` 是字面量联合、生成的是 `string`),
UI 的 switch 分支与 Record 键依赖这个收窄,所以保留本地类型、在返回边界断言,
而不是直接改用生成类型。

**转换过程抓出三个与 Go 侧脱节的死字段**——它们原先靠 `Call.ByName` 的 `any`
返回值加 `as` 断言蒙过编译器,实际永远是 `undefined`:
`logs.ts` 的 `RecordStorageInfo.health_check_count`、
`RecordCleanupResult.deleted_health_checks`、
`configImport.ts` 的 `imported_health_checks`。
根因是 `health_check_history` 全套在第 0 批已删除。已确认无人读取后删掉。

### 前端 F1/F2:`Main/Index.vue` 拆分(已完成)

4568 行降到 2465 行(模板 + 装配 + 样式),脚本逻辑按域拆进 `components/Main/`:

| 文件 | 职责 |
|---|---|
| `platformTabs.ts` | Tab 定义、排序持久化读取 |
| `state.ts` | 状态层:`createMainState()` 工厂,持有 Tab、卡片、代理开关、直连应用、Gemini 缓存、自定义 CLI 工具、轮询门控;组件 setup 时创建,不做模块级单例(生命周期与页面一致) |
| `utils.ts` | 纯函数:格式化、排序、序列化 |
| `platformAdapters.ts` | 平台适配器:卡片加载/持久化、代理开关、直连应用、复制,按 Tab 查表;原先散落六处的 if/else 平台分支收敛于此 |
| `composables/` | 9 个按域 composable:cards、proxy、directApply、stats、blacklist、lastUsed、usageTooltip、platformOrderMenu、vendorModal、customCliTools |

顺带完成:

- **剩余 20 处 `Call.ByName` 清零**。direct apply 走 `claudeSettings.ts` 的平台映射表扩展
  (`fetchDirectAppliedProviderID` / `applySingleProvider`),gemini 走 `geminiSettings.ts`
  新增封装,黑名单手动解禁走 `blacklist.ts` 新增封装,连通性测试走 bindings
  `TestProviderManual`。
- **删除一个必然失败的死调用**:`ProviderRelayService.GetAllLastUsedProviders`。
  该服务从未注册进 Wails(全历史搜索确认),调用永远抛错被 catch 吞掉;
  且 `lastUsed` 是后端内存态,启动时本为空。删除后行为不变,
  「正在使用」标记仍靠 `provider:switched` / `provider:blacklisted` 事件填充。
- **删除整套不可达的 pi 分支**:pi 自 f278276(Pi 独立页面上线)起不在首页
  `defaultTabs` 里,`activeTab`/`modalState.tabId` 永远取不到 `'pi'`,
  但模板与脚本里留着完整的 Pi 表单、models.json 预览、请求头模板、pi 代理开关分支
  (含 3 处 pi 的 `Call.ByName`,也是死调用)。连带删除零引用组件
  `RequestHeaderTemplateEditor.vue`、`PiModelsJsonPreview.vue`。
  遗留观察:Go 侧 `PiSettingsService.PreviewModelsJSON` 因此不再被前端调用,未动。
- **修复 others Tab 复制供应商**:原实现把 `'others'` 当 kind 传给
  `DuplicateProvider`,后端解析不了必然失败;改为 `custom:{toolId}`。
- **删除死代码**:`handleUnblock` 别名、`manualUnblock` 导入、`appVersion`/
  `loadAppVersion`/`compareVersions`/`normalizeVersion`、`releaseApiUrl`、`mcpIcon`、
  `goToLogs`/`goToMcp`/`goToSkill`、`providerStatsLoading`(只写不读)、
  挂在 `window` 上的定时器句柄 hack(改为 composable 内局部变量 + 生命周期钩子)。

行为保持核对过的点:轮询门控(`useActivePolling` 依赖 `KeepAlive` 的
`onActivated`,已确认 `App.vue` 包了 `keep-alive`)、黑名单三组定时器语义、
Gemini 卡片 ID(300+下标)与缓存映射、改名走 `RenameProvider`、
保存失败不关弹窗并回滚新卡片。验证:`npm run build`(vue-tsc + vite)与
`vitest run`(16 文件 62 用例)全绿。

### A4/A5 拆包 · 类型归属图与阶段 1(infra 已拆出)

**类型归属图**(`scripts/depmap` 一次性 go/parser 工具,统计 117 个非测试文件的跨文件符号引用按域聚合):

三组共享工具制造了大部分假耦合,先归位它们才能谈域拆分:

1. header 工具(`canonicalizeHeaderMap`、`setHeader`、`applyUserAgentIdentity` 等,声明在 relay 域)被 provider/pi/relay 三方使用 → 应独立为共享 http 工具包。
2. `ProxyConfig` + `NewHTTPClientWithProxy` + `describeProxyTransportError`(声明在 networkservice)被 app/pricing/provider/relay 使用 → 同上。
3. `RequestLog` 声明在 relay 域(telemetry),实为 logging 域核心类型(logging→relay 18 处引用里 17 处是它)→ 归 logging。

两个环的解法(印证原先的判断):

- **pricing↔pi**:pricing→pi 是 `PiSettingsService` + `PiModelCost`;pi→pricing 只是 4 个定价来源常量。成本类型(`PiModelCost` 及分段结构)归 pricing,pi 单向依赖 pricing;`resolveModelPricing` 在 pricing 侧收成小接口,由 pi 实现注入。
- **provider↔pi**:`Provider` 结构体内嵌 `PiModelEntry`/`PiModelOverride`(存储 schema 的一部分),pi 又引用 `Provider` 46 处。Pi 模型 schema 类型与 sync/validate 逻辑归 provider,pi 单向依赖 provider。

分层(自底向上):infra(已拆)→ dbcore(`dbExec`/事务原语,migrations 除外)→ httpx(header + proxy)→ pricing → provider → blacklist / logging → pi → relay(顶层,直接 import 各域)→ migrations(顶层,天然触达所有域的 schema)。

**Wails 约束**:注册进 Wails 的服务类型不能挪出 services 包——bindings 路径编码了包名(`frontend/bindings/codeswitch/services/`),移动会破坏全部前端导入。最终形态是 services 只留 Wails 门面 + 装配,实现逐步下沉到 `internal/` 子包。

**阶段 1(已完成):`internal/infra`**

- `git mv` 9 个实现文件 + 2 个测试文件:applog、atomic_write(3 个平台文件)、fileutils、userhome、config_backup、cmd_windows/other;符号导出改名(`atomicWriteFile`→`AtomicWriteFile` 等)。
- applog 的 `consoleLogSink` 接口改为 `ConsoleLogFunc` 函数值:原接口方法 `addLog` 未导出,跨包后 `ConsoleService` 无法实现它;函数值同样避免 infra 反向依赖控制台实现,`ConsoleService` 构造时传 `cs.addLog`。
- `services/infra_bridge.go` 用别名转发(`var atomicWriteFile = infra.AtomicWriteFile` 等),包内约 200 处调用点零改动;各域拆出时改为直接 import infra,全部迁完后删除该文件。
- 复核后不搬的两个:`env_file_edit.go` 带着 gemini 偏好键序(`geminiPreferredEnvKeyOrder`),归 clisettings 域;`servicestore.go` 是 Wails 服务(SuiStore)且持有 `hotkeyDBFileName`,留在 services。
- 验证:`go build .`、`go vet ./services ./internal/infra`、`go test ./internal/infra` 全量、`go test ./services` 按 AppLog/Console/Atomic/SettingsProxyLifecycle/Blacklist/Gemini 过滤,全部通过(GOARCH=amd64)。

顺带发现(未处理):`scripts/` 根目录有多个互相冲突的 `package main` 调试文件(`debug-blacklist.go`、`test-concurrent-insert*.go` 等),`go build ./scripts` 一直是坏的;属于历史调试残留,可另行清理。

**阶段 2(已完成):`internal/dbcore`**

- `git mv` dbwrite.go / database_dsn.go 及各自测试文件;导出改名(`dbExec`→`Exec`、`dbTxExecutor`→`TxExecutor`、`CloseDatabase`→`Close` 等);`appDatabaseFilename` 从 logservice.go 归位到 dbcore,DSN 构造复用同一常量。
- `InitDatabase` 编排与全部 migrations 留在 services:migrations 天然触达所有域的 schema,属于顶层。
- `services/dbcore_bridge.go`:类型别名(`dbStatement`/`dbTxExecutor`)+ 函数别名;`CloseDatabase = dbcore.Close` 保持 main.go 调用不变。

**阶段 3(已完成):`internal/httpx` 之 proxy 客户端**

- proxystate.go 按域切开:`ProxyConfig` + 客户端构造 + 协议错配识别/自动回退 + `TestProxyConfig` 约 400 行搬到 httpx;`ProxyState`(CLI 代理状态文件)与 AppSettings 归一化留在 services。
- **实测发现 Wails 绑定约束的第二层**:`ProxyConfig`/`ProxyTestResult` 出现在 `AppSettingsService` 三个导出方法的签名里。若在 services 用类型别名指向 internal 包,`wails3 generate bindings` 会把模型生成到 `frontend/bindings/codeswitch/internal/httpx/` 并从 services 的 models.ts 删除(已实测确认后回退)。结论:**门面类型必须以具体结构体留在 services 包**,桥用 Go 结构体显式转换衔接(两边字段同型,转换零成本)。重新生成 bindings 后模型回到原位、行号未变,前端零改动。
- `doProxyAwareRequest` 里的 `fmt.Printf("[Proxy] ...")` 顺带迁到 slog(对齐 M7)。

**阶段 3b(已完成):`internal/httpx` 之 header 纯工具**

- upstream_policy.go 的纯函数层搬到 httpx/headers.go:`canonicalizeHeaderMap`、`setHeader`/`removeHeader`/`headerValue`、`validateAdditionalHeader`/`validateHeaderNameAndValue`、`blockedUpstreamHeaders`、大小写特例表、`mergeCommaSeparatedHeader`。
- 留在 services 的部分(`buildUpstreamHeaders`、`applyUserAgentIdentity`/`Policy`、`userAgentPresets`)操作 `Provider`/`ProviderRequestIdentity`,归 provider 域,后续随域走;endpoint 工具(`splitEndpointQuery` 等)依赖 relay 的默认端点常量,留给 relay 阶段。
- 验证:`go build .` + vet + internal 全量测试 + services 按 Pi/Provider/Relay/Codex 等过滤测试 + bindings 重新生成(仅注释同步 diff)+ 前端构建,全部通过。

**路线修正(实施后的重要结论)**:Wails 模型约束重塑了拆包优先级。注册服务导出签名里的类型都必须以具体结构体留在 services——pricing 的十来个 `Pricing*` 类型、provider 的 `Provider`、pi 的 `PiModel*` 全在此列,「`PiModelCost` 归 pricing」的原设想因此作废。整域搬移这些"模型重"的域,每个类型都要一套转换样板,收益低于成本,复核后放弃(pricing 门面方法与目录记录深度耦合,只提取引擎的切口也处处要转换)。**高价值目标是 relay 域**:`ProviderRelayService` 从未注册进 Wails、几乎没有模型类型,且是包里最大的一块(providerrelay + relay_* + codex_* + protocol_*)。另外依赖图里 pricing→provider 的 `Provider` 引用是字段名误报(`PricingBuiltinRow.Provider` JSON 字段),pricing 的真实外部依赖只有两个单方法注入点。

**收尾批次(已完成):P1/P3/P5 + relay 前置清理 + 死代码**

- **P3**:`applyProviderRequestBodyPolicyForModel` 改 gjson/sjson 定点修改。原实现整体 decode/encode 请求体（数百 KB 上下文每 attempt 全量走一遍，还把顶层键字典序重排）；定点改只触碰 metadata 子树，其余字节原样保留（新增保真度测试锁死）。原方案里的"重试循环外缓存"复核后不做：Preserve 模式（默认）零开销直返，定点改后剩余成本已极小，而缓存键要含协议转换后的 body,复杂度不划算。
- **P5**:新增 `relay_proxy_snapshot.go`,`dispatchWithFailover` 入口读一次全局代理配置挂到 gin 上下文,所有 attempt(含 Codex 续写轮次,`sendNativeCodexResponsesRequest` 加了 ctx 参数)共享,消掉每 attempt 一次的 `os.Stat`。顺带删掉 degrade 分支里重复查拉黑模式的 `isRoundRobinEnabled`(调度入口已判定过,直接读轮询开关)。
- **P1**:新增 `blacklist_cache.go`。缓存值是目标的 `blacklisted_until`(nil=无拉黑行),到期判断读取时做时间比较所以**过期无需失效**;五个写路径(`recordSuccessFor`/`recordFailureFor`/`ManualUnblockAndReset`/`ManualResetLevel`/`AutoRecoverExpired`)defer 整表失效。缓存键与 `locator()` 语义一一对应(有 ID 按 ID,否则按名);改名不改 ID 天然安全,删除的 provider 不再进候选列表、ID 不复用。4 个新测试锁失效契约。
- **relay 前置清理**:`RequestLog` 类型移入 `services/requestlog.go`(logging 域,Wails 模型,relay 拆出后反向引用);`boolToInt`/`nullableProviderID` 归 `dbcore`(`sqlvalues.go`);endpoint 工具(`splitEndpointQuery` 等 + `DefaultOpenAIChatEndpoint`)归 `httpx/endpoints.go`。
- **死代码**:删除 `PiSettingsService.PreviewModelsJSON` 全套(方法 + 两个请求/结果类型 + 两个测试;Pi 页用的是 `PreviewPlatformModeChange`,该方法自 Main 页 pi Tab 移除后无调用者),bindings 重新生成后 240 方法/113 模型(`PiModelsPreview*` 与不再出现在签名里的 `PiConfigDiagnostic` 模型移除,前端零引用,构建通过)。清理 `scripts/` 下 28 个互相冲突的一次性调试文件和两个误入库的 SQLite 数据文件(其中两个文件含密钥样式的硬编码字符串),`go build ./...` 全模块恢复可用;保留 `generate-latest-json.py`、`publish_release.sh`、`depmap/`。

### A4/A5 · relay 域整段搬移(已完成)

`internal/relay` 包成立:28 个实现文件 + 22 个测试文件(git mv 保留历史),单向 import services 与 infra/dbcore/httpx。`services` 包不再包含转发循环、协议矩阵、Codex 续写/bridge。

**阶段 A(接缝)**:
- 生产侧反向依赖三处解除:客户端拒绝错误契约移入 `services/client_reject_error.go`;`ReplaceModelInRequestBody` 移入 `body_filter.go`;pi_relay.go 的两个 `ProviderRelayService` 方法剪切到 `relay_pi_handlers.go` 随搬移集走,pi 助手留下并导出。
- 约 30 个接缝符号在 services 导出(BuildUpstreamHeaders、ApplyProviderRequestBodyPolicy、Pi 入口、BlacklistTarget 等)。

**阶段 B(搬移)**:
- 自写 AST 定位 + 字节偏移改写工具(保注释与原始字节):对搬移文件的标识符按声明归属加 `services.` 前缀(跳过 selector 右侧与结构体字面量键),bridge 别名直接改写为 infra/dbcore/httpx 直连,自动插入 import。28 个文件 243 处替换,**首次编译零错误**。main.go 改一处 import。

**阶段 C(测试)**:
- 18+1(pi_relay_test)个测试文件随包搬移并同法改写;`testsupport_test.go` 复制 services 侧测试基建(HOME 隔离、独立 app.db、迁移建 schema),差异仅路径函数走 infra、迁移走导出的 `RunMigrations(On)`。
- 两个混合测试文件按域拆分(performance_hotpath、pricing_single_engine);`TestDefaultConnectivityAuthType` 移回 services;`failureCountFor` 在 services 侧保留副本;`BlacklistTarget` 加 `Platform()`/`SourceID()` 只读访问器。

**实施中发现并修正:Wails 绑定面泄漏**。阶段 A 曾把 BlacklistService 三个记账方法与 `PricingService.newRequestSnapshot` 导出为方法,重新生成 bindings 后方法数 +4、模型 +2(注册服务的导出方法会全部进绑定面)。改为**包级导出函数接缝**(`services/relay_seams.go`:`BlacklistedFor`/`RecordBlacklistSuccess`/`RecordBlacklistFailure`/`NewRequestPricingSnapshot`),方法回退非导出——Wails 只绑定方法不绑定包级函数,绑定面回到基线(240 方法/113 模型,实测确认)。这条与「门面类型必须留在 services」并列为拆包第三条 Wails 约束。

**M2 后半顺带完成**:`RelayAttemptLog` 行类型与 `RequestLogInsertStatement`/`RelayAttemptInsertStatement` 两个语句构造归 `services/requestlog_repo.go`(logging 域),attempt 语句签名解耦 telemetry(收标量参数);relay 遥测组装后经 `dbcore.ExecStatements` 单事务提交。
桥别名顺带修剪(`dbExecStatements`/`defaultOpenAIChatEndpoint`/`canonicalUpstreamHeaderName` 已无使用者)。

**验证**:`go build ./...` 全模块、vet、`go test ./internal/relay` 全量(全部 failover/codex/protocol/telemetry 基线)、services 广谱过滤测试、bindings 重生成(仅注释同步与 PreviewModelsJSON 移除的既有 diff)、前端构建,全部通过。

### 剩余
(无 —— 重构计划全部项目已完成或已记录为「复核后不做」。)

顺序警告:A1 与 A3 都会碰 `provider_delete.go`、`provider_rename.go` 和转发循环,**不要在并行分支上同时做**,冲突代价高于串行成本。
