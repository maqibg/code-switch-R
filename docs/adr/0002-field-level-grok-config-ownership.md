---
status: accepted
date: 2026-07-30
---

# 只接管 Grok 连接字段

code-switch-R 管理 Grok Build 时只拥有 `[models].default` 和 `[model.code-switch-r]`。应用记录这些字段原来的存在状态、原始值和最后注入哈希，保留所有无关 TOML 字段；当受管字段被外部修改、不再匹配注入状态时，拒绝切换或恢复。

拒绝使用整份文件快照自动恢复，因为它可能破坏用户或其他工具在接管后新增的模型、MCP Server、插件、subagents、注释和设置。冲突必须由用户显式解决：备份后重新接管，或者保留当前文件并放弃接管。
