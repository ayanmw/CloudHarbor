# 单个下载任务暂停与继续设计

## 目标

在现有全局队列暂停之外，允许管理员单独暂停某个未完成的顶层下载任务，并在之后继续。暂停状态写入现有 SQLite 队列记录，进程重启后保持暂停。

## 现状与推荐

`.123` 的全局暂停只阻止调度器领取新任务，已开始的传输会自然完成。单项暂停需要停止选定任务的活动传输，同时保留该任务已有的安全续传 partial；其他任务和目录监控应继续工作。

实现复用当前任务 context cancel、SHA-256 收据和 Range 续传机制。请求暂停时先在队列锁内持久化 `paused` 状态和操作事件，再取消该任务 context。完成回调识别这个用户暂停意图，将被取消的活动尝试保留为 paused，不计作 provider 失败。恢复下载中的任务时转成 pending，由现有队列重新领取并执行完整身份检查与 partial 校验。

## 状态与证据

- 新增持久字段 `paused_from_status`，只保存 `pending`、`retry_wait` 或 `downloading`。旧记录没有该字段时保持现有状态语义。
- API 状态将 `paused` 暴露为稳定任务状态，且提供 `can_pause`/`can_resume`。暂停时间由该任务的 `Updated` 与新增 `task_paused`/`task_resumed` SQLite 事件表达；事件证据记录之前/之后状态、操作原因和 UTC。本机记录时间不冒充百度观测时间。
- `retry_wait` 暂停时保留原重试截止；恢复时若截止已过则立即进入 ready，否则继续等待原截止。
- 活动下载暂停先存储意图再发 cancel。回调持久化暂停完成证据；若进程在回调前退出，启动仍看到 paused，恢复会从 pending 继续。

## 安全边界

- 只允许对 `pending`、`retry_wait`、`downloading` 执行单项暂停。正在进行或结果未知的网盘后处理 (`SourceState != ""`) 不能被单项暂停/恢复改写，维持现有隔离机制。
- 暂停必须先 SQLite 保存成功，之后才显式取消所选活动下载。若 SQLite 写入失败，维持“未确认暂停”并遵循现有队列持久化故障的 fail-closed 行为；不假报 paused。
- 活动任务正在收尾时，恢复请求返回冲突；只有取消回调已移除 active 后才能恢复，避免旧尝试和新尝试并发。
- 全局暂停、维护门禁和单项暂停相互独立。单项恢复不能清除全局暂停或 maintenance；监控继续，暂停任务不参加调度。
- 恢复不自动发起百度远端写操作；下载使用既有任务身份、授权及 partial 校验规则。

## 接口与页面

- 管理员认证保护 `POST /api/tasks/{id}/pause` 与 `POST /api/tasks/{id}/resume`，重复操作幂等，非法状态返回冲突，存储失败返回服务错误。
- Poll 与 WebSocket DTO 同步包含任务暂停操作能力。任务卡展示“已暂停”，说明这是本机队列状态，并提供明确的“暂停任务”/“继续任务”按钮。
- 请求仅由用户点击发出；401 沿用会话失效处理，失败不乐观切换状态。

## 验收

覆盖 pending/retry_wait 活动状态跨重启、真实下载中断后 partial 续传、取消与远端 source operation 竞态、SQLite 持久失败、重复操作、全局暂停/维护门禁交互、API 认证及 Poll/WebSocket 页面点击行为。全量发布仍需 Go 测试/vet、WebUI/userscript、Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP/源码快照/运行数据排除检查。真实百度和 NAS/SMB 边界单独声明。
