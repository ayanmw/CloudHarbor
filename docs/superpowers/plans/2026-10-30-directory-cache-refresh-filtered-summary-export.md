# 刷新运行筛选摘要导出实施计划

> 沿用持续 preview 路线图授权；主检出内实施，不用 worktree，不访问真实账号，不提交 Git。

1. [x] Store：先写失败测试，基于 `.90` 的筛选查询导出一致 SQLite 快照；最多 5,000 条，超限拒绝整份。
2. [x] Downloader/API：认证账号绑定，校验筛选参数；显式 GET 导出 JSON、筛选回显、UTC、附件名和 no-store。
3. [x] WebUI：在筛选栏添加显式导出按钮；错误可见，筛选更改后忽略旧响应。
4. [x] 更新路线图与进度；全仓 Go/vet、WebUI/userscript、三平台打包和 ZIP 来源快照核验全部通过。
