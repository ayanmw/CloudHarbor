# Plan: 网盘缓存不完整目录清单

## Task 1：SQLite 稳定清单

- 文件：`internal/taskstore/cloud_inventory.go`、`internal/taskstore/cloud_inventory_test.go`。
- 新增 filter/page/entry DTO 与 `ListCloudInventoryIncompleteDirectories(account, beforePath, status, query string, limit int, expectedGeneration *int64)`。
- [x] 先写回归：状态目录、缺失状态、结构不完整路径、关键字/状态过滤、稳定分页、generation 冲突和账号隔离；先运行确认失败，再实现。
- [x] Store 按路径游标稳定分页并返回总数、generation、目录状态及观测时间；generation 冲突拒绝旧页。

## Task 2：Downloader/API

- 文件：`internal/downloader/cloud_inventory_scan.go`、`internal/downloader/cloud_inventory_scan_test.go`、`internal/intakeapi/server.go`、`internal/intakeapi/server_test.go`。
- [x] 绑定当前授权账号；认证 GET `/api/cloud-inventory/incomplete-directories`，验证 limit/reason/q/cursor/generation 并以固定 JSON DTO 返回；测试 Provider 零调用及指纹不泄漏。

## Task 3：管理页

- 文件：`internal/webui/index_cloud.html`、`scripts/webui.test.cjs`。
- [x] 仅用户打开详情/点击筛选或翻页时调用本地清单 API；呈现状态/UTC证据，响应 generation 冲突回第一页；旧响应不能覆盖新筛选。
- [x] “在缓存中打开”只切换缓存浏览；“填入刷新路径”只填输入框，保持原有刷新按钮的显式操作边界。

## Task 4：发布

- [x] 更新 `.135` 路线、版本表与进度记录；全仓测试/vet、WebUI/userscript、三平台包、ZIP/SHA/私有数据门禁；按既有授权保持真实账号运行数据为单份，完成只读启动验收并生成验证记录。

## 全局限制

- 不用 worktree、不提交 Git、不删除现存 smoke-test 目录。
- 列表、筛选和分页不得调用百度。实际局部刷新需用户在页面点击已有刷新按钮后才发送百度列表请求。
- 结构计数仅是本地缓存诊断，不暗示云端是原子快照。
