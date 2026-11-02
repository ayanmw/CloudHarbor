# 重复文件扫描运行历史与证据实施计划

> **For agentic workers:** 按项目约束在主目录内联实现；不要创建 worktree，不提交或推送。

**目标：** 为重复文件扫描提供可跨重启查看的按轮历史摘要、筛选、事件分页和有界导出。

**架构：** 新增账号/run 唯一 SQLite 摘要投影；状态保存时与现有 checkpoint/追加事件同事务更新。schema 32→33 从旧事件回填可证明证据，并以当前 SQLite 检查点校准当前 run；HTTP 用显式 DTO 按账号查询，页面按需选定 run 后再加载事件或导出。

**Tech Stack：** Go、SQLite (`modernc.org/sqlite`)、现有 HTTP API、内嵌 HTML/JavaScript、Node WebUI tests。

**Spec:** `docs/superpowers/specs/2026-11-01-duplicate-scan-run-history.zh-CN.md`

## Global Constraints

- 历史查询按 `account_fingerprint` 隔离，但任何 HTTP/JSON 输出不得包含该指纹。
- 状态 checkpoint、关键 append-only 事件及摘要投影属于同一 SQLite 事务；不逐文件/逐页产生事件。
- 历史导出按显式 run ID、有界、单 SQLite snapshot、超限全拒绝。
- 旧事件未保存的历史进度不得推断；不得导出 SHA-256 流状态。
- 不访问百度 API，不执行云端写入，不创建 worktree，不提交/推送。

## Review Focus

- 迁移面对当前 run checkpoint 比最后事件更新：迁移回归证明只用 checkpoint 校正同一 run。
- 旧事件缺少进度字段：迁移回归证明使用未知/默认值，不回填伪造计数。
- 投影触发器故障：注入摘要写失败并证明状态与事件一起回滚。
- 过滤与分页期间新增 run：稳定 start sequence cursor，不重不漏。
- 账号切换/超量导出/迟到 UI 响应：不得跨账号泄露、部分导出或覆盖新选择。

---

### Task 1: schema 33 和运行摘要迁移

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/migration.go`, `internal/taskstore/store.go`, `internal/taskstore/migration_test.go`

- [x] 写迁移回归：从真实 schema 32 事件表迁移，含两轮、不同账号、当前检查点比事件更新、旧 evidence 缺字段；验证 schema 33、sequence、索引/trigger、账号隔离和 `PRAGMA integrity_check`。
- [x] 先运行新增测试并确认失败，再实现 `cloud_duplicate_scan_runs`、32→33 回填与当前检查点校准。
- [x] 重跑迁移回归及旧 schema 链迁移测试。

### Task 2: 状态保存原子更新 run 投影

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`

- [x] 测试 transition 与非 transition checkpoint 均更新 run 摘要；关闭重开后列表进度一致。
- [x] 注入摘要 upsert 失败，验证 duplicate state 与 event 不部分提交。
- [x] 实现投影同步 helper；新 run 从其开始事件建立摘要，旧 run 不受当前账号状态覆盖。

### Task 3: Store run 查询和导出

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`，必要时新增 `internal/taskstore/cloud_duplicate_history.go`

- [x] 增加状态/UTC 日期过滤、稳定 `start_sequence` 游标分页及有界摘要导出回归。
- [x] 增加显式 run ID 的事件分页与单轮完整证据导出回归，含账号隔离及全有/全无上限。
- [x] 验证投影时间与事件时间均返回 UTC，查询不访问 provider。

### Task 4: HTTP/API 显式 DTO

**Files:** `internal/downloader/cloud_duplicate_scan.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`

- [x] 为 run list、summary export、selected-run events、selected-run evidence export 增加鉴权后的 handler/Backend 接口。
- [x] 测试非法重复参数、状态/日期、cursor、跨账号 run ID、超限导出及 fingerprint/SHA-state 排除。
- [x] 仅在错误路径和空结果上增加响应测试，不改变当前扫描/删除接口。

### Task 5: 云管理页历史浏览体验

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] 按需加载历史 run，按状态/UTC 日期筛选、稳定分页；当前 run 状态区保持原用途。
- [x] 选中 run 后仅显示该 run 的事件，并支持更早事件分页和显式 JSON 导出。
- [x] 覆盖迟到请求、筛选重置游标、事件转义、导出超限/失败、未点击不下载。

### Task 6: 完整门禁、路线图和 `.120` 预览包

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, release validation record

- [x] 执行 `go test ./... -count=1`、`go vet ./...`、WebUI/userscript 与三平台发布脚本。
- [x] 验证 ZIP/载荷哈希、源码快照、版本命令、无私有运行数据及无 staging 残留。
- [x] `.120` 仅在五项结果和全门禁通过后交付；记录未执行真实百度账号/NAS/SMB 验收。

