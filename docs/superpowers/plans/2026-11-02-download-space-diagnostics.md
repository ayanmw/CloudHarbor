# 下载空间诊断与恢复提示实施计划

> 主目录内联实施；保留 dirty/untracked 内容；不使用 worktree，不提交或推送。

**目标：** 将 `.121` 容量失败的可审计证据直接呈现在持久任务快照、任务列表和事件时间线中。

**设计：** `docs/superpowers/specs/2026-11-02-download-space-diagnostics.zh-CN.md`

## Task 1：失败任务持久容量摘要

**文件：** `internal/downloader/scheduler.go`、`internal/downloader/downloader.go`、`internal/downloader/task_events_test.go` 或新增 `storage_space_test.go`。

- [x] 先加 RED 测试：空间失败写入可选任务摘要并由 task page 读出；旧任务 JSON 可加载；手动 retry 和后续非空间失败清除过期摘要。
- [x] 将 `local_storage` 可选字段加入 queueJob 与 Task；仅从 typed storage error 投影摘要，绝不保存底层路径错误消息/栈或 provider 响应。
- [x] 在 claim/retry/complete 生命周期维护摘要，与既有 queueJob 持久化顺序一致。
- [x] 关闭 SQLite 后通过 `OpenDownloader` 重开并读回任务摘要；旧 JSON 兼容和手动重试回归通过。

## Task 2：任务页容量诊断

**文件：** `internal/webui/res/shared.js`、`internal/webui/index_poll.html`、`internal/webui/index_ws.html`、`internal/webui/res/shared.css`、`scripts/webui.test.cjs`。

- [x] 先加 RED UI 回归：不足、未知和写入时磁盘满均分类正确，显示路径/字节/UTC，未知容量不暴露负数。
- [x] 实现一份共用 escaped renderer，供 Poll 与 WebSocket 页面复用；保留现有任务操作能力。
- [x] 断言只渲染诊断时不发送重试请求；空间路径和 HTML payload 均按文本呈现。

## Task 3：事件时间线诊断卡

**文件：** `internal/webui/res/shared.js`、`scripts/webui.test.cjs`。

- [x] 先加 RED 事件测试：空间分类标签、专用字段展示及本机 UTC 标签；不把 provider/cloud/response 时间改名。
- [x] 原始结构化证据继续折叠可查，现有 JSON 导出不变。
- [x] 迟到事件响应/切换 task 逻辑保持原有实现；渲染复用同一纯函数且不发请求。

## Task 4：发布 `.122`

**文件：** `docs/ai/progress.md`、`docs/ai/cloud-management-roadmap.zh-CN.md`、`docs/ai/release-versioning.zh-CN.md`、`release/TESTING.md`。

- [x] 五项独立结果均有回归证据，`.122` 五项全部完成。
- [x] 运行 `scripts/package.ps1 -Version v0.8.30-preview.122 -AllowDirty`；Go tests/vet、WebUI/userscript 与 Windows AMD64/Linux AMD64/Linux ARM64 构建通过。
- [x] 同一 `.122` 预览包已纳入最终交付状态文档；ZIP 13 项、12 项载荷哈希匹配、420 项源码快照逐项匹配，Windows `--version` 正确，私有运行数据为 0，staging 残留为 0。完整记录写于 releases 同级 validation 文件。
- [x] 验收范围限于本机模拟服务和 UI；未访问真实百度账号或 NAS/SMB。
