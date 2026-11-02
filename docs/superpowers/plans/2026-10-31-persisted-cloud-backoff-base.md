# 持久化云扫描退避基数实施计划

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax.

**目标：** 全盘扫描、目录刷新和重复扫描在重启后沿用下一档指数退避，不因进程重启重复使用旧退避间隔。

**方案：** 保持 `retry_delay_millis` 表示最近一次实际等待时长，新增独立的 `retry_backoff_base_millis` SQLite 状态列保存下一次失败使用的指数退避基数。三类扫描在同一轮退避状态转换中更新计划时长、UTC 截止和下一基数；旧版本迁移时以现有 retry delay 初始化新列。

**技术：** Go、SQLite (`modernc.org/sqlite`)、现有扫描测试 provider。

**范围约束：** 不改变百度请求/重试分类、不重发远端写操作；保留当前退避上限和 Retry-After 取较大值策略；不创建 worktree、不访问真实账号/NAS、不提交或推送。

## 任务

### 1. 钉住恢复后的下一档退避

- [x] 增加 downloader 回归：从已持久化退避状态恢复，首次重试失败后，持久化下一档基数必须高于当前档；覆盖全盘扫描、局部刷新和重复扫描。
- [x] 先运行测试确认当前实现无法保留下一档基数。

### 2. 扩展状态和 SQLite schema

- [x] 三种状态增加内部 `RetryBackoffBaseMillis` 字段；JSON 不暴露该内部计算值。
- [x] schema 25→26 为三张扫描状态表增加有界非负 `retry_backoff_base_millis`，既有行以 `retry_delay_millis` 初始化。
- [x] 更新 manifest/schema 校验和迁移恢复测试；状态保存、加载及关闭重开往返保留新字段。

### 3. 统一 worker 退避状态转换

- [x] 三类 worker 从持久化下一基数恢复；安排重试时原子保存实际等待、下一基数和 UTC 截止；到期后清除当前等待但不重复递增。
- [x] 成功处理后按现有策略重置退避；暂停/重启期间保留下一基数。
- [x] 运行扫描聚焦回归、taskstore 测试及全仓测试、vet、diff 检查。

### 4. 记录候选版本进度

- [x] 更新 `.107` 路线图和进度，保持事项按独立用户结果计数；未满 5 项不编号、不打包。
- [x] 修复重复扫描终止失败后仍保留旧重试时间/退避证据的问题，并以真实 SQLite 读回回归锁定。
