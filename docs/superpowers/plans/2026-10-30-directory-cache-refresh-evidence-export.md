# 目录缓存刷新运行证据导出实施计划

> 沿用用户对 preview 路线图的持续执行授权；不使用 worktree、不访问真实账号、不提交 Git。

**目标：** 导出所选目录缓存刷新运行在同一 SQLite 快照中的完整且有界 JSON 证据。

**Spec：** `docs/superpowers/specs/2026-10-30-directory-cache-refresh-evidence-export.zh-CN.md`

## 任务

1. [x] Store：先写失败测试，新增 run 摘要+事件单事务导出，账号隔离、100 事件全量上限、缺失 started/超限行为。
2. [x] Downloader/API：仅使用认证客户端账号指纹，提供受保护 GET 路由；校验 run ID，账号身份不进入导出，错误整份拒绝。
3. [x] WebUI：所选运行显示导出按钮；显式点击才请求；切换运行时旧响应不下载或覆盖状态。
4. [x] 更新体验计划、进度、路线图与版本记录；全仓 Go/vet、前端回归、三平台打包与 ZIP 检查。
