# 百度请求退避与重复内容核验可靠性设计

## 意图

在现有低频请求和 SQLite 扫描检查点基础上，补齐重复内容扫描、授权账号核验与网页只读诊断中未共享的请求节流/重试语义；防止内容摘要缓存把文件元数据已变化的对象继续作为重复项展示或删除候选。

## 当前确认的缺口

源码审查确认五处行为差异：

1. 全盘扫描和目录刷新会采用 `APIError.RetryAfter`，重复内容扫描的指数退避仍忽略百度建议。
2. 全盘扫描、目录刷新和重复内容扫描对 UTC 重试截止时间只创建一次完整时长的 timer；运行期间系统时钟前跳后，worker 可能继续等待旧时长。
3. 重复内容扫描的下载前/下载后元数据复核不比较百度 `MD5`，本地已核验 SHA-256 的写入条件也没有再次约束缓存 MD5。
4. `credentials.BaiduProvider.Account` 会单独创建 `baiduapi.Client` 验证 `UserID`，没有接入 Downloader 的共享 metadata pacer。
5. `/api/cloud` 和手动入队预检的安全只读错误已显示 HTTP/百度错误码，但未显示已解析出的 `Retry-After` 等待建议。

## 目标与验收条目

本批为 5 项独立用户结果：

1. **重复扫描尊重服务端退避**：429/可重试服务端错误的有效 `Retry-After` 与本地指数退避取较大值，最长 1 小时；实际等待毫秒、UTC 截止时间及错误原因写入现有 SQLite 检查点，暂停/重启继续遵守截止时间。
2. **扫描截止时间响应时钟调整**：全盘扫描、目录刷新和重复内容扫描以最多 30 秒间隔重读 UTC 截止时间；时钟前跳后尽快恢复，时钟后退时不提前运行，取消仍可即时暂停。
3. **重复文件摘要绑定可用的百度 MD5 证据**：在下载前、下载后及 SQLite 保存时，只要库存和实时 metadata 两侧都提供 MD5，就必须大小写无关地相等。若仅 MD5 与缓存不同而 fs_id/path/size/mtime 相同，使用实时 MD5 更新本地库存并删除该项旧 SHA-256；本轮扫描安全停止，用户可重新扫描新内容。其他身份字段不同时不在此处猜测更新，沿用目录刷新/安全失败。MD5 缺失继续遵守现有 fs_id/path/size/mtime 核验规则，不虚构证据。
4. **授权账号读取共享请求 pacer**：默认 BaiduProvider 的 `UserID` metadata GET 经过当前 Downloader 的同一个取消感知 pacer，避免授权验证请求绕过后台请求间隔。
5. **只读失败显示百度建议等待时间**：只读网盘列表及入队预检的结构化错误在 `RetryAfter > 0` 时显示向上取整的秒数，并提供 `Retry-After` 响应头；不改变 HTTP 错误状态、不自动重试、不将此提示套用到远端写操作。

## 非目标

- 不调整 pacer 间隔，不对远端写操作自动重试。
- 不把百度端对象更新等同于本地缓存观测；MD5 为空时不宣称内容身份已被 MD5 证明。
- 不为现有 provider MD5 新建持久化字段；库存已有 `content_md5`，沿用其变化时删除对应 SHA-256 缓存的语义，并在哈希前后/持久化边界补齐验证。
- 不执行真实账号远端写操作或大文件 hash scan；NAS/SMB 文件系统验收仍需目标设备。

## 方案与取舍

1. 将每种扫描器的重试和时钟逻辑独立实现：改动局部，但三种扫描会继续漂移。本方案不选。
2. 让三类扫描复用同一 UTC 截止等待 helper，并仅让重复内容扫描补用现有 `APIError.RetryAfter`；改动聚焦，可用确定性单测覆盖，也不改变既有退避上限。**采用此方案。**
3. 把所有云端读取集中到新的全局 API 调度服务：会扩大并发、生命周期和凭据组件重构范围，超过已确认缺口。本方案不选。

MD5 采用现有库存字段作为核验来源，不做 schema 迁移；避免把提供方元数据重复存一份。遇到两个非空 MD5 不匹配时安全失败，触发已有失败/刷新路径，不覆盖线上对象或悄悄重用旧摘要。

## 文件边界

- `internal/downloader/cloud_inventory_scan.go`：统一重试截止等待与重复扫描 Retry-After 选择。
- `internal/downloader/cloud_inventory_scan_test.go`、`cloud_duplicate_retry_test.go`：时钟/退避恢复回归。
- `internal/downloader/cloud_duplicate_scan.go`、对应 downloader 测试：实时 MD5 身份比对。
- `internal/taskstore/cloud_duplicates.go`、`cloud_duplicates_test.go`：摘要写入条件绑定库存 MD5。
- `internal/credentials/manager.go`、`manager_test.go`、`internal/downloader/auth.go`、`main.go`：默认账号验证 provider 接入 Downloader pacer。
- `internal/intakeapi/server.go`、`server_test.go`：安全只读失败提示与标准响应头。
- `internal/webui` 自动化测试仅在错误消息需有额外浏览器处理时调整；现有页面读取纯文本错误，不预期需要产品界面改动。
- 更新 `docs/ai/release-versioning.zh-CN.md`、`docs/ai/cloud-management-roadmap.zh-CN.md`、`docs/ai/progress.md`、`release/TESTING.md`，并更新同级 `.98` 验证记录。

## 验收范围

- 定向 Go 测试先证明缺口，再证明修复；全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI/userscript 检查及 Windows AMD64/Linux AMD64/Linux ARM64 构建通过。
- `.98` ZIP 仅在五项功能均完成且发布门禁通过后覆盖/创建；核验 ZIP 载荷哈希、源码快照、私有凭据排除和包内版本号。
- 用当前真实账号只读启动复核 health、账号绑定、任务数、目录读取与缓存状态；不执行云端写操作、重复清理或下载。
- 本机代码/测试不声称代替 NAS/SMB 验收。
