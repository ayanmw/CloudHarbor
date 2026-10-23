# 大队列任务分页索引 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对名称/大小排序的重复任务分页使用可失效的轻量 ID 索引，并复用全局任务计数摘要，避免每次轮询都扫描全队列。

**Architecture:** 在 downloader 的现有队列/任务锁内惰性构建 name/size ID 排序切片，按版本号复用；计数摘要单独按状态版本复用。进度仍走现有全量扫描和有界堆。新增/元数据与状态变更通过原子 generation 使相应缓存失效。

**Tech Stack:** Go 1.22+ 标准库、现有 `sort.Slice` 与 downloader 测试/基准。

**Spec:** `docs/superpowers/specs/2026-10-24-task-page-indexes-design.md`

## Global Constraints

- 保持 `taskCandidateComesBefore` 的排序次序和既有 API/WS 分页字段不变。
- 不改变持久化 JSON v1、任务历史保留、调度、取消、重试或源操作语义。
- 仅 name/size 排序可使用缓存索引；progress 排序必须读取当前进度。
- 返回的 Counts 为调用方所有，不能暴露可修改的内部缓存 map。

## Review Focus

- 任务状态和集合在缓存建立后变化：下次 health/page 必须显示当前全局计数。
- 云端元数据变化同 FsID：旧 name/size 索引必须失效。
- 非根任务进入/改变状态：总数和状态计数不得漏算。
- progress 排序在缓存建立后进度变化：仍须按最新进度排序。
- 大 FsID、相同排序键和大小写文件名：分页顺序必须与旧 comparator 一致。

---

### Task 1: 定义缓存失效 generation 和摘要读取

**Files:**
- Modify: `internal/downloader/downloader.go`
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/auth.go`
- Modify: `internal/downloader/intake.go`
- Test: `internal/downloader/task_page_test.go`

**Interfaces:**
- `Downloader` 新增原子 task-catalog、task-stats generation；队列缓存保存 generation、`total` 和内部 counts。
- `GetTaskStats() (int, map[string]int)` 保持签名，缓存命中时复制 counts map 返回。

- [x] **Step 1: 编写 RED 测试**：缓存 health/page 摘要后，覆盖持久化 job 状态变化、非根 task 新建/状态变化和调用方修改返回 map；验证后续结果准确。
- [x] **Step 2: 运行聚焦测试确认失败**：`go test ./internal/downloader -run 'Test.*Task.*(Stats|Counts)' -count=1`。
- [x] **Step 3: 加入 stats generation 与惰性摘要缓存**：在 job 持久化状态变更、task 集合增加、非根 task 状态变更时原子失效；摘要在既有双锁下重建，Counts 每次复制。
- [x] **Step 4: 运行 downloader 测试**：验证 RED 覆盖与既有 stats 行为通过。

### Task 2: 惰性 name/size 排序索引

**Files:**
- Modify: `internal/downloader/downloader.go`
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/intake.go`
- Test: `internal/downloader/task_page_test.go`

**Interfaces:**
- scheduler 按需保存 name/size 排序的 `[]int64` 和相应 catalog generation。
- `GetTaskPage` 对 name/size 使用缓存切片窗口；progress 保留原候选堆算法。

- [x] **Step 1: 编写 RED 测试**：验证排序缓存建立后新任务和同 ID 元数据更新会使缓存失效，并与全量排序在多 offset、tie-breaker 上一致。
- [x] **Step 2: 运行聚焦测试确认失败**：`go test ./internal/downloader -run 'TestGetTaskPage' -count=1`。
- [x] **Step 3: 建立按需排序索引**：索引仅保存 FsID；排序键提取规则沿用现有 comparator。入队集合变化和 reconcile 元数据替换时增加 catalog generation。
- [x] **Step 4: 接入分页窗口并运行 downloader 测试**：name/size 切片直接复制当前页视图，progress 扫描逻辑不变。

### Task 3: 基准、全量验证与交付

**Files:**
- Modify: `internal/downloader/task_page_test.go`
- Modify: `docs/ai/experience-plan.md`
- Modify: `docs/ai/progress.md`

- [x] **Step 1: 增加 benchmark**：覆盖 10,000/100,000 条历史的 name/size 冷建与热命中、progress 动态扫描、stats 命中；分开报告分配量。
- [x] **Step 2: 验证**：Windows `go test ./...`、`go vet ./...`、WSL `go test -race -count=1 ./... && go vet ./...`。Node 与发布构建由打包门禁覆盖。
- [ ] **Step 3: 打包并实际启动独立预览包**：核对三平台构建、payload 哈希、Linux AMD64 版本/revision、认证与分页、合成大队列、优雅关闭及端口释放。
- [ ] **Step 4: 更新计划/进度与验证记录并提交**：明确冷建仍为 O(n log n)、进度排序仍为 O(n log k)，只报告实际测量边界。
