---
status: accepted
date: 2026-07-30
---

# 分离 Grok Build 运行模式

Grok Build 自定义 Provider 与 xAI OAuth 账号建模为两个独立产品平台，因为二者的所有权、遥测和失败语义不同。Provider 与账号数据可以共存，但 Grok 运行时只能应用自定义 Relay、官方 OAuth 或未接管模式之一；OAuth 请求由 Grok Build 直接访问 xAI，不进入 Provider Level、轮询、降级、黑名单、请求日志或成本统计。

拒绝将 OAuth 账号合并到 Provider 池，也拒绝让两种模式同时显示为已应用，因为 Grok Build 只有一个有效默认模型入口，并发所有权最终只会变成最后写入者生效。该决定要求使用共享模式协调器执行原子切换，同时保留非活动平台的数据，但不能把它们视为已应用运行状态。
