# Plan: 网盘缓存覆盖不完整原因分布

**Goal:** 在同一 SQLite 快照中返回账号隔离的缓存覆盖原因计数，并在网盘管理页展示，帮助用户判断下一步刷新范围。

**Architecture:** 扩展 `CloudInventorySearchCoverage` 的嵌套 JSON 投影；由 `cloudInventoryCoverageInTx` 复用的事务查询构造分布。API 继续使用现有扫描状态端点，UI 只读显示，不新增百度 API 调用。

**Files:** `internal/taskstore/cloud_inventory.go`、对应 Store 测试、`internal/downloader/cloud_inventory_scan.go`、Downloader/API 测试、`internal/webui/index_cloud.html`、`scripts/webui.test.cjs` 和本计划/规格/发布路线文档。

## Task 1: 持久层原因分布与账号隔离

- [ ] 先添加 Store 测试：fixture 创建各状态目录与五类结构异常；断言计数、同一账号隔离、遗漏状态行可见。
- [ ] 运行目标测试确认因缺少字段/查询而失败。
- [ ] 增加明确 JSON 字段和事务查询；保留原 `IncompleteDirectories` 算法。
- [ ] 运行 Store 定向测试。

## Task 2: API 状态投影

- [ ] 扩展 Downloader/API 回归：原字段兼容、新分布嵌套字段准确，数据库错误不返回陈旧摘要，无 Provider 调用。
- [ ] 先运行失败，再实现最小接线并运行定向 Go 测试。

## Task 3: WebUI 原因说明

- [ ] 添加 UI 测试，断言所有原因类别和“类别可重叠”说明，且不得出现额外 provider 请求。
- [ ] 先确认失败，再渲染摘要和详情文字；读取失败时显示当前不可读及上次成功摘要。
- [ ] 运行 WebUI 全量和 userscript 检查。

## Task 4: 路线、门禁与发布

- [ ] 更新 roadmap、版本表和进度文档，将 `.134` 定为 5 项主题版本。
- [ ] 运行 `go test ./... -count=1`、`go vet ./...`、WebUI/userscript、发布构建与 ZIP/源码/私有数据检查。
- [ ] 记录 `.134` 源码/ZIP 哈希和真实账号只读验收；仅在现进程无任务时移动唯一 `.nas-downloader`，不做百度远端写入。

## 风险与边界

- 结构问题计数条件可重叠，必须在 UI 明示，不能相加解释为总量。
- 报表不会自动修复缓存；实际重扫会产生百度列表请求，须由用户在页面明确启动。
- NAS/SMB、Linux ARM64 原生执行和人工浏览器体验仍需独立环境验证。
