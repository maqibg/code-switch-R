# 设计评审:全项目不合理设计分析

日期:2026-07-27(第二轮复核同日更新)
方法:5 路并行扫描(架构耦合 / 存储设计 / 可维护性 / 正确性风险 / 性能+前端),交叉验证后汇总。P0 级结论以及第二轮复核涉及的条目已逐条读原文证实。
前提:大预算重构,可破坏性改动、一次性迁移;无硬禁区,但协议矩阵、Codex 续写/bridge、Pi 托管冲突检测、CLI 手术式写入按高风险区做风险加权。

评级说明:
- 收益 = 修复后消除的用户可见故障 / 长期维护成本
- 成本 = 预估改动规模
- 风险 = 改坏现有行为的可能性(高风险区加权)

读这份报告的顺序建议:先看「第零部分:根因收敛」判断投入方向,再按「重构路线」执行,后面的分维度清单当作查证明细用。

---

## 第零部分:根因收敛

后面列的约 20 个问题不是 20 个独立缺陷,而是 4 个根决策派生出来的。这决定了重构的投入顺序——修根决策比逐个修症状的杠杆率高一个量级。

### 根决策 1:Provider 主数据放 JSON、日志放 DB、两者按 name 字符串关联

派生症状:`provider_alias` 表 + 48h TTL、禁止链式改名、文件-DB 补偿 Saga、B3 首次改名失败、跨存储崩溃后永久不一致、`platform='custom:<id>'` 兼容 OR 进每条统计 SQL。

6 个症状,1 个修复点(A1)。

### 根决策 2:用应用层队列替代数据库层并发控制

派生症状:双队列 + 事务旁路(单写者前提破产)、B4 busy_timeout 修在了错的位置、settings 双行更新退化成 Saga、遥测串行等待 100ms×k、request_log/relay_attempt 跨批次成孤儿行、B8 的读-改-写竞态、B9.4/B9.5/B9.6 队列关闭与批失败。

8 个症状,1 个修复点(A2)。**B8 竞态的根因也在这里**:写入必须过队列,所以用不了 `SET failure_count = failure_count + 1` 之外的事务手段,被迫在 Go 侧读-改-写。

### 根决策 3:平台差异用复制代码而非抽象表达

派生症状:三套转发循环、四份 settings 服务、GeminiProvider 平行类型、`providerFilePath` 两份已漂移的 switch、新增平台改 12 处、gemini 分支吞错误、custom 分支未门控日志。

7 个症状,1 个修复点(A3 + A6)。

### 根决策 4:失败时"继续执行"而非"拒绝"

派生症状:B5 解析失败用空 map 继续、B6 `.env` 重写丢内容、B7 流中断记成功、B9.8 Codex 历史丢失静默降级。

这一条和前三条性质不同:**前三条是架构债,这条是价值观债**,而且它直接违反了项目自己定的红线「不添加 mock 成功、静默 fallback 或吞异常;失败必须通过错误、日志或测试暴露」。四处代码都在违反这条规则,其中 B5、B7 两处还写了注释为自己辩护(见下文引用)。

4 个症状,没有单一修复点,但每处改动都很小——**因此排进第 0 批**。

### 顺序结论

按根因看,**根决策 2(队列)应排在根决策 1(存储统一)之前**,与初版路线相反。理由:队列是存储统一的前置条件——Provider 入库需要可靠事务,而当前队列恰恰让事务用不了。先去队列,根决策 1 的 8 个症状里有 5 个会自动消失或大幅简化。

---

## 第一部分:确定性 Bug(不是"设计争议",是会坏的)

### B1. Windows 更新脚本给只读变量 `$pid` 赋值,便携版更新必然失败 【P0】

- 位置:`services/updateservice.go:939`(`$pid = %d`),脚本头部 `$ErrorActionPreference = 'Stop'`。
- `$PID` 是 PowerShell 只读自动变量,赋值即抛错,Stop 模式下脚本第一行就终止。用户点"重启并更新"→ 应用已 Quit,脚本什么都没替换 → 重开还是旧版,且 `pending_apply.json` 残留,下次启动恢复 ready 状态形成死循环。
- 修复:改名为 `$targetPid` 等非保留名。**一行修复,收益极高。**
- 收益:高 | 成本:极低 | 风险:低

### B2. `piGatewaySync` 回调生产代码从未装配,Pi 网关同步链路部分断裂 【P0】

- 位置:`services/providerservice.go:199`(`setPiGatewaySync`,非导出),全仓库只有测试文件调用(已 grep 复核),main.go 无装配。
- `saveProvidersLocked` 保存 pi 平台时会调用该回调同步 `~/.pi/agent/models.json`,但生产中恒为 nil。走 `DeepLinkService`(`deeplinkservice.go:208` 支持 pi)或任何直接 `SaveProviders("pi", ...)` 的入口时,models.json 不同步,Pi CLI 侧配置静默漂移。测试全部注入回调通过,恰好掩盖了生产链路为空。
- 修复:main.go 显式装配(方法导出化),或构造参数强制注入。
- 收益:高 | 成本:低 | 风险:中(Pi 托管属高风险区,装配后要验证双写路径不冲突)

### B3. 全新安装首次改名 Provider 必失败:`health_check_history` 表不存在 【P0】

- 位置:`provider_rename.go:310` UPDATE `health_check_history`,但该表只在 `provider_delete.go:94`(删除路径)惰性创建,`InitDatabase` 不建;`RenameProvider` 只 ensure 了 request_log(`provider_rename.go:72`)。已 grep 复核。
- 全新安装、从未删除过 provider 的用户,首次改名时事务内 UPDATE 报 `no such table`,整个 rename 失败。
- 更深一层:健康检查功能 v2.6.52 已移除,这张表是"永远为空的表",rename/delete/统计/导入却都还在维护它(见 M4)。
- 修复:短期把建表挪进 InitDatabase;长期直接删掉全部 health_check 相关 DDL/DML。
- 收益:高 | 成本:低 | 风险:低

### B4. `busy_timeout` 只对连接池中一条连接生效 【P0】

- 位置:`services/database.go:46` 用 `db.Exec("PRAGMA busy_timeout = 30000")`。`database/sql` 是连接池,该 PRAGMA 只作用于当时借到的那条连接;DSN(`database.go:28`)里没有。WAL 持久化在文件里没问题,busy_timeout 是 per-connection 的。
- 后果:并发写时新连接没有等待时间,直接 `database is locked`——这正是双写队列声称要解决的问题,而"修复"本身是坏的。
- 修复:DSN 加 `_pragma=busy_timeout(30000)`(modernc 驱动支持)。一行改动。
- 收益:高 | 成本:极低 | 风险:低

### B5. Codex `EnableProxy` 重复启用 + TOML 解析失败 = 清空用户整个 config.toml 【P0】

- 位置:`services/codexsettings.go:98-102`。解析失败时 `raw = make(map[string]any)` 继续写回;备份只在首次启用(`:93` 的 `if !stateExists`)时做。
- 失败场景:已启用代理 → 用户手改 config.toml 引入语法错误 → 重启应用触发幂等重入 → 不再备份、解析失败、用空 map 覆盖 → mcp_servers、profiles 全部丢失,旧备份不含用户后来的编辑。
- **代码注释在为不存在的安全网背书**(第二轮复核重点):

  ```go
  if err := toml.Unmarshal(content, &raw); err != nil {
      // TOML 解析失败，使用空配置继续（备份已保存）
      fmt.Printf("[警告] config.toml 格式无效，已备份到 %s，将使用空配置: %v\n", backupPath, err)
      raw = make(map[string]any)
  }
  ```

  "备份已保存"是错的——备份在 `:93` 的 `if !stateExists` 分支里,重复启用路径根本没执行,而日志还会打印一个实际未写入的 `backupPath`。这比单纯的 bug 更危险:它让后续维护者读代码时误判风险已被处理。
- 附带:`toml.Unmarshal → Marshal` 往返丢弃所有注释和键顺序;`DisableProxy`(`:220-222`)对**同样的解析失败选择直接返回错误**,导致代理占位配置无法移除,应用退出后 Codex CLI 全部请求打向已停止的本地端口。同一个文件、同一种输入,一边"继续并覆盖"一边"拒绝"——这不是设计取舍,是两次不同时间的决定没对齐。
- 修复:解析失败必须拒绝启用并报错;每次写入前做时间戳备份;改为文本级手术式编辑。
- 收益:高(数据丢失级) | 成本:中 | 风险:高(高风险区,需补测试后再动,见 M6)

### B6. Gemini `.env` 重写丢弃注释、`export` 行、空值键 【P1】

- 位置:`geminiservice.go:456-482, 521-558`。`parseEnvFile` 只认合法 `KEY=VALUE`,注释和 `export KEY=x` 被丢弃;`writeGeminiEnv` 只写回非空值。启用/停用代理各触发一次,静默丢内容。
- 修复:按行手术式编辑,只增删目标 key。
- 收益:中 | 成本:低 | 风险:中(高风险区)

### B7. 流式中断被记成"成功",坏 Provider 永不拉黑 【P1】

- 位置:`providerrelay.go:996-1011`。2xx 后复制响应出错时,非转换路径只打 WARN(`:1008`)然后 `return true, nil`。上游中途断流:客户端拿到没有终止帧的半截 SSE 可能永久挂起,同时 `RecordSuccess` 清零失败计数——反复半途断流的 Provider 永远达不到拉黑阈值。
- **注释同样在为错误逻辑辩护**(第二轮复核重点),`:1010`:

  ```go
  // 只要provider返回了2xx状态码，就算成功（复制失败是客户端问题，不是provider问题）
  return true, nil
  ```

  "复制失败是客户端问题"这个前提不成立:`copyRelayExecutionResponse` 的失败既可能来自客户端写失败,也可能来自上游读失败。代码在 `:999` 已经用 `c.Request.Context().Err()` 把客户端断开这一种识别并提前返回了,所以能走到 `:1008` 的情况**恰恰更可能是上游断流**。这行注释把唯一没被排除的可能归给了已经排除掉的原因。
- 协议矩阵转换路径的对应问题:`protocol_matrix_stream.go:57-66` 置错后 `ProcessLine` 永远返回空串,客户端 SSE 静默沉默,无 error 事件无 `[DONE]`,转换器还继续空转消费上游整个剩余流。
- 修复:区分上游读错误与客户端写错误;流已提交后出错时向客户端发协议对应的 error/终止帧并 RecordFailure(不重试,只记账)。
- 收益:高 | 成本:中 | 风险:高(协议矩阵高风险区)

### B8. 黑名单失败计数 read-modify-write 竞态 【P2,已下调】

- 位置:`blacklistservice.go:213`(读)、`:254`(Go 侧 `failureCount++`)、`:318`(经队列写回 `SET failure_count = ?`)。并发失败时两个请求都读到 2 都写 3,真实失败次数被低估,坏 Provider 拉黑被推迟,单请求会多打坏上游。30 秒去重窗口(`:245`)读的也是旧值,并发时两次都通过检查。
- **第二轮复核修正两点**:
  1. 初版写的"双 INSERT 产生两条记录"不成立——`database.go:110` 有 `UNIQUE(platform, provider_name)`。并发首次失败的实际症状是其中一条 INSERT 在队列里报约束冲突,即"丢一次失败计数 + 队列一条错误日志",不是数据重复。
  2. 严重度从 P1 下调到 P2:`IsBlacklisted`(`:450`)和 `RecordFailure` 都以 `IsBlacklistEnabled()` 为前置,而 `database.go:121` 的默认值是 `enable_blacklist=false`。**默认配置下这条竞态不触发**,只影响主动开启拉黑的用户。
- 同模式竞态还有 `AutoRecoverExpired` 与 `RecordSuccess` 都写 `last_recovered_at` 互相覆盖(:79-85, 572-656),导致降级计时起点被反复重置。
- 同模式竞态还有 `AutoRecoverExpired` 与 `RecordSuccess` 都写 `last_recovered_at` 互相覆盖(:79-85, 572-656),导致降级计时起点被反复重置。
- 修复:改 `SET failure_count = failure_count + 1` 原子自增 + UPSERT;条件写 `COALESCE(last_recovered_at, ?)`。
- 收益:中 | 成本:低 | 风险:低

### B9. 其余确定性问题(短清单)

| # | 问题 | 位置 | 修复方向 |
|---|---|---|---|
| B9.1 | `extractUpstreamError` 成功读取分支不关 Body,失败风暴下连接/FD 泄漏 | providerrelay.go:1026-1061 | 统一 defer Close |
| B9.2 | `forwardRequest` 32 小时超时且不绑客户端 context,客户端断开后 goroutine 挂 32h | providerrelay.go:907-930 | 传入 `c.Request.Context()` |
| B9.3 | 更新下载取消后旧 goroutine 可覆盖状态 / 双下载写同一文件 | updateservice.go:324-340, 604-888 | 各阶段查 ctx + 下载代际 ID |
| B9.4 | DBQueue worker panic 重启的 `wg.Add` 与 Shutdown 的 `wg.Wait` 竞态(WaitGroup misuse panic) | dbqueue.go:172-189, 527-546 | 重启前查 `closed.Load()` |
| B9.5 | 队列 Shutdown 竞态:queue 有空位的生产者随机命中 shutdownChan 分支被丢弃 | dbqueue.go:390-408, 529-532 | 两阶段关闭 |
| B9.6 | `commitBatch` 一条 SQL 失败整批 50 条日志全丢,错误只打 stdout | dbqueue.go:340-360 | 失败降级逐条执行 |
| B9.7 | 统计时区硬编码 Asia/Shanghai,非东八区用户日统计错位一天 | logservice.go:20-23, 268 | 存 UTC,展示用本地时区 |
| B9.8 | Codex bridge 历史仅内存 LRU(128),重启/淘汰后模型静默"失忆" | codex_chat_history.go:19-32 | 未命中至少告警或持久化 |
| B9.9 | `checkPendingApply` 恢复 ready 后不清 pending_apply.json,targetInfo 残缺 | updateservice.go:1177-1194 | 补全或重走 check + 最大年龄 |
| B9.10 | request_log 默认 `LogRetentionDays=0` 永不清理,无限膨胀(修正见下) | appsettings.go:171 | 见下 |

### B9.10 补充修正:日志膨胀不是机制 bug,是默认关闭 + 无提示

初版把它写成"清理机制脆弱",复核后不准确:`CleanupExpiredRecords`(`log_maintenance.go:107`)有 `days < 1 || days > 3650` 的硬校验,机制本身是好的。真实问题是 `LogRetentionDays` 默认 0 = 不清理(`:99`),且**没有任何 UI 提示或首次运行引导**,用户不知道要开。

注意这一条的修法有陷阱:直接把默认值改成 90 天,对已有用户等于"升级后某天突然删了历史数据"。**属于行为变更,必须配合升级时的显式提示**,不能静默改。这也是它没被排进第 0 批的原因。

---

## 第二部分:架构性问题(重构主战场)

### A1. 存储双源真相:Provider 主数据在 JSON、关联数据在 DB 且按 name 字符串关联 【最大单点】

这是全项目复杂度最高的一个决定,以下全部是它的连锁成本:

- `provider_alias` 表 + 48 小时 TTL + 禁止链式改名(provider_rename.go 整个文件的存在理由);
- 改名/删除的"先写 JSON、后提交 DB 事务、失败补偿回写"Saga(provider_rename.go:221-327、providerservice.go:258-282)——补偿本身可失败,进程在文件写完、事务提交前崩溃则**永久不一致**且无对账机制;
- `request_log.provider` 存名字字符串、无外键,rename 要 UPDATE 四张表;
- 黑名单等级配置同时存 JSON 和 DB,读取时"用 DB 覆盖 JSON 两个字段"打补丁(blacklist_level_config.go:55-71 注释自认【关键修复】),两条写路径天然分叉;
- app 设置分裂:`LogRetentionDays` 在 app.json,`blacklist_duration_minutes` 在 app_settings 表;
- `UpdateBlacklistSettings` 因为写走队列没法用事务,被迫用"Saga 模式"更新同一张表的两行(settingsservice.go:179-230)。

**重构方向(大预算下的正解)**:Provider 元数据迁入 SQLite,int64 ID 做关联主键;JSON 仅保留导出/导入格式。alias 表、48h TTL、链式改名限制、文件-DB 补偿回滚全部可删。app 设置单一化(全进 DB 或全进 JSON,选一)。
- 收益:极高(消灭一整类一致性 bug 和一整套补偿机制) | 成本:高 | 风险:中(迁移一次性,需写好迁移+回滚)

### A2. 双写队列的前提不成立,且倒逼出反模式 【高】

- 队列声称"消除 SQLITE_BUSY"(dbqueue.go:2),但 rename/delete 事务、`cleanupExpiredAliases`、各 ensure 建表全部绕过队列直连 DB——实际是 3+ 个并发写者,单写者前提早已破产,SQLITE_BUSY 仍靠(坏掉的,见 B4)busy_timeout 兜底。
- 队列还倒逼出:遥测 `finish()` 串行等待每条批次结果,k 次 attempt 尾延迟 (k+1)×100ms(relay_telemetry.go:193-210);request_log 与 relay_attempt 拆成独立任务跨批次提交,违反 dbqueue.go:129 自己写的"仅同构写入"约束,崩溃时产生孤儿行;settings 双行更新只能 Saga。
- **重构方向**:二选一——(a) 去队列:WAL + DSN busy_timeout + 短事务,SQLite 单机场景本来就够;(b) 真单写者:`SetMaxOpenConns(1)` 的专用写连接 + 所有写入过队列。当前"双队列 + 事务旁路"是最差组合。
- 收益:高 | 成本:中 | 风险:中

### A3. 三套平行复制的转发调度循环 【高】

- `proxyHandler`(providerrelay.go:400-841,~440 行)、`geminiProxyHandler`(:1608-1878)、`customCliProxyHandler`(:2034-2410)各自实现完整的"过滤 → Level 排序 → 轮询 → 拉黑/降级两套重试 → 记账 → 通知",约 1300 行重复,且已漂移:
  - custom 路径硬编码 `AnthropicMessages`,漏掉主路径的部分改进;
  - gemini 路径 `RecordSuccess/RecordFailure` 错误全部 `_ =` 吞掉(:1744-1836),主路径有 WARN;
  - custom 路径仍是无门控 `fmt.Printf`(每请求 5-10 行过 os.Pipe + 分类),主路径已收敛到 `relayDebugf`。
- gemini 独立的根因是 `GeminiProvider` 是与 `Provider` 平行的另一套类型(string ID vs int64),连 `roundRobinOrderGemini` 都复制了一份;`GeminiService`(1106 行)自建了第二套 Provider CRUD。
- **重构方向**:抽统一 `dispatchWithFailover(scope, providers, forward)` 调度器;GeminiProvider 并入统一 Provider 模型(string ID 迁移映射)。任何拉黑/重试策略改动从改三处变成改一处。
- 收益:高 | 成本:高 | 风险:中(需先补 failover 主循环测试,见 M6)

### A4. services 上帝包:90 文件 4.8 万行、零接口、全局单例 【高】

- 全部服务是具体结构体互相持有指针,无一个 interface;包内可见性使"私有"失效(PiSettingsService 直接调 `providerService.loadProvidersRaw`;Pi 服务复用 `ClaudeProxyStatus` 作为返回类型)。
- `GlobalDBQueue`/`GlobalDBQueueLogs` 包级全局,依赖不出现在任何构造签名里;main.go:104 注释自认曾因初始化顺序出过 bug;telemetry 判空静默丢日志,blacklistservice 不判空会 panic。
- `SetPricingService` 后置注入完全没必要(pricing 在 relay 之前就构造好了),但它导致"已构造未注入"窗口态 → LogService 三个构造变体 + telemetry legacy 分支 → **同一进程可能同时存在三套定价引擎,计费口径可分叉**(relay_telemetry.go:63-66、logservice.go:331-351)。
- **重构方向**:按域拆子包(relay / provider / pricing / logging / blacklist / platform);队列作为构造参数注入;删除全部 setter 注入和构造函数变体,只留全参构造。
- 收益:高(长期) | 成本:高 | 风险:低(纯结构移动,行为不变)

### A5. Pi 子系统"半复制半侵入" 【中高】

- 26 个 pi_*.go 约 8500 行:既平行复制了一套 provider CRUD/事务/回滚(pi_supplier_mutation.go),又向通用模块渗透——relay 主循环 5+ 处 `if kind == "pi"` 特判、Provider 结构体塞 `PiPlatform/PiTemplate` 字段、pricingservice.go:1030 的 pi 分支、`var piDebugLogging` 包级全局由构造副作用设置。
- pricing(横切层)依赖 piSettings(平台特化层)是依赖倒置错误;将来 pi 要查价格就成真环。
- 术语混乱的根源也在此:同一物在主体系叫 Provider、pi 体系叫 Supplier(40 处),`GetSupplier(providerID int64) (Provider, error)` 一行里两个词。
- **重构方向**:提为 `services/pi` 子包;定义 `PlatformExtension` 钩子接口(body 预处理、credential 清洗、model 映射、定价解析),pi 实现注册进注册表,relay 主循环删除全部 `kind == "pi"` 字面量;pricing 反转为 `PlatformPriceResolver` 接口。
- 收益:中高 | 成本:高 | 风险:中(Pi 托管高风险区)

### A6. 平台扩展无注册表:新增一个平台要改 ≥12 处 【中】

- providerFilePath switch(且 `directapply_helpers.go` 有一份**已漂移**的拷贝:不支持 pi/custom,返回空路径 → 静默失效)、resolvePlatform 别名 switch、registerRoutes、protocol/plan.go 两个 switch、deeplink 两个、mcp/prompt/envcheck/cliconfig 各若干、新建整个 `xxxsettings.go`、main.go 构造注册。
- 平台别名(`claude|claude-code|claude_code`、`deepseekcode|deepseek_code|deepseek-code`)在至少 5 个文件重复归一化。
- 四个平台 settings 服务(claude 421 行 / deepseekcode 361 / reasonix 320 / codex 811)是逐行同构的手工拷贝,连错误消息逐字相同,deepseekcodesettings.go:112 还留着复制未对齐的缩进;修一个兜底 bug 要记得改 4 处,且这四个文件**零测试**。
- **重构方向**:`PlatformDescriptor{ID, Aliases, ConfigFile, Routes, Protocols, EnvKeys}` 注册表,一处注册各方查表;settings 服务抽通用 `envJSONSettingsService`,codex 的 TOML 分支单独保留。
- 收益:中高 | 成本:中 | 风险:中(改写用户 home 目录文件的代码,先补测试)

### A7. JSON 落盘不统一:正确的 `atomicWriteFile` 只有 provider JSON 在用 【中】

- `app.json` 用裸 `os.WriteFile` 直接覆盖(appsettings.go:254,半写即损坏);mcpservice 写用户级 claude.json/settings.json 也是裸写(:885-998);gemini/prompt/skill/blacklist_level 用 temp+rename 但无 fsync;`writeGeminiSettings` 读-合并-写全程无锁。
- `atomicWriteMutex` 是全进程一把大锁(过度),不走它的写入又完全无锁(不足)。
- **重构方向**:所有 JSON 落盘统一走 `atomicWriteFile`;锁改按路径分段。
- 收益:中 | 成本:低 | 风险:低

---

## 第三部分:可维护性与残留

### M1. `ReqeustLog` 拼写错误扩散为公共 API(64 处、10+ 文件,含导出函数 `ReqeustLogHook`)。纯机械重命名,越晚做扩散越广。
### M2. providerrelay.go 2571 行混杂 8 种职责(路由、转发、DDL 迁移、4 个平台的 SSE 解析、轮询、URL 工具);request_log 的 34 列写 schema 在 relay_telemetry.go 内联 SQL,读 schema 在 logservice.go,无共享定义,加字段要改三处、漏一处静默产生 0 值统计。
### M3. schema 演进无迁移机制:request_log 28 列靠启动时逐列 pragma 探测 + ALTER(同文件两套写法并存),其余表只有 CREATE IF NOT EXISTS 没有加列机制;建表逻辑散落 4 个文件、被业务路径惰性触发(B3 即其恶果)。应引入 `schema_version` + 顺序迁移。
### M4. 死代码/残留:health_check_history 全套 DDL/DML 维护一张永远为空的表;Provider 结构体背着 6 个"仅兼容读取"字段;`components/Availability/`、`components/SpeedTest/` 空目录;前端 `DeepLinkImportDialog.vue`、`endpointSync.ts`、`hotkeyUtils.ts`、`piProviderTemplates.ts` 零引用;`services/TEST_README.md` 引用不存在的路径。
### M5. 七层历史兼容常驻:最糟的是 `platform='custom:<toolId>'` OR 兼容进了**每条统计 SQL**且永远删不掉——应一次性 UPDATE 回填旧行后删分支;`.cc-switch` 三代格式、`.codex-swtich` 拼错目录、旧 mcp.json 双格式试解析等应收敛到独立 migration 模块。
### M6. 测试覆盖倒挂:协议桥接和 pi 体系覆盖较好,但 relay failover 主循环、四大 settings 服务(改写用户 home 文件的最危险代码)、dbqueue(全局单例 646 行)、importservice/updateservice/cliconfigservice/mcpservice 四个千行文件全部零测试。**这直接锁死了 A3/A6/B5 的重构**——动手前必须先补安全网。
### M7. 日志无框架:130+ 处 `fmt.Printf`,前缀风格混杂(`[WARN]`/`[警告]`/emoji),全部过 consoleservice 的 os.Pipe + 逐行分类;错误消息中英随机。引入 `log/slog` 统一。

---

## 第四部分:性能(现存,已排除上一轮 perf commit 修掉的)

| # | 问题 | 位置 | 方向 |
|---|---|---|---|
| P1【已下调为中】 | `IsBlacklisted` 每次直打 SQLite,单请求过滤阶段 N 次 DB 往返;而低频的 IsBlacklistEnabled 反而有缓存。**复核修正**:`:450` 以 `IsBlacklistEnabled()` 为前置且默认 `enable_blacklist=false`(database.go:121),仅影响主动开启拉黑的用户 | blacklistservice.go:447-467 | 黑名单内存 map + 写路径失效 |
| P2 | 遥测串行等待批次,k 次 attempt 尾延迟 (k+1)×100ms 占住 goroutine | relay_telemetry.go:193-210 | fire-and-forget 或单条多行 INSERT |
| P3 | metadata 注入路径每 attempt 全量 decode/encode 请求体(数百 KB 上下文) | upstream_body_policy.go:27-59 | sjson 定点改 + 重试循环外缓存 |
| P4 | 启动同步解析全量 LiteLLM 价格表(上万模型 3 次 Unmarshal + 每条 json.Indent),阻塞窗口创建 | pricingservice.go:232-274 | 后台加载,rawJSON 格式化按需 |
| P5 | 每 attempt `os.Stat` 代理配置 + 轮询设置各 stat 一次 | providerrelay.go:912-921 | 请求级快照 |

定价热路径查询本身(atomic load + 预编译正则线性扫)已合理,无需动。

---

## 第五部分:前端

| # | 问题 | 方向 |
|---|---|---|
| F1 | `Main/Index.vue` 4592 行:5 平台 tab、CRUD、黑名单、Pi 开关、3 个轮询定时器、~139 函数,且基本未接 i18n(27+ 处硬编码中文,全项目其他 40 文件已接入) | 按平台适配器抽 `useProviderPlatform`,面板拆子组件 |
| F2 | 无状态管理:唯一 store 是 useUpdateStore 模块级单例;多组件各自轮询同一数据,设置改动要等下轮询才可见 | Pinia 或按域模块级 composable + Wails events 推送替代轮询 |
| F3 | 大面积绕过生成 bindings 手写 `Call.ByName` 字符串 + `as` 断言(settings.ts 全文件、Main 20+ 处),Go 签名改动运行时才炸 | 统一走 bindings,ByName 仅留未覆盖处 |
| F4 | 死代码见 M4;`Mcp/index.vue` 小写与其余 `Index.vue` 大写混用,大小写敏感文件系统有踩坑风险 | 删除 + 拆 views/ 与 components/ 统一命名 |
| F5 | 其余大文件:CLIConfigEditor 1856 / General 1727 / Pricing 1650 / Mcp 1567 行 | 随功能改动渐进拆分 |

---

## 重构路线建议(第二轮按根因重排)

与初版的差异:① 第 0 批增加根决策 4 的四处「静默继续 → 拒绝」改动;② 第 2 批内部**先队列后存储**(初版是先存储后队列),理由见第零部分。

### 第 0 批:立即修的确定性 bug(每个 ≤ 半天)
1. B1 `$pid` → `$targetPid`(更新功能当前对便携版是坏的)
2. B4 busy_timeout 进 DSN
3. B3 health_check_history 建表挪 InitDatabase(临时止血)
4. B2 装配 piGatewaySync
5. B9.1/B9.2 Body 关闭 + context 绑定
6. **根决策 4 的四处**(违反项目自己的「失败必须暴露」红线,每处改动都小):
   - B5:`codexsettings.go:98` 解析失败 `return err`,不写空 map;同时删掉那句错误的「备份已保存」注释
   - B7:`providerrelay.go:1010` 区分上游读失败与客户端写失败,删掉错误注释
   - B6:Gemini `.env` 改按行手术式编辑
   - B9.8:Codex 历史未命中至少告警,不静默降级
   
   注:B5/B6 触及用户 home 文件(高风险区),若想更稳妥可只做「拒绝 + 注释修正」,把「文本级手术编辑」留到第 1 批有测试后再做。

### 第 1 批:补安全网(为后续重构解锁)
7. M6:给四大 settings 服务补 temp-home 表驱动测试;给 relay failover 主循环补拉黑/降级模式测试;dbqueue 补并发/关闭测试
8. B5/B6 的手术式编辑改造(若第 0 批只做了拒绝)

### 第 2 批:并发与存储(最大收益的架构刀,注意顺序)
9. **A2 先做**:队列二选一(建议去队列 + WAL + DSN busy_timeout + 短事务)。这是 A1 的前置——Provider 入库需要可靠事务,当前队列让事务用不了。B8、B9.4-B9.6、P2 随之消失或简化
10. **A1 后做**:Provider 主数据入 SQLite,删 alias/Saga/补偿全套;app 设置单一化;M3 引入 schema_version 迁移机制(顺带删 health_check 全套、回填 custom: 旧格式行、清 M5 兼容层)
11. A7:JSON 落盘统一 atomicWriteFile
12. B9.10:配合迁移给 LogRetentionDays 一个非零默认值 + 升级时显式提示(行为变更,不可静默改)

### 第 3 批:转发层收敛
13. A3:统一 failover 调度器,GeminiProvider 并入 Provider(有第 1 批测试兜底);P1/P3/P5 顺带做

### 第 4 批:结构治理(可与日常开发并行、渐进)
14. A6:平台注册表 + settings 服务合并(与 A3 同属根决策 3,可合并规划)
15. A4/A5:按域拆包、Pi 提子包、删 SetPricingService/构造变体、M1 重命名、M7 slog
16. 前端 F1-F4

### 建议放弃/降级的项
- B9.7 时区:若用户群确定在东八区,可只留 backlog
- P4 启动价格表解析:桌面应用启动 <1s 感知不明显,排最后
- F5 其余前端大文件:不专项重构,随功能改动渐进拆

---

## 交叉验证说明

- B1、B2、B3、B4 四个 P0 已在主会话用 grep 独立复核原文,均属实。
- 多路扫描独立收敛到同一结论的(可信度高):三套转发循环复制(架构/可维护性/性能三路各自发现)、providerrelay.go 巨型文件、四份 settings 拷贝、双队列设计缺陷、health_check_history 残留、`ReqeustLog` 拼写、Availability/SpeedTest 空目录。
- 正确性扫描同时给出了"排除项":重试 body 重放正确(bytes 每次新建 reader)、响应已提交防降级设计正确、provider 缓存有锁且深拷贝、无发现无锁 map——这些不是问题,不要顺手"修"。

### 第三轮:实施验证(2026-07-27,第 0/1 批已落地)

实施记录见 `doc/refactor-plan.md`。实施过程对本报告的修正:

1. **B4 的成因比报告写的更明确**。`database.go:27` 原有注释声称"modernc.org/sqlite 需要显式执行 PRAGMA",这是错的。实测:DSN `_pragma=busy_timeout(30000)` 对连接池每条连接都生效;旧写法只有第 1 条连接是 30000,其余为 0。对照测试已固化,防止改回去。
2. **B7 的修复点比报告识别的多一处**。报告只指出了 `providerrelay.go:1010` 的分类错误,但即使分类正确,`errResponseCommitted` 分支在两处(拉黑模式 `:648`、降级模式 `:786`)都在 `RecordFailure` 之前 `return`——也就是说"流中途断开"从来没有进入过 provider 记账。只改分类不够,必须同时补记账。
3. **B5 的"解析失败继续"不止 codex 一处**。同一模式存在于 `claudesettings.go:101`、`reasonixsettings.go:88`、`cliconfigservice.go` 的 `saveClaudeConfig` 和 `saveCodexConfig`,共 5 处。这正是根决策 3(复制代码)的直接后果:一个 bug 被复制了五份。报告按单点记录低估了范围。
4. **B9.6 已由测试实测确认**:批量提交 3 条(含 1 条违反 NOT NULL)后落库 0 行,两条正常数据被连带回滚。
5. **新发现一个测试性问题**:代理状态文件在可执行文件同级 `.code-switch-R/proxy-state/` 而非 HOME 下,`t.Setenv("HOME")` 无法隔离,测试之间会串扰(实际导致了一次假失败)。A1 存储统一时应一并解决配置根目录的可注入性。
6. **一个前置环境问题**(与代码无关但阻塞全部验证):本机 Go 工具链是 386,`go test ./services` 会在 Wails webview2 COM 回调注册处 panic。必须 `GOARCH=amd64`。这解释了为什么这些模块长期零测试——本地跑不起来。

### 第二轮复核(读原文验证,非 grep)

已直接读原文核对并证实的条目:B5(`codexsettings.go:64-199`)、B7(`providerrelay.go:985-1018`)、B8(`blacklistservice.go:195-333`)、`provider_blacklist` 表定义(`database.go:79-136`)、`IsBlacklisted`(`blacklistservice.go:447-477`)、`CleanupExpiredRecords`(`log_maintenance.go:99-118`)、`AppSettingsService.saveLocked` 裸写(`appsettings.go:245-264`,证实 A7)。

三处被修正(初版有误或过重):
1. B8 的"双 INSERT 产生两条记录"不成立(有 UNIQUE 约束),且默认配置不触发 → P1 降 P2。
2. P1 性能项的"每请求 N 次 DB 查询"有 `enable_blacklist` 前置,默认 false → 高降中。
3. B9.10 不是清理机制脆弱(有硬校验),而是默认关闭且无提示;且改默认值属行为变更。

一个新增判断:B5 与 B7 的问题代码各自带一句**为错误逻辑辩护的注释**,注释内容与实际控制流不符。这类"注释比代码更危险"的位置值得在后续 review 中专门留意——它们会让维护者误判风险已被处理。
