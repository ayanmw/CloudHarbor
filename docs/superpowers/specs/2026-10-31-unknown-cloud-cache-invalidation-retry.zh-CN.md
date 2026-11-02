# unknown 远端操作的缓存失效重试

## 背景

远端操作被持久化为 `unknown` 后，`MutateCloud` 会尝试将源/目标目录缓存标为 `stale`，但当前忽略失效错误；启动恢复也不会再次检查已是 `unknown` 的记录。SQLite 暂时写入失败后，页面可能继续把受影响缓存显示为完整。

## 方案

使用独立本地事件 `local_cache_recovery_complete` 作为“受影响缓存已安全失效”的耐久标记。unknown 操作在当前请求中失效成功后追加标记；失效或标记写入失败时保留未完成状态，追加可行的失败证据，并由下一次启动事务性重试。prepared 启动恢复在同一事务写入 unknown、失效路径、恢复事件和标记；confirmed/unknown 的未完成恢复共用缓存失效逻辑。所有这些只操作 SQLite，不查询或重发百度写请求，不改变 unknown 状态，也不解除远端对象隔离。

## 验收

- 本地失效失败后的 unknown 操作重开时将对应目录变为 stale，并记录本地 UTC 恢复事件。
- 缓存失效成功的 unknown 操作具备完成标记；重开不重复增加 generation。
- SQLite 恢复事务失败时不写标记，随后可重试收敛。
- unknown 操作状态、缓存条目和内容哈希保留；其他账号不受影响。
- 只做本机 SQLite/mock 验证，不访问真实百度或 NAS/SMB。
