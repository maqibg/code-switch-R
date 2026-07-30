---
status: accepted
date: 2026-07-30
---

# 保持 Grok OAuth 凭据便携

Grok OAuth 凭据以明文 JSON 保存在可执行文件同级 `.code-switch-R` 数据目录。应用定位是本地便携工具，因此移动应用数据时必须能够同时迁移已保存账号，不能依赖 DPAPI、Keychain、Secret Service 或其他绑定当前机器的凭据存储。

本功能拒绝使用系统密钥库和不可便携的加密存储。应用仍必须使用原子写入、保持导入源文件只读，并阻止 access token 和 refresh token 出现在日志、普通前端响应、请求遥测或错误文本中；这些规则只防止程序意外泄露，不宣称抵御本地磁盘访问。
