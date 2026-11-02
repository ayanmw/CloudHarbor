# 下载磁盘空间安全与恢复实施计划

> **For agentic workers:** 按用户长期授权在主目录内联实施；不创建 worktree，不提交或推送。逐项按 RED/GREEN 验收。

**目标：** 在 Windows/Linux 下载目标卷不足以容纳文件剩余字节时不发起百度内容请求；协调并发空间使用，并将空间阻断或磁盘满写入明确的本地失败证据，保留安全续传能力。

**架构：** 新增 Windows/Linux 可用空间查询、按 fs_id 管理剩余字节 reservation 的 Downloader 本地协调器。在现有 partial 身份检查后、百度下载链接/内容请求前准入；写入推进时减少 reservation，所有返回路径释放。空间错误使用普通 `failed` + 现有手动 Retry，不安排 provider retry；复用 SQLite `download_attempt_finished` 事件，不改 schema。

**Tech Stack:** Go 1.22、`golang.org/x/sys`、现有 SQLite taskstore、Node WebUI tests。

**Spec:** `docs/superpowers/specs/2026-11-01-download-disk-space-safety.zh-CN.md`

## Global Constraints

- 支持包构建目标 Windows AMD64、Linux AMD64、Linux ARM64。
- 容量观察是本机 UTC 证据，不是百度观测时间。
- 身份匹配的 `content.part` 保持现有 Range 和 SHA-256 验证语义；空间不足不得截断或删除 partial。
- 不执行远端写入，不自动清理文件，不自动重试本地空间错误，不修改 SQLite schema。
- 单个 Downloader 内以并发 reservation 保护活动传输；已知上层共享路径策略时不得假称跨多个独立实例协调。

## Review Focus

- 已存在 partial 恰好等于目标大小但没有有效 SHA receipt：空间需求应为完整文件，预检失败不应先截断 partial。Task 3 覆盖。
- 多个并发下载中一个已写入大量字节：另一下载的有效容量不得因未更新 reservation 被重复计算。Task 2/3 覆盖。
- Windows ERROR_DISK_FULL/ERROR_HANDLE_DISK_FULL 与 Linux ENOSPC：归类为本地空间失败，不误判 provider retry。Task 3/4 覆盖。
- 配额/Statfs 查询失败或目标目录不可访问：fail closed，且不请求百度下载内容。Task 1/3 覆盖。
- 同 ID 但元数据身份不匹配的旧 partial：不得将其长度作为可续传 credit。Task 3 覆盖。

---

### Task 1: Windows/Linux 可用空间探测

**Files:**
- Create: `internal/downloader/storage_space.go`
- Create: `internal/downloader/storage_space_windows.go`
- Create: `internal/downloader/storage_space_linux.go`
- Test: `internal/downloader/storage_space_test.go`

**Interfaces:**
- Produces: `func localAvailableBytes(path string) (int64, error)`；返回当前用户可用字节数。测试可用 Downloader 内注入的查询函数，不改 package-global probe。
- Windows 使用 `GetDiskFreeSpaceEx` 的 caller-available 值；Linux 使用 `unix.Statfs` 的 `Bavail * Bsize`，并对溢出返回错误。

- [x] **Step 1: 写容量查询测试**，验证目录可用值为非负、路径不存在/不可查询返回错误；加入 mock probe 的 bytes/error 用例。
- [x] **Step 2: 运行测试确认失败**：`go test ./internal/downloader -run 'TestLocalAvailableBytes|TestDownloaderSpaceProbe' -count=1`；Expected: 缺少 `localAvailableBytes` 或探针注入实现。
- [x] **Step 3: 实现平台探针和 Downloader probe 注入点**；不新增依赖，复用现有 x/sys。
- [x] **Step 4: 重跑聚焦测试**；Expected: Windows 主机容量读取成功，注入错误原样传播。

### Task 2: 并发剩余容量 reservation

**Files:**
- Modify: `internal/downloader/downloader.go`
- Create: `internal/downloader/storage_space.go`
- Test: `internal/downloader/storage_space_test.go`

**Interfaces:**
- Consumes: Task 1 的 `localAvailableBytes`。
- Produces: `reserveLocalSpace(fsID int64, path string, required int64) (*localSpaceReservation, error)`；reservation 提供 `UpdateRemaining(int64)` 和幂等 `Release()`。

- [x] **Step 1: 写 RED 测试**：同一空闲容量不能被两个 reservation 重复占用；`UpdateRemaining` 后第二项可使用已写入释放出来的空间；释放后新 reservation 成功。
- [x] **Step 2: 运行容量 reservation 测试确认失败**：`go test ./internal/downloader -run '^TestLocalSpaceReservation' -count=1`；Expected: reservation 类型/方法未定义。
- [x] **Step 3: 实现 mutex 保护的活动剩余字节表**；读系统容量与检查/写入 reservation 在同一锁范围内，校验负值、重复 fs_id 和 arithmetic overflow。
- [x] **Step 4: 重跑 reservation 测试**；Expected: 并发准入容量不超额，更新和 release 均可恢复容量。

### Task 3: 下载前准入、续传空间和运行中 ENOSPC

**Files:**
- Modify: `internal/downloader/transfer.go`
- Modify: `internal/downloader/downloader.go`
- Modify/Create: `internal/downloader/transfer_test.go` 或现有下载器测试文件
- Test: `internal/downloader/downloader_test.go`

**Interfaces:**
- Consumes: Task 1 probe、Task 2 reservation。
- Produces: `localStorageSpaceError`，包含 kind、path、required/available/reserved bytes、checked_at_utc 和底层 cause；可由 `errors.As` 读取。

- [x] **Step 1: 写 RED 测试**：容量不足、容量探测错误时 0 次百度下载链接/内容请求；失败事件尚由 Task 4 增加。确认测试失败是因为当前代码已发起内容请求。
- [x] **Step 2: 运行准入回归确认失败**：`go test ./internal/downloader -run 'TestDownloadRejectsInsufficientLocalSpaceBeforeProviderRequest|TestDownloadRejectsUnknownLocalSpaceBeforeProviderRequest' -count=1`。
- [x] **Step 3: 写 RED 续传测试**：身份匹配 partial 的 required 为剩余 bytes；身份不匹配或无效全尺寸 partial 的 required 为整个文件；容量拒绝不截断原 partial。
- [x] **Step 4: 实现准入**：在 `transfer` 完成 partial 身份/offset 核对后、创建/截断 partial 和获取百度下载链接前预留 `size-offset`；每次成功写入后减少 reservation；defer 释放。
- [x] **Step 5: 写 RED ENOSPC 测试**：注入磁盘满写错误，要求显式 `local_storage_full` 类型、保留实际 written partial 字节、无 `complete.json`、不发布最终文件，reservation 最终释放。
- [x] **Step 6: 实现 ENOSPC 转换**：Windows 识别两个 Windows disk-full errno，Linux 识别 ENOSPC；只转换这些错误，其他 I/O 错误保留原分类。
- [x] **Step 7: 运行该任务测试**；Expected: 上述预检/身份/续传/并发磁盘满回归全部通过。

### Task 4: SQLite attempt evidence 和可见恢复提示

**Files:**
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/task_events_test.go`
- Modify: `scripts/webui.test.cjs`（仅当任务错误呈现回归需要新增）
- Modify: `release/TESTING.md`

**Interfaces:**
- Consumes: Task 3 的 `localStorageSpaceError`。
- Produces: `download_attempt_finished` evidence 中 `outcome_class=local_storage_blocked` 或 `local_storage_full`、`error_class=local_storage_space_unavailable`/`local_storage_insufficient`/`local_storage_full` 和 `local_storage` 对象（path、required_bytes、available_bytes、reserved_bytes、checked_at_utc）。

- [x] **Step 1: 写 RED event 断言**：Run event 包含本机 UTC 容量检查时间、目标本地路径和 required/available bytes；不创建 `ResponseObservedAtUTC` 或 `CloudObservedAtUTC`。
- [x] **Step 2: 运行 task event 测试确认失败**：`go test ./internal/downloader -run 'TestDownloadAttemptPersistsLocalStorageEvidence|TestLocalStorageErrorsDoNotScheduleProviderRetry' -count=1`。
- [x] **Step 3: 实现分类和 event evidence**：空间失败保持 `failed`，不匹配 provider retry；事件明确局部错误类别和容量字段，task.Error 使用简明中文动作提示。
- [x] **Step 4: 验证页面**：WebUI 测试确认失败 task 的中文容量提示按普通转义文本显示，仍暴露现有人工 Retry，不引入自动操作。
- [x] **Step 5: 重跑 task event/UI 回归**；Expected: 事件时间归属本机、容量数准确、敏感/HTML 文本仍安全转义。

### Task 5: `.121` 文档、完整门禁与 preview

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Create/Update: `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.30-preview.121-validation.md`

- [x] **Step 1: 完成五项路线图**，列明容量预检、剩余量续传、并发 reservation、本地空间失败证据、释放后手动重试。
- [x] **Step 2: 发布门禁**：`go test ./...`、`go vet ./...`、`node scripts/webui.test.cjs`、`node scripts/userscript.test.cjs` 和 `scripts/package.ps1 -Version v0.8.30-preview.121 -AllowDirty`。
- [x] **Step 3: 验证 preview**：载荷哈希、源码快照、Windows 版本命令、无凭据/运行数据路径、无 staging 残留和 `git diff --check`。
- [x] **Step 4: 只有门禁全通过才把 `.121` 标记已交付并记录边界；失败则继续更新 `.121`，不递增版本。**

## Pre-flight interface check

Task 1 produces the platform free-space probe used by Task 2 reservation and Task 3 preflight. Task 2 produces a remaining-byte reservation used by Task 3. Task 3 produces typed local storage errors consumed by Task 4 event evidence. No other shared interfaces.

## Builder self-check

Design self-review: Windows SMB and Linux mounted filesystem capacity are queried through OS APIs; unknown capacity fails closed. Only current Downloader reservations are coordinated; queue single-writer lock prevents two processes using the same queue state, but independent state roots that intentionally share a download directory are not coordinated. Partial identity, unknown/oversized partials and out-of-space writes are explicitly assigned tests.
