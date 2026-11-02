# 单条远端操作脱敏证据导出 Implementation Plan

**Goal:** 让管理员把单笔远端操作摘要及其完整追加事件保存为脱敏 JSON。

**Architecture:** Store 在单个 SQLite 只读事务中读取摘要和有界事件；API 复用现有递归证据遮蔽规则并提供附件；历史条目用同源 fetch 生成 Blob 下载。

**Constraints:** SQLite-only；无 provider 请求、写操作或 schema 迁移；超限整批拒绝；不导出 account fingerprint、Cookie 或原始敏感 evidence。

## 工作项

1. [x] 审查事件表：evidence 上限 64 KiB 且事件可追加；定为每操作 100 条，读取第 101 条检测超限，最坏 payload 约 6.25 MiB。
2. [x] Store 测试先行：单只读事务摘要/事件、序号排序、100 条完整边界、101 条拒绝、未知操作与 context 取消；实现只读事务查询。
3. [x] API 测试先行：GET-only、附件头、事件递归遮蔽、敏感字段不泄漏、unknown 仍只读；注册受管理认证路由。
4. [x] WebUI 测试先行：条目按钮只传 operation ID、不带分页；Blob 下载、HTTP 错误可见，并验证请求未完成时重复点击不会启动第二次下载。
5. [x] 更新中文测试指南、版本路线图与进度；全仓验证、三平台打包、ZIP 校验和真实账号只读 GET 已通过。

## 验收重点

- 不允许以截断事件列表冒充完整导出。
- count 与事件内容处于同一 SQLite 快照。
- API 输出与现有事件分页相同的敏感键递归脱敏，不允许前端自行复制原始证据。
- 发布包不得携带 `.nas-downloader`、`admin-key.json`、`.env` 或百度授权文件。
