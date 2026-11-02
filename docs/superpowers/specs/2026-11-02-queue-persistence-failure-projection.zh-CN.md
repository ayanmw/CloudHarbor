# 队列 SQLite 写失败时的状态投影一致性

## 目标

队列任务状态以 SQLite 最近一次成功提交为持久事实。现有少数路径先修改内存 `queueJob`，再写 SQLite；写入失败时虽然队列进入 fail-closed 并停止调度，但有的路径仍保留新状态，导致管理页显示的状态与重启后 SQLite 恢复出的状态不同。本版统一修复五个独立的状态转换点。

## 行为要求

1. **领取下载任务失败：** `download_attempt_started` 保存失败时恢复领取前的状态、attempt 计数、AttemptID、重试时间、错误和容量摘要；不得反映成没有实际启动的 `downloading`。
2. **进入授权等待失败：** 当前任务的 `authentication_wait_entered` 保存失败时恢复其原状态、错误、重试截止和更新时间；已先前成功保存的其它任务仍保持已提交状态。
3. **扫描观察到云端身份变化但队列写失败：** 恢复原 `FileInfo`、状态、错误、source state、重试截止和更新时间；不把未保存的云端身份展示成当前队列身份。
4. **授权恢复任务失败：** `task_recovered_after_scan` 保存失败时仍显示原 `auth_wait`，不显示未真正入队的 `pending`，并保留旧错误/时间字段。
5. **下载尝试收尾写失败：** 不覆盖此前已独立成功保存的 `paused`、用户 `cancelled`、`auth_wait` 或 confirmed/unknown source-operation 状态；普通尝试结果仍按现有 fail-closed 诊断处理。

## 共同约束

- 每种写失败都保留现有 `q.err` 与取消 Downloader context 的 fail-closed 行为；不得继续调度或重新发出百度请求。
- 失败前状态来自 SQLite 最近一次成功提交；失败的事务不能被内存 UI 投影误表示为已提交。
- 候选/调度器停止后的可恢复性依赖重启加载的 SQLite 记录；本版不要求在队列已经 fail-closed 的同一进程内继续运行。
- 不新增 schema、不改变事件语义、不接触真实百度服务；UTC 云端观察、HTTP 响应和本机操作时间保持原有区分。

## 验收

使用关闭 SQLite 连接的定向失败注入，分别覆盖上述五个转换。断言内存状态、云端身份、错误、时间、计数及 source quarantine 与最近成功状态一致，同时确认队列错误已设置且不继续领取。再运行 downloader、全仓 Go/vet、WebUI/userscript、三平台构建和 preview 包核验。验收限本机与 SQLite/mock。
