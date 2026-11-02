> 状态：已实现并通过 v0.8.30-preview.148 发布验收。

# 局部目录刷新分页漂移证据

## 目标

在局部云目录缓存刷新遇到重复 `fs_id` 时，自动从该目录第一页重新核对；同时把触发重核的来源、原 offset 和重复对象 ID 保存到 SQLite，使运行状态、历史筛选和整轮证据能说明为何重核。

## 已有行为

- schema v31 已为全盘扫描和局部刷新提供跨重启的 `boundary_restart` 事件、每目录两次上限及持久请求额度。
- 局部刷新已识别百度单页重复 ID 和 SQLite staging 跨页重复 ID，并调用 `RestartCloudInventoryRefreshBoundary`。此行为是本候选批次第 1 项，尚未发布。
- 刷新事件已有 `restart_reason`，历史页能区分三类原因；但无法指出哪一个 `fs_id` 重复，也不记录触发问题的 offset。

## 五项用户结果

1. 对百度单页重复 ID 或已暂存页间重复 ID，局部刷新从第一页自动重核；最多两次且维持请求额度预留、旧缓存发布和 staging 安全。（现有实现并入本批，补齐全量回归）
2. `boundary_restart` 事件保存发生问题时的原 offset；历史旧事件保持 NULL，不能推测过去。
3. 重复 ID 可确定时，事件保存该 `fs_id`；无法确定的边界身份变化不伪造 ID。
4. 管理页可按固定重核原因筛选局部刷新事件；筛选贯穿事件分页，清除后返回完整时间线。
5. 事件行显示原因、原 offset 和重复 ID；显式整轮导出包含相同字段并保持账号指纹排除。

## 存储与 API

- SQLite schema v39→v40，仅向 `cloud_inventory_refresh_events` 增加可空的 `restart_offset` 和 `restart_fs_id` 列；表继续 append-only，迁移保留序号、历史值、索引和触发器。
- 只允许 `boundary_restart` 事件填写 offset/ID；offset 必须大于零，fs_id 必须为正整数。旧事件和无重复 ID 的边界重核两列均可为 NULL。
- `baiduapi.ErrDuplicatePageItem` 和 `taskstore.ErrCloudInventoryDuplicateAcrossPages` 保持 `errors.Is` 兼容，并提供可安全读取的 typed fs_id；调用层不得从错误字符串解析 ID。
- Store 重核事务接收原 offset、原因及可选 fs_id，并在同一事务内核对次数、清除本目录 staging、重置 checkpoint、写入事件。
- 刷新事件 API 增加固定枚举 `restart_reason` 精确筛选，可与现有 `kind` 组合；历史页面只筛当前运行事件。整轮完整证据导出仍导出该 run 的全部事件，并带上新增 nullable 字段。

## 安全边界

- 每次百度列表 GET 仍须在调用前耐久预留请求额度。
- 重核未完成前保留已发布本地缓存；不得触发百度远端写操作或下载。
- 数据库迁移不得重写历史时间、事件序号、账号作用域或既有事件 JSON。
- `fs_id` 是整数证据，不导出 token、账号指纹、目录身份边界 JSON 或请求 URL。

## 验收

- provider 单页重复和 staging 跨页重复均写入准确原因、原 offset、重复 fs_id，并重核后只在完整目录成功时发布。
- 纯 boundary mismatch 记录原 offset，不写入不存在的重复 fs_id。
- 两次重核后第三次重复失败关闭且不发额外 GET；请求额度不足时保留重核事件和 offset，恢复仍从第一页开始。
- 注入迁移/重核事务写失败时，不出现部分 checkpoint、staging 或事件变化。
- v39→v40 迁移保留历史事件序号和 NULL 语义，append-only trigger、integrity 和新查询过滤通过。
- API 拒绝未知或重复原因参数；按 reason 与 kind 组合筛选、分页和完整证据导出行为正确。
- WebUI 旧事件显示“过去未记录”，不把 NULL 渲染为 offset 0 或 fs_id 0；用户数据安全转义。
- 全仓 Go 测试/vet、WebUI/userscript、三平台构建、ZIP/SHA/source snapshot 和 `.147→.148` 本机状态切换通过。

## 非目标

- 不保证百度分页期间的远端原子快照。
- 不更改全盘扫描既有证据 schema/API，不新增云端操作入口，不允许用户按 ID 自动删除/移动远端文件。
- 不在版本门槛未达成前发布；已完成项目内联实现，不创建 worktree、不提交或推送。
