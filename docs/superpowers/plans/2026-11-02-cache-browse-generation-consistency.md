# 本地缓存浏览分页代次一致性实施计划

> **For agentic workers:** Execute inline in the primary checkout; preserve existing dirty files, do not use a worktree, and do not commit or push.

**Goal:** 让本地缓存目录浏览和搜索的分页绑定一个 SQLite 缓存代次，变化后安全回到第一页。

**Architecture:** 复用账号级 SQLite generation。Store 在单个事务内读取 generation、条目、总数和目录大小；API 对陈旧代次返回固定 409；WebUI 清除旧 token 并重读第一页，迟到响应由请求/上下文标识丢弃。

**Tech Stack:** Go、SQLite、net/http、嵌入式 WebUI、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-11-02-cache-browse-generation-consistency.zh-CN.md`

## Global Constraints

- 不新增 SQLite schema 或运行时文件。
- 仅改变本地缓存浏览/搜索分页；实时百度浏览与远端操作契约保持不变。
- 代次不是远端快照证明；保留现有缓存覆盖状态和本机/百度时间语义。
- 在主 checkout 内联实施；保留现有 dirty/untracked 工作，不使用 worktree，不提交或推送。
- 新 preview 累计至少 5 条独立可验收结果后再编号。

## Review Focus

- 不存在 generation 行时，按代次 0 正常提供首次缓存浏览。
- 代次更新与旧页读取并发时，响应不能混合两代数据。
- 缓存搜索和目录浏览使用各自一致的代次检查。
- 用户反复切换模式/目录/搜索时，旧响应不覆盖当前上下文。
- 连续扫描不断推进 generation 时，冲突只触发一次回首页重读，不形成重试循环。

## Task 1：Store 事务与代次检查

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`.

- [x] 为目录浏览增加可选 expected generation；返回目录元数据中的实际代次。
- [x] 将条目、总数、目录大小和 generation 读取放入同一事务快照。
- [x] 为搜索增加相同代次响应和冲突校验；定义 sentinel `ErrCloudInventoryGenerationChanged`。
- [x] 添加 RED/GREEN 回归：首次代次 0、同代次翻页、扫描/远端缓存发布后旧代次拒绝且无数据、大小与列表代次一致。

## Task 2：Downloader/API 契约

**Files:** `internal/downloader/cloud_inventory_scan.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`.

- [x] 目录/搜索方法透传 expected generation。
- [x] 列表响应和搜索响应返回 `cache_generation`。
- [x] 旧代次返回 HTTP 409 JSON `{code:"cache_generation_changed",cache_generation:<current>}`，不返回 entries。
- [x] 验证认证门禁、无参数兼容、无 generation 更新与过期 generation 的 API 行为。

## Task 3：缓存浏览/搜索上下文

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] 缓存目录和搜索分页分别记录当前代次，下一页请求携带 token。
- [x] 409 时清除 token、将 offset 设为 0、提示缓存已更新并只重读一次。
- [x] 目录、搜索词、模式变化时重置分页代次；迟到响应不得覆盖当前上下文。
- [x] 添加 UI 回归覆盖目录页、搜索页、冲突恢复、重复冲突不循环和慢响应跨上下文。

## Task 4：文档与候选路线

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/progress.md`, 本计划/设计。

- [x] 把 `.130` 从重复的未完成候选改为已交付，不修改历史包。
- [x] 将本批记为 `v0.8.30-preview.131` 候选 5/5，并记录逐项验收证据。
- [x] 同版打包；在进度与 validation 记录中分开本机、真实账号只读、NAS/SMB 边界。
