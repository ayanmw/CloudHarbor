# SQLite 重启只做一次完整性检查实施计划

> **For agentic workers:** 按 `superpowers:executing-plans` 逐任务执行；步骤使用复选框跟踪。

**目标：** 保持 SQLite 重启失败关闭和完整性验证，同时消除正常重启时重复执行的全库 `integrity_check`。

**架构：** 区分新库初始化与权威现有库验证。新库路径创建 schema/meta 并执行一次完整性检查；已有库路径绝不创建或补回 schema，只配置并核验 pragmas、检查必需 schema/meta/scope，再执行一次完整性检查。正常 `Store.Open` 直接走已有库验证，不先单独完整检查再重新打开。

**技术栈：** Go、`database/sql`、`modernc.org/sqlite v1.34.5`。

**设计依据：** `docs/superpowers/specs/2026-10-28-taskstore-sqlite-design.zh-CN.md` 的 fail-closed、打开时 integrity/schema/scope/pragmas 核验要求。

## 全局约束

- 保持 SQLite schema v2、事件只追加、v1 原始记录无损迁移及 `sending` 恢复隔离语义。
- 权威数据库缺表、版本或 scope 不符时失败关闭，不能 `CREATE IF NOT EXISTS` 修复。
- Windows AMD64、Linux AMD64、Linux ARM64 无 CGO 构建继续通过。
- 不降低 `synchronous=EXTRA`，不跳过或延后 integrity check。

## 复核重点

- 删除权威库必需表后重启仍失败，且缺表不会被自动重建。
- 权威数据库不存在、scope/版本错误和完整性错误仍失败关闭。
- v1 迁移阶段创建库仍初始化完整 schema，发布后能正常重开。
- 正常重启路径只调用一次完整性检查；100k 基准与旧样本用相同机器、Go 工具链、夹具形状及计时范围比较。

---

### 任务 1：拆分建库与权威库校验

**文件：** `internal/taskstore/sqlite.go`、`internal/taskstore/store.go`、`internal/taskstore/migration.go`、相关 taskstore 测试、`docs/ai/experience-plan.md`、`docs/ai/progress.md`。

- [x] 添加权威库缺表不修复的回归（先因缺少 existing-open API 编译失败，再通过）；现有 Store schema 损坏测试仍覆盖启动失败关闭。
- [x] 将 SQLite 打开逻辑拆分为初始化模式与现有库校验模式；现有库模式先验证 schema/meta/scope，不创建表、索引、触发器或 meta 项。
- [x] `Store.Open` 的 SQLite 格式直接走一次现有库校验；迁移临时库关闭后以现有库模式重开校验。
- [x] Windows 全模块 Go 测试/vet、WebUI 16 项、userscript 3 项和 taskstore/downloader 定向测试通过；三平台构建纳入下方发布门禁。
- [x] Windows Go 1.27.1、Intel Core Ultra 7 265KF、100k 合成历史：改动前 `Store.Load` 3 次 609–674 ms，中位数 613 ms；改动后 5 次 459–526 ms，中位数 476 ms，B/op 与 alloc/op 基本不变。样本含主库打开、integrity check 和全量任务读取，不代表 NAS。
- [x] 生成 v0.8.29-preview.1 不可变预览包；12 项归档/11 个 payload 哈希核对通过，包内连续创建/重开诊断冒烟通过。全量 race 未验证并记录环境限制。
