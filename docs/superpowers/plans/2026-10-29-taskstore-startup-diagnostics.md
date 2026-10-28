# SQLite 启动诊断实现计划

> **For agentic workers:** 按 `superpowers:executing-plans` 逐任务执行；步骤使用复选框跟踪。

**目标：** 每次服务启动时记录 SQLite 队列验证摘要，便于排查重启后的状态和持久化问题。

**架构：** Store 在成功打开后只读采集格式版本、队列范围的短指纹、任务/事件数量、最新事件序号、数据库大小、实际 pragma、当前运行 ID、启动时迁移动作及 UTC 采集时刻。主程序将结构化结果写成一条日志；不增加 HTTP 接口、不执行第二次完整性扫描、不改变事件或任务数据。

**技术栈：** Go、`database/sql`、现有 SQLite Store 与标准 JSON/logger。

**设计依据：** `docs/superpowers/specs/2026-10-28-taskstore-sqlite-design.zh-CN.md` 的“启动诊断”要求。

## 全局约束

- 保持 Windows AMD64、Linux AMD64、Linux ARM64 无 CGO 构建。
- 不在诊断日志输出云路径、下载路径、凭证、管理员口令或任务内容。
- 复用 Open 已完成的完整性验证结果；诊断过程不得再次执行全表 `integrity_check`。
- 诊断失败要明确记日志，不能伪报完整摘要；Store 已打开后不能因此清除或重建数据。
- 事件只读，未知线上操作保持隔离。

## 复核重点

- v1 迁移、首次建库和普通重启分别报告正确动作。
- scope 摘要可关联当前队列而不能反推出或直接泄露队列路径。
- 空库、已有任务、已有事件的计数及最大事件序号精确。
- pragma 是从已打开连接读取的实际值，且失败时不伪报成功。
- 诊断采集不增加一次完整性扫描，不能改变队列或事件。

---

### 任务 1：Store 诊断与启动日志

**文件：** `internal/taskstore/store.go`、`internal/taskstore/sqlite.go`、`internal/taskstore/sqlite_test.go`、`internal/taskstore/migration_test.go`、`internal/downloader/scheduler.go`、`internal/downloader/downloader.go`、`main.go`、SQLite 设计文档和进度文档。

- [x] 先编写并运行 Store 诊断测试，确认接口缺失时失败。
- [x] 在 Store 打开流程记录本次动作，并实现加锁、只读、脱敏的诊断快照。
- [x] 在下载器完成恢复后暴露快照；主程序只记录一条 JSON 结构化日志，诊断失败则记录明确错误。
- [x] 覆盖 fresh/migrated/reopened、非空计数、scope 脱敏及实际 pragma；Windows 全模块测试和 vet 通过。
- [x] Node WebUI 16 项和 userscript 3 项通过。
- [x] 发布门禁完成 Windows AMD64、Linux AMD64、Linux ARM64 无 CGO 构建。
- [x] `scripts/package.ps1 -Version v0.8.28-preview.1` 通过；复核最终差异与归档 payload 哈希。
- [x] 生成不可变预览 ZIP、SHA256、测试指南和验证记录。完整 race 因本机 cgo `exit status 2` 与 WSL `E_ACCESSDENIED` 未验证，已明确记录，未声称通过。
