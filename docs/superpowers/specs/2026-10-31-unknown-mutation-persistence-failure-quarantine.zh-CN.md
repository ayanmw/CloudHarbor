# unknown 结果持久化失败时继续隔离来源

## 背景

百度写请求已返回不确定错误后，Downloader 会把 `prepared` 更新为 `unknown`。若该 SQLite 更新本身失败，当前流程直接返回；内存队列仍可能把来源当成可下载项目，远端缓存也可能保持完整，直到进程重启恢复遗留的 `prepared`。

## 方案

只要远端写请求结果未知，即使 SQLite 无法持久化 unknown 状态，也立即在内存队列按 fs_id 和源/目标路径隔离对象，并尽力将对应本地缓存标为 `stale`。返回对象状态按 `unknown` 展示，并明确说明本地审计写入失败；不重发远端请求。数据库行仍为 `prepared` 时，现有启动恢复会在下一次打开时将其转为 `unknown` 并事务性失效缓存。

## 验收

- SQLite trigger 注入状态写失败后，远端 mutation 只调用一次，来源任务保持原身份但立即处于内存隔离。
- 缓存可失效时立即 stale；即使未成功，网页也不会将结果误报为已知失败或允许来源继续。
- 关闭 trigger 后重启，遗留 prepared 转为 unknown，缓存 stale；不自动重发。
- 本机 mock/SQLite 验证，不访问真实百度或 NAS/SMB。
