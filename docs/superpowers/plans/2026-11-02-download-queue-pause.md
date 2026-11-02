# 下载队列暂停与恢复实施计划

> 主目录内联实施；保留 dirty/untracked 内容；不使用 worktree，不提交或推送。

**目标：** 实现跨重启的全局下载队列暂停/恢复，暂停只阻止新任务领取，不中断活动传输和持续监控。

**设计：** `docs/superpowers/specs/2026-11-02-download-queue-pause.zh-CN.md`

## Task 1：SQLite 暂停状态与调度门禁

**文件：** `internal/downloader/scheduler.go`、`internal/downloader/downloader.go`、`internal/downloader/*_test.go`。

- [x] RED：旧设置缺字段、暂停设置关闭/重开恢复、失败写入原子性。
- [x] 独立维护 `userPaused` 与 maintenance；暂停排空活动任务但不取消任务。
- [x] claim 在用户暂停期间不取任务，恢复只清除用户暂停并唤醒 worker。
- [x] 证明扫描/reconciliation 可在暂停期间将新任务登记为 pending。

## Task 2：管理 API 与队列状态

**文件：** `main.go`、`main_test.go`、`internal/downloader`。

- [x] RED：认证门禁、状态/UTC/active 字段、幂等控制、维护和错误态冲突。
- [x] 加入暂停/恢复 POST API，并将权威状态随 Poll 和 WebSocket status 一并返回。
- [x] 证明恢复请求不能解除维护门禁或忽略 SQLite 持久化失败。

## Task 3：Poll/WebSocket 页面

**文件：** `internal/webui/index_poll.html`、`index_ws.html`、`res/shared.js`、`shared.css`、`scripts/webui.test.cjs`。

- [x] RED：两个页面显示服务端暂停状态、活动任务排空提示和显式控制按钮。
- [x] 控制操作只在用户点击后发送；失败时保留已知状态并提示，不推测成功。
- [x] WebSocket 与 Poll 使用统一状态渲染，不因过期响应回滚到旧值。

## Task 4：发布 `.123`

- [x] 更新 `docs/ai/progress.md`、路线图、版本表及 `release/TESTING.md`。
- [x] 完成 Go/vet/UI/userscript、三平台构建、ZIP/SHA-256、源码快照和运行数据排除检查。
- [x] 生成 releases 同级验证记录，注明仅本机/SQLite/mock，不连接真实百度账号或 NAS/SMB。
