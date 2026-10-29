# SQLite 关键任务事件证据补齐设计

## 状态

已按持续优化授权实现，范围采用用户选择的“关键事件补齐”。

## 背景与目标

SQLite 已保存任务当前态和不可变事件，但取消、手动重试、扫描恢复及源项目缺失仍使用通用状态持久化；下载尝试结束事件也没有记录结构化错误分类与计划重试时间。本次补齐这些关键转折的可追溯证据，便于区分操作意图、本机观察、线上观察与未来计划，并支持重启后核查。

## 范围

1. **用户取消**：写 `task_cancelled` 事件；记录任务 ID、原/新状态、当时 attempts/failures、`recorded_at_utc`、原因码 `user_request`。取消入口可来自管理 API；不保存请求头、令牌、IP 或原始错误文本。
2. **手动重试**：写 `task_retry_requested` 事件；记录原/新状态、重置前后的 attempts/failures、`recorded_at_utc`、原因码 `manual_retry`。不推断操作者身份。
3. **扫描恢复与源项缺失**：分别写 `task_recovered_after_scan` 和 `task_source_missing`。记录扫描 epoch、扫描成功观察时间 `cloud_observed_at_utc`、任务原/新状态及稳定原因码。epoch 是本进程认证代次，不能单独当作跨重启唯一扫描 ID；事件自身的 `run_id` 用于区分启动运行。只有通过现有认证 epoch 校验的成功扫描可产生这类事件。
4. **下载尝试完成**：保留现有 `download_attempt_finished`，扩充结构化 `outcome_class` 和结局状态，按最终状态及已有错误分支映射：`done` 为 `succeeded`、`auth_wait` 为 `authentication_wait`、使用新凭证继续排队的 `pending` 为 `authentication_recovered`、停机/上下文取消后的 `pending` 为 `interrupted`、`retry_wait` 为 `retry_scheduled`、预算耗尽的 `failed` 为 `retry_exhausted`、其他 `failed` 为 `non_retryable`、`cancelled` 为 `cancelled`、`source_unknown` 为 `source_operation_unknown`。只有 `retry_scheduled` 写未来的 `next_attempt_at_utc`，值先规范成 UTC；否则为 null。若错误链中含现有 `baiduapi.APIError`，记录安全的操作名、数值 HTTP 状态和提供方错误码；不得写原始错误字符串。现有错误模型不足以可靠区分本地 I/O 与永久提供方错误，因此不在事件中臆分这些子类。

## 时间与证据规则

- `recorded_at_utc` 是本机持久化该事件时的 UTC 时间；不是线上发生时间。
- `cloud_observed_at_utc` 只表示成功云端扫描的观察时刻；不回填为 `occurred_at_utc`。
- `next_attempt_at_utc` 是未来调度计划，不是已发生时间，且只在 `retry_wait` 时记录。
- `response_observed_at_utc` 只适用于实际收到 HTTP 响应的事件；取消、手动重试和本地扫描状态变化不伪造响应观察时间。
- 不以任务 `created`/`updated`、文件 `mtime` 或数据库重启时间推断操作发生时间。旧任务没有的证据保持 null。
- 事件和对应任务快照继续在同一 SQLite 事务提交；事件表不可更新/删除。现有本机/线上时间字段语义不变。

## 不在本次范围

- 不改 SQLite schema、任务状态机、重试策略、取消/重试权限或云端改名/删除行为。
- 不新增事件查询 API、管理页、人工隔离解除入口或自动事件清理。
- 不把所有持久化状态变化统一事件化；只补以上关键事件。
- 不将无法分类的错误强行归入确定类别；使用 `unknown` 并保留现有安全行为。

## 验收标准

- 取消、手动重试、扫描恢复、源项缺失及下载成功/重试/最终失败各产生预期事件，事件与快照原子提交。
- 注入事务提交失败并重启重开数据库后，事件与任务状态同时回滚或同时存在。
- 模拟认证失效扫描、未成功扫描、扫描 epoch 过期时，不写入扫描恢复/源项缺失证据。
- 重试事件将下一次时间标为计划值；时钟回拨不伪造线上发生时间。
- 事件证据不含令牌、访问 URL 或原始错误字符串；现有 taskstore 事件查询可读到字段。
- 运行相关 Go 测试、race、vet，并核对 diff；不把本地数据库验证表述为 NAS/SMB 耐久验收。

## 实现记录

实现沿用现有 SQLite 事件表和 `persistWithEvent` 事务，不改 schema、状态机和重试策略。回归覆盖取消/手动重试、成功扫描恢复/源项不再具备资格、过期认证扫描不产生事件，以及 HTTP 错误响应观察时间和 UTC 未来重试计划。下载失败类别按现有状态分支映射；现有错误类型不足以再区分本地 I/O 与永久提供方错误时保留 `non_retryable`，不作推测。
