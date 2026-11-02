# 已确认网盘删除后的本地队列重启对账

## 目标

如果百度已同步确认删除，但进程在 SQLite 保存对应本地任务取消事件前退出，下一次成功完成监控目录核对时，应依据持久化的 confirmed 删除回执把精确匹配的未执行任务恢复为 cancelled。

## 已核实的问题

`Downloader.MutateCloud` 先将远端删除持久化为 `confirmed`，之后调用 `cancelConfirmedCloudDelete` 更新 SQLite 任务及事件。如果任务事件写入失败，方法返回错误且任务仍为 pending。重启后的普通监控目录核对只看到源项缺失，现会把该任务记录为 `failed / task_source_missing`；远端操作历史却明确是 confirmed delete，导致两个本地页面对同一对象给出不一致结果。

## 设计

- 只在成功读取完整监控目录、认证 epoch 仍有效的 `reconcileObserved` 中处理缺失任务；不在 API 列表失败或部分读取时推断删除。
- 只对当前账号下持久化状态为 `confirmed` 的 delete 操作匹配 `object_id == job.Info.FsID` 且 `source_path == job.Info.Path` 的任务。不得用名称或目标路径代替身份匹配。
- 只转换没有活动 worker、没有源后处理状态、attempts/failures 均为 0、状态为 `pending` 或 `auth_wait` 的未执行任务；`retry_wait` 不属于安全重绑定状态。保留任务 ID、下载策略、尝试计数和原始身份快照。
- 通过既有任务与 append-only 事件同事务写入，把该任务标为 `cancelled`，事件类型为 `task_source_deleted_remotely`，记录操作 ID、响应观察 UTC 和本机记录 UTC。失败时保留旧任务状态并让后续成功核对重试。
- 已有对应操作事件的删除不重复写事件；unknown/failed 操作、账号不符、fs_id 或源路径不符、活动/已处理任务继续走原有保守逻辑。
- 不调用百度 mutation，不删除任务历史，不新增 schema。

## 验收

- SQLite trigger 使删除后的本地取消事件写入失败；远端操作 journal 仍 confirmed，任务原样保留。
- 关闭并重开 Downloader 后，成功的空监控目录核对依据该回执将任务 cancelled，并写关联 operation ID、response UTC 与本机记录 UTC 的单条事件；不写 `task_source_missing`。
- 账号、fs_id、路径、状态、活动任务和未知/失败操作不匹配时不会被此恢复路径更改。
- 不向百度重发删除；同一恢复再次运行不追加重复的删除事件。
- 通过 downloader/taskstore 定向回归、全仓 Go/vet、WebUI/userscript、三平台打包检查。

本设计仅处理可由本机 SQLite 中 confirmed 同步回执证明的删除，不推断异步任务终态，也不改变 unknown 隔离策略。
