# 下载日志关联

默认文件传输并发为 2（Config 和 Start.ps1），本次没有调高并发。目录准备不计为文件传输。

## 关联字段

- `at_utc`：本机记录事件的 UTC 时间，RFC3339 纳秒格式。
- `batch_id`：根任务本轮执行的持久化 AttemptID，可与任务事件对应；根任务重新执行时使用新 ID。独立测试或无根队列调用可为空，不伪造持久执行记录。
- `transfer_id`：单个文件本次传输的随机 ID。获取链接、HTTP、Range 回退、本地验证和发布共用此值；新的文件重试会产生新 ID。
- `fsid` / `path`：云端文件身份与完整路径。`local_path` 出现在 Download 层。
- 重复文件哈希扫描也使用相同 HTTP helper；其 `transfer_id` 带 `hash-` 前缀，`batch_id` 对应扫描 RunID。

路径和 ID 使用引用格式转义，避免文件名中的换行拆成伪造日志。不会输出 token、AppKey、临时下载链接、带查询凭据的 URL 或 provider 原始响应正文。

## 阶段

1. `transfer_started`：开始处理本地传输，记录预期字节。
2. `link_request` / `link_finished`：获取链接，结束时提供成功/失败、HTTP 状态和 provider 错误码。
3. `http_request` / `http_response`：记录续传 offset、HTTP 状态与 Content-Length；200/206 只证明收到响应，不代表文件已下载完成。长度 -1 表示尚未知晓。
4. `http_failed`：请求阶段失败，仅输出错误类型和取消标记，避免 URL 泄漏。
5. `range_restart`：服务端忽略 Range，本次传输仍使用原 transfer_id，并记录原偏移。
6. `transfer_progress`：持续读取数据时，最多每 30 秒记录已完成字节、预期字节和耗时；不逐数据块输出。阻塞读取期间由已有空闲超时处理，不另起心跳请求。
7. `local_verify_started`：开始本地 SHA-256 核对。已有本地文件验证成功输出 `local_verified reused=true`。
8. `local_publish_started` / `local_published`：发布到目标路径；后者表示已完成验证、发布及目录同步。
9. `transfer_finished`：整段本地传输结束，结果为 success / failed / cancelled，带耗时；失败时输出安全的错误类型、provider 状态码及可重试分类。

`transfer_finished outcome=success` 表示本地文件传输/校验成功，不表示根任务的网盘保留标记或删除后处理已经完成。

## 排查

先按 `path` 或 `fsid` 找到文件，再按 `transfer_id` 过滤本次传输；按 `batch_id` 可以查看同一根任务下多个并发文件的日志。跨重启时以 fsid/path 与根任务事件关联，不将新的 ID 混作过去执行。

## 本次写入量

`transfer_finished` 增加 `written_bytes_this_attempt`，表示当前 transfer_id 调用内实际写入下载部分文件的字节数，成功、失败、取消均输出。断点续传不计入已有部分；服务端忽略 Range 后从零重下按本次实际写入计数；复用已验证本地文件为 0。

该值不是网络接收字节、持久化统计或已发布凭据；写入后仍可能发生同步、校验或发布失败。仍需结合 outcome 和 local_published 判断结果。`elapsed_ms` 保持原有整段处理耗时口径，不能用它替代任务的有效下载时长或据此计算纯网络平均速度。
