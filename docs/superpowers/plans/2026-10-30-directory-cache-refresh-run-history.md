# 目录缓存刷新运行历史实施计划

> 由用户持续推进 preview 路线图的授权执行；不使用 worktree、不访问真实账号、不提交 Git。

**目标：** 让网盘管理页浏览持久化的所有目录刷新运行，并按选择读取指定运行的事件时间线。

**实现：** SQLite schema 20 为启动事件增加账号/事件类型/序号索引；Store 以不可变 `started` 序号稳定分页，并返回每轮最新事件摘要；认证 API 从当前授权账号查询；页面按需呈现运行列表并复用现有事件 API。

**Spec：** `docs/superpowers/specs/2026-10-30-directory-cache-refresh-run-history.zh-CN.md`

## 约束

- 运行列表游标固定使用 `started` 事件序号，恢复事件不会移动分页位置。
- 账号指纹只能来自认证上下文；运行摘要和事件响应都不序列化该字段。
- `started` 事件之前的历史状态不合成历史记录。
- 运行列表和事件查询均不发起网盘请求。
- 保留现有工作区的其他改动；只处理本次涉及的文件。

## 任务

### 1. Store 与 schema 19→20

- [x] 先加失败测试：多轮/多账号排序与游标、恢复后摘要更新而启动游标不变、跨重开读取、19→20 与 manifest 恢复。
- [x] 实现账号/事件类型/序号索引、迁移和 runs 查询（每轮最新事件摘要）。
- [x] 定向 taskstore 测试通过。

### 2. Downloader 与只读 API

- [x] 先加 API 失败测试：账号隔离、参数/重复值/页大小校验、稳定游标、不泄露指纹。
- [x] 增加受保护的 `/api/cloud-inventory/refresh/runs?before_sequence=…&limit=…` 与 downloader 只读适配；保留旧事件 API。
- [x] 定向 downloader/intakeapi 测试通过，确认仅查询 SQLite。

### 3. 网页运行选择器与发布

- [x] 先加 WebUI 测试：默认不请求、显式加载运行列表、选中旧 run 加载事件、运行/事件游标各自稳定、转义和过期请求保护。
- [x] 增加最近运行列表与独立“更早运行/更早事件”操作；记录请求失败且保留重试入口。
- [x] 更新 `experience-plan.md`、`progress.md`、网盘管理路线图和版本路线图，记录 `.88` 与验证边界。
- [x] 全量 go test/vet、WebUI/userscript、三平台 package 门禁；复核 ZIP 清单、SOURCE-SNAPSHOT、私有路径及最终 diff。

## 最终审查重点

- 一次新运行、恢复旧运行或追加新事件不得造成运行页重复/跳过既有启动游标边界。
- 不同账号不能通过 run ID 猜测读取运行摘要或事件。
- 读取摘要不能覆盖运行状态或请求远端列表。
- 老版本迁移不伪造 `started` 事件，且升级中断后再次打开可完成恢复。
