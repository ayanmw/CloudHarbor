# 网盘目录缓存扫描与重复哈希扫描互斥

## 问题

目录刷新和全盘列表扫描会改变用于 SHA-256 重复文件扫描的 SQLite 清单。重复扫描按 fs_id 游标分页；若清单扫描在哈希扫描运行中引入更小 fs_id 的新对象，哈希任务可能越过该对象仍结束为 complete。反向同时启动时，哈希结果也可能遇到处于扫描中的目录而无法完成持久化。

## 行为要求

- 同一 Downloader 中，全盘扫描、局部目录刷新和重复哈希扫描共用一个持久化任务准入槽。
- 任一槽位正在启动或运行时，其他两类启动在任何持久化状态更新前拒绝。
- 重启后仍未完成的 running/paused 状态也阻止另外类型启动；用户须先恢复原任务或让其完成。
- 正常完成后释放槽；暂停/失败状态保留在 SQLite，下一次启动只恢复同类任务。
- 不改变网盘写操作与重复扫描任务已有状态事件语义。

## 验收

- refresh running 时 duplicate scan 启动拒绝；duplicate scan running 时 full scan 和 refresh 启动拒绝。
- 只从 SQLite 恢复的 duplicate running 状态同样阻止 full/local inventory scan。
- 任一启动错误释放仅属于本次请求的 volatile starting 标记；已有任务继续占用槽位。
- 全仓调用与恢复测试通过。
