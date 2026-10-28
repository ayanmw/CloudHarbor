# 持续优化进度

## v0.8.28-preview.1（增加 SQLite 启动诊断）

- SQLite 成功打开并完成既有完整性检查后，服务启动日志新增一条 JSON 摘要：run ID、schema 版本、云队列范围的 64 位短指纹、任务/事件计数、最大事件序号、数据库文件大小、当前启动动作、已验证完整性结果、实际 journal/synchronous/foreign_keys/busy_timeout 和 UTC 采集时间。摘要不输出队列路径、任务内容或凭证，也不再次全表扫描、不写数据库。
- 回归先因 StartupDiagnostics API 缺失而失败，接线后通过；覆盖首次创建、v1 迁移、普通重开、非空计数、scope 不直出、pragma 和只读行为。Windows 全模块 Go 测试/vet、WebUI 16 项、userscript 3 项及 Windows AMD64/Linux AMD64/Linux ARM64 无 CGO 构建通过。
- ZIP 内 Windows AMD64 无百度凭证实际启动；认证 health 为 `waiting_authorization`，启动日志中 schema=2、jobs/events/序号=0、integrity=`ok`、journal=`delete`、synchronous=3、foreign keys=true、busy timeout=5000、迁移动作=`created`。ZIP 12 项，11 个 payload 哈希一致。
- 全量 race 未验证：Windows cgo 工具退出码 2，Ubuntu-24.04 WSL 返回 `Wsl/Service/E_ACCESSDENIED`。真实百度、NAS/SMB 锁与刷盘、掉电、实机浏览器/油猴及长期容量仍待目标环境验收。
- 独立包：[v0.8.28-preview.1.zip](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.28-preview.1.zip)，SHA256：`9BA885EE00C12F001EA1FFF3D512494B1530A0BCCB7AF9B7397399110B9F4ECF`；验证记录：[v0.8.28-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.28-preview.1-validation.md)。源码提交：`a59d58b8c094d3511d61dbe03a9a709823b1e834`。

## v0.8.27-preview.1（限制事件时间线的浏览器内存）

- 修复事件页“更早事件”累积历史项的问题：前后翻页时替换当前 20 项，而不是把所有浏览过的事件加入同一 DOM；使用游标栈返回较新页面，点击刷新回到最新页。一次只保留一个展开任务的事件状态。
- Node 回归先在旧实现上因翻页后仍同时保留新旧事件失败；修正后 WebUI 16 项与油猴 3 项全绿。回归明确断言当前事件数组不随翻页增长、同时展开另一任务会释放旧面板状态。
- 独立包门禁：Windows 全模块 Go 测试、`go vet`、WebUI/油猴 19 项和 Windows AMD64/Linux AMD64/Linux ARM64 无 CGO 构建通过。
- 包内版本/revision：`v0.8.27-preview.1` / `1bffe4e301fb244bcafbf0bed364d9a2b98db48b`。实际 Windows 包无百度凭证启动后 health=`waiting_authorization`；嵌入资源包含较新页导航和单任务面板限制。ZIP 有 12 项、11 个 payload 哈希一致，SHA256 `8B37CB59FE15A7FA90886CF03CEA73C745E65E8B5C561B358B055AA6E364A685`。
- 独立包：[v0.8.27-preview.1.zip](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.27-preview.1.zip)；验证记录：[v0.8.27-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.27-preview.1-validation.md)。补跑全量 race 时 WSL 返回 `Wsl/Service/E_ACCESSDENIED`，Windows race 被当前环境 Go build cache `Access is denied` 与链接器子进程退出码 1 阻断，因此仍未验证 race；未测试真实百度、NAS/SMB 与实机浏览器。

## v0.8.26-preview.1（管理页核查 SQLite 任务事件）

- 新增管理员认证保护的只读 `GET /api/tasks/{id}/events`，按任务读取最新事件，默认 20 条、最多 50 条，使用 exclusive `before_sequence` keyset 游标；事件按 sequence 正序展示。空历史返回空数组，不存在的任务返回 404。
- WebSocket 与轮询任务列表的根任务行新增“查看事件”，首次展开才读取；支持刷新和读取更早事件。时间字段分别标明本机记录、提供方发生、云端扫描观察、HTTP 响应观察及未来重试计划，并原样保留 UTC；结构化 evidence 转义为文本。没有事件修改/删除、跨任务全库 API 或隔离放行功能。
- SQLite 回归验证重开数据库、连续翻页、查询期间新事件追加后旧游标稳定、空历史、任务隔离和参数边界；HTTP 回归覆盖管理员认证、匿名 401、无效参数 400、未知任务 404、非 GET 405 和下一页 cursor；WebUI/油猴共 19 项通过。
- 打包门禁：Windows 全模块 Go 测试、`go vet`、WebUI 16 项和油猴 3 项通过；Windows AMD64/Linux AMD64/Linux ARM64 无 CGO 构建通过。将 Node 两文件测试拆为独立进程，避开当前 Windows 受限环境下 `node --test` 多文件 runner 的 `spawn EPERM`。
- 包内 Windows 程序版本为 `v0.8.26-preview.1`，revision `56aac264d9eb1e7214c5634a7e0f36246b55fb9e`；无百度凭证启动冒烟显示 `waiting_authorization`，匿名 health 401、未知 task events 404、打包事件 UI 资源 200。ZIP 含 12 项，11 个 payload 与校验清单/归档内容一致；SHA256 `BD1ECD5FE613735A4EA4A28DA2C9568AB030F5AE6DD0AB22C9FB80A05A575BE6`。
- 独立包：[v0.8.26-preview.1.zip](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.26-preview.1.zip)；证据：[v0.8.26-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.26-preview.1-validation.md)。
- 没有真实百度账号或 NAS/SMB 环境验证；真实云端链路、目标文件系统锁/刷盘与掉电耐久、实机浏览器和长期运行仍需目标环境验收。全量 Go race 未在本预览包流程中执行。

## v0.8.25-preview.1（补齐 SQLite 关键任务事件证据）

- 新增取消、手动重试、成功扫描后的鉴权恢复/源项不再具备资格事件；下载尝试事件增加稳定结果分类、可用的 HTTP/提供方错误码、HTTP 响应观察时间和 UTC 未来重试计划。任务投影与事件仍同事务提交，不改 schema、状态机、重试策略或管理页接口。
- 自动化验证：Windows 全仓 Go 测试/vet、Node 17 项通过；Go 1.22.2 Ubuntu-24.04 WSL 全模块 race 通过。一次全模块 race 的既有退避用例曾遇时序超时，单项重跑与之后的完整 race 重跑通过。Windows race 因本机 cgo 子进程退出码 2 未能验证。
- 打包脚本的 Windows AMD64、Linux AMD64、Linux ARM64 无 CGO 构建通过；ZIP 12 个文件与发布目录一致、11 个 payload 哈希匹配，ELF 架构分别为 x86-64/AArch64。包内 Windows 冒烟验证版本/revision、匿名 401、管理员认证 health=`waiting_authorization`、无百度凭证启动及停止后端口释放；未做真实云端操作。
- 源码提交：`5b654b955a29a65b274403b6513a453c1f95ae4d`。独立包：[v0.8.25-preview.1.zip](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.25-preview.1.zip)，SHA256：`5FE241409EFD177C3A918C7B9732FDBDADBD1E62D8C843F857FBDD2C8D7A0978`；验证记录：[v0.8.25-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.25-preview.1-validation.md)。
- SQLite 事件仍未开放管理 API/UI 查询；目标 NAS/SMB 耐久、真实百度链路、浏览器/油猴实机和长期运行仍待环境验收。

## 2026-10-28 任务状态查询接口证据更新

- 同类工具 BaiduPCS-Rust 的变更记录说明其对异步 `filemanager` 结果调用 `taskquery` 轮询；公开的旧版网页脚本展示 `/share/taskquery` 并携带 `bdstoken`。这确认网页会话路线存在相似用法，但不能证明该接口接受本项目 OAuth access token，亦不能证明其终态可作为当前源操作的完成凭据。
- 百度官方 Go SDK 的 `filemanager` 响应模型标注 `async=2` 返回 `taskid`，其公开 SDK 文件管理方法未列出任务状态查询；官方 MCP 工具列表同样未列出该查询。故维持既有安全行为：未知源操作继续隔离，不按路径或目录可见性推断完成，不提供人工放行。
- 资料链接与边界已补入 [SQLite 设计文档](../superpowers/specs/2026-10-28-taskstore-sqlite-design.zh-CN.md)。关键任务事件证据设计与实现记录见 [task-event-evidence.zh-CN.md](../superpowers/specs/2026-10-28-task-event-evidence.zh-CN.md)。

## v0.8.24-preview.1（补齐旧格式迁移事件时间证据）

- 修复 `legacy_import` 事件只记录 `source_format`、没有原任务时间字段的问题。现在事件保留 v1 原始 JSON 的 `created`/`updated` 值，来源缺字段时明确写 `null`；仅将本机导入观察时刻记为 `recorded_at_utc`，不伪造 `occurred_at_utc`。任务原始 JSON 未改变。
- TDD：迁移回归先失败，指出事件证据只有 `{"source_format":1}`；实现后，带时间、含非 UTC 时区以及缺失时间三种用例通过。迁移中断重试和副本保留回归仍通过。
- Windows 全模块 `go test -count=1 ./...` / `go vet ./...` 通过；Go 1.22.2 WSL 全模块 `go test -race -count=1 ./...` 通过；Node 17 项通过；Windows AMD64/Linux AMD64/Linux ARM64 无 CGO 构建通过。
- 包内 Windows 程序从临时 v1 队列实际启动并完成迁移；匿名 health=401、Basic 认证 health=200，任务总数 2、状态等待授权。SQLite `integrity_check=ok`；事件读回原始时间 `2026-10-20T00:00:00Z` 与 `2026-10-21T12:34:56+08:00`，缺失时间读回 null。未配置百度凭证或执行云端操作。Ctrl+C 后日志 `Service stopped`，18091 端口无监听；PowerShell 外层 PTY 返回码 1，不宣称外层 shell exit 0。
- ZIP 11 项 payload SHA256 匹配，包内 Windows 版本/revision 匹配；ZIP 大小 15,134,541 字节，SHA256 `CA3F591EF52AE13B2A5D8B5C2ED9C9EAE72D277658B2FB911D647BDEF7C47254`。源码 revision `3da7cb61ac46429bea375f8c4a2efb7ef54355ac`。独立包：[v0.8.24-preview.1.zip](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.24-preview.1.zip)，验证记录：[v0.8.24-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.24-preview.1-validation.md)。
- 真实 NAS/SMB 锁与刷盘、百度真实账号链路、以及尚未确定的事件细化范围仍待后续处理；本包仅验证本机迁移行为。

## v0.8.23-preview.1（SQLite 任务历史与重启证据）

- 任务当前投影迁入 SQLite，状态快照与追加事件在同一事务提交；事件使用单调序号并分离本机记录时间、云端元数据观察时间、线上响应观察时间、提供方 mtime 和未来重试时间。源操作的 sending 在网络请求前落盘；重启将未决发送恢复为 unknown 并隔离，不自动或人工重放。启动恢复、下载尝试和云端扫描证据均通过独立 run/operation/attempt ID 关联。
- v1 任务 JSON 经暂存数据库、完整性/范围/记录数和逐 ID 原文等价验证后切换权威清单；旧 JSON 与 intake/凭证/账户/设置 sidecar 保留。损坏 SQLite、scope/版本不符和耐久设置不符时失败关闭。
- Windows Go 1.27.1 全模块 `go test -count=1 ./...`、`go vet ./...` 通过；Go 1.22.2 WSL 全模块 `go test -race -count=1 ./...` 通过；Node 17 项通过；Windows AMD64/Linux AMD64/Linux ARM64 无 CGO 构建通过。Windows AMD64 合成基准（5 次，Intel Core Ultra 7 265KF，夹具创建不计时）：10k `Store.Load`/`OpenDownloader` 均值约 61/72 ms，100k 约 698/739 ms；100k 恢复后保留堆单次测量约 35.07 MiB（367.8 B/任务）。旧版本记录的 Go 工具链未与本次固定一致，因此不把跨版本比值作为严格 A/B；结果也不代表目标 NAS/SMB。
- 本机 100,000 个最小合成任务与 100,000 条通用状态事件占用 36,716,544 字节（约 35.02 MiB），`integrity_check=ok`；每项平均约 367 字节。完整云端文件信息与操作证据会增加事件体积，此数据只是容量基线。迁移后 v1 文件仍保留，旧格式副本暂未清理。
- 对 100,000 条真实结构形状的合成 v1 JSON 做完整迁移：创建文件 45.73 s，迁移/完整重开等价校验 34.17 s；源 JSON 逻辑长度 23,766,685 字节，SQLite 41,881,600 字节，切换后源目录仍有 100,000 个文件，两者合计约 65.65 MB。SQLite 完整性及任务/事件计数为 `ok`、100,000/100,000。此为单次 Windows 本机测量；目标 NAS/SMB 迁移耗时和实际分配量未测。
- 解压后的 Windows 包从独立目录启动并创建 SQLite 状态库；匿名 health=401，Basic 认证 health=200，状态显示 `waiting_authorization` 和包内版本/revision。Ctrl+C 后服务日志报告 `Service stopped`，18090 端口不再监听；PowerShell 外层 PTY因 Ctrl+C 返回 1，因此不宣称外层 shell exit 0。未提供真实百度凭证，也未进行云端操作。
- 本机 10,000 条 SQLite 写入基准显示逐条各自提交约 52.83 秒，单次 `SaveBatch` 事务约 212 毫秒；10k/4,096/1,024 记录分批的一次性样本约 274/279/329 毫秒（第二组使用最小 payload，不与前组作严格 A/B）。因此入队批量路径继续合并事务，下载和线上副作用状态仍逐项先提交，避免异步攒批造成证据缺口。
- 11 个 payload SHA256 与清单匹配；ZIP 与发布目录 12 个文件一致；Linux AMD64/ARM64 ELF64 machine 分别为 62/183；Windows `-version` 输出的版本/revision 与清单一致。
- 源码 revision `37aa64a7b13d4f314942aaa3842a0c7d9c06c45c`。独立 ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.23-preview.1.zip`，15,132,493 字节，SHA256 `68DF6B28D423EBABD959DBABCE469F5E3EB72FC3E70C80EC277F74777187C14A`。详细验证：[v0.8.23-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.23-preview.1-validation.md)。
- 真实 NAS/SMB 的锁、刷盘与断电恢复、真实百度异步完成查询契约、浏览器/油猴实机兼容及长时间运行仍未验收；本包不等同这些环境的认证。

## v0.8.22-preview.1（精简启动恢复的派生状态同步）

- 启动恢复将持久化的 `downloading` 任务改为 `pending` 后，恢复记录都处于非活动态，且派生 `Downloader.tasks` 表为空；移除每条记录上的 `reflect` 调用，避免逐条加锁并删除空表项。任务查询仍以 scheduler job 映射为持久化来源，运行期活动任务仍由既有 `reflect` 维护。
- 现有恢复回归验证重开后的单任务视图、列表、分页和统计，并确认启动时不物化派生任务对象。Windows 全模块 `go test -count=1 ./...` / `go vet ./...` 与 WSL Go race 全模块测试/vet 通过。Windows 10,000 条启动基准各 3 次方向性样本约 1.11 s → 0.90 s；样本和缓存条件不足以宣称稳定端到端提速。
- Windows AMD64、Linux AMD64、Linux ARM64 构建及 ZIP 11 项 SHA256 清单通过；Linux AMD64 包内版本/revision、ARM64 ELF 架构、认证 health=`waiting_authorization`、匿名 401、SIGTERM 后正常退出通过。验证记录：[v0.8.22-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.22-preview.1-validation.md)。
- 源码 revision `2a8ba81eea5a4884c8c5fcfeab409a7bfbd3d28f`。独立 ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.22-preview.1.zip`，SHA256 `0EF7E5FC108D2621F7CC32F3DB776010548B7A245527A405E9DB9B3E1D2E5FB5`。
- 未使用真实百度凭证或执行云端操作；真实 NAS/SMB 启动性能仍待目标环境验证。

## v0.8.21-preview.1（加速大队列启动时目录项排序）

- taskstore 恢复记录前仍按文件名排序，以维持稳定回调顺序；将反射式 `sort.Slice` 改为 Go 1.22 泛型 `slices.SortFunc`，排序语义不变。ext4 合成 100k DirEntry 的 sort-only 中位数约从 18.3 ms 降到 13.0 ms，3 次分配降为 0。100k 原始记录串行加载的中位数约从 323.5 ms 降到 302.7 ms（约 6.4%，各组有明显波动）；Windows 10k 并行读仍由逐文件 I/O 主导，未观察到稳定的整体增益。
- Windows 全模块 Go test/vet、WSL Go 1.22.2 全模块 race/vet、Node 17 项和三平台构建通过。ZIP 解压后 11 个 payload 哈希匹配；Linux AMD64 包内版本/revision、ARM64 ELF 架构、认证 health=`waiting_authorization`、匿名 401、SIGTERM 后正常退出通过。
- 源码 revision `56b9ede8b161cd0e45c947f112e7f7fb0013540a`。独立 ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.21-preview.1.zip`，10,119,693 字节，SHA256 `18DE3EC7EDCDC0DB0F590CA3D5F612CF5471415F9053445DB653451DB5AA4E48`；验证记录：[v0.8.21-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.21-preview.1-validation.md)。
- 未使用真实百度凭证或执行云端操作。真实 NAS/SMB、大队列目标设备启动时间、Docker/ARM64 实机及长期运行尚未验收。

## v0.8.20-preview.1（全局预算约束目录文件 worker）

- 复现并修复 v0.8.19 多根目录时每个目录都创建 `MaxParallel` 子 worker 的放大问题：4 个目录各 4 个文件、全局上限 4 时，旧实现让 16 个文件都进入 downloading，实际只允许 4 个传输。现在目录必须先预留 Downloader 级传输槽位再创建 worker；没有可用槽位的目录调用方顺序等待，不创建多余的文件 goroutine。活动传输和并发文件 worker 均受全局 `MaxParallel` 约束。
- RED/GREEN 回归覆盖多目录等待 worker 数、同目录传输并发、跨根峰值上限、MaxParallel=1、普通失败聚合、认证失败停止排队、取消停止新任务并等待活动请求。固定延迟 8 小文件（每个本机 HTTP 响应 10 ms）对照单样本为串行 164.817 ms/op、MaxParallel=4 52.056 ms/op；约 3.2 倍，非真实百度/NAS 吞吐数据。
- Windows 全模块 Go 测试/vet、WSL Go 1.22.2 全模块 race/vet、Node 17 项通过；Windows AMD64、Linux AMD64、Linux ARM64 构建通过。ZIP 解压后 11 个 payload 哈希匹配。Linux AMD64 包内版本/revision、AArch64 ELF 架构、认证 health=`waiting_authorization`、匿名 401、SIGTERM 后 `Service stopped` 和退出码 0 通过。
- 源码 revision `fa52963771eb206ad61a9caf7707c60cdf3d39e6`。独立 ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.20-preview.1.zip`，10,119,925 字节，SHA256 `F4964E5BE526B7B27DCFFBE8DE8AF1A37CDD9BBBD624A80998B848717753ED18`；验证记录：[v0.8.20-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.20-preview.1-validation.md)。
- 未使用真实百度凭证或执行云端操作。真实百度限流、NAS/SMB、Docker/ARM64 实机和长期运行仍未验收；unknown 目录仍持续隔离且无人工放行。

## v0.8.19-preview.1（目录内文件有界并行下载）

- 单个目录内的直接文件可按 `MaxParallel` 使用固定 worker 并行传输；所有根任务和嵌套目录共用同一 Downloader 级传输槽位，不突破全局并发上限。目录递归仍逐层进行，父目录只在所有文件成功后完成并进入原有源项后处理。
- 新增回归覆盖同目录并行、多个根目录共享上限、`MaxParallel=1` 顺序行为、普通子项失败后的兄弟继续下载、认证失败停止继续排队、取消停止新任务并等待活动请求结束。独立的 8 小文件本机 HTTP 固定延迟对照，每个响应 10 ms：串行 172.077 ms/op，并行 4 为 55.664 ms/op，单样本约快 3.1 倍；这不是百度或 NAS 吞吐数据。
- Windows `go test -count=1 ./...` / `go vet ./...`、WSL Go 1.22.2 `go test -race -count=1 ./...` / `go vet ./...`、Node 17 项通过；Windows AMD64、Linux AMD64、Linux ARM64 构建通过。ZIP 解压后 11 个 payload 哈希匹配；Linux AMD64 包内启动、版本/revision、认证 health、匿名 401、SIGTERM 正常退出及 ARM64 ELF 架构检查通过。
- 源码 revision `fb05fef87163d1a1ab5e13b42dcaeb985a127d35`。独立 ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.19-preview.1.zip`，SHA256 `75A848FDEDC3E0DC1DB0C1A23083E641182D5964598B87160469D31253B7D4DA`；验证记录：[v0.8.19-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.19-preview.1-validation.md)。
- 没有真实百度凭证或云端操作。真实百度限流、NAS 吞吐/耐久、多日运行和突然断电未验收。源项改名/删除仍在发送前重读 `fs_id` 元数据核对身份，但核对到执行间存在云端竞态；结果未知且缺少完成凭据的目录持续隔离，不提供人工放行。

## v0.8.18-preview.1（批量持久化新发现的队列任务）

- 云端扫描发现多个新根任务时，先暂存完整扫描结果；最多每批 1,024 条、最多 4 个并行写入 worker。每条记录仍单独写入并 `Sync`，然后原子重命名；每批只同步一次 jobs 目录。全部批次成功后才写入内存队列/候选堆，既有任务状态更新仍单条持久化。
- 中途持久化失败会停止调度且不发布本次扫描的内存任务；批次可能已有部分完整记录落盘，但源项目尚未进行后处理，重启后扫描可恢复已写记录并补齐缺项。
- WSL Go 1.22.2，10,000 项新任务 reconcile：逐项持久化基线单次 17.94 s；当前三次 4.78/4.15/3.90 s，约快 3.8–4.6 倍。Windows taskstore 10,000 条逐项 Save 与批量 SaveBatch 的单次对照为 34.38 s 与 26.89 s，约快 22%；Windows 完整 reconcile 当前单样本 23.59 s，无同机前版本基线。数据为本机合成文件，不代表 NAS/SMB。
- Windows 全模块 Go test/vet、WSL Go 1.22.2 全模块 race/vet、Node 17 项、三平台构建通过。11 个 payload 哈希匹配；Windows/Linux AMD64 版本/revision、Linux ARM64 ELF 架构、Linux AMD64 包内启动/认证 health/匿名 401/SIGTERM 正常退出通过。未使用百度凭证或执行云端操作。
- 源码 revision `0b864b70aa08e580f8ce45accddc03631fa2b14b`。独立 ZIP：10,107,235 字节，SHA256 `FA33C439C963A4ED0A2227E0827090DA35A1A106111F66946D1FC8C787E2F781`；验证记录：[v0.8.18-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.18-preview.1-validation.md)。
- 未使用百度凭证或执行云端操作；真实 NAS/SMB durability 与长队列扫描验收仍待用户环境。

## v0.8.17-preview.1（缓存稳定历史进度排序）

- 进度页缓存非下载中任务的排序 FsID；当前下载任务每次分页按最新进度排序，再与稳定索引合并。保持原有完成文件/其他状态进度比率、FsID 同分顺序、offset/limit、全局计数和接口响应不变。状态或任务集合持久化变化使缓存失效，实时进度 tick 不重建索引。
- 100,000 条合成历史，WSL Go 1.22.2：热页三次 100x 约 9.4–15.9 μs/op，38,171 B/op、106 allocs/op；索引冷重建三次 100x 约 112.7–118.8 ms/op，841,299–841,300 B/op、111 allocs/op，紧凑键优化后冷重建三次 30x 约 32.9–36.2 ms/op、3,241,596–3,241,599 B/op、113 allocs/op。热页较 6.87–7.34 ms/op 基线约快 450–780 倍；变化后首次页有全量排序成本。数据为本机合成队列，不代表 NAS 绝对时延。
- 新增实时进度不失效稳定索引、非活动持久化状态变化使索引重建的回归，并增加 progress_cold 基准。Node 17 项、Windows 全模块 Go test/vet、WSL Go 1.22.2 全模块 race/vet、三平台构建通过；11 个 payload 哈希匹配，Windows AMD64 版本/revision 与 ARM64 ELF 架构正确。Linux AMD64 包内启动、认证 health、匿名 401、SIGTERM 退出通过。
- 源码 revision `a9c457c8be1069eda64dcf58e4d8978647b7970b`。独立 ZIP：SHA256 `73CBE5D3E43E77123AC04F205E3DD6F394E83F14A68CBD33658EC7643A06E825`；验证记录：[v0.8.17-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.17-preview.1-validation.md)。
- 未使用真实百度凭证或执行云端操作；真实百度/NAS、目标文件系统和长时间运行仍待验收。

## v0.8.16-preview.1（Windows 大队列记录并行读取）

- Windows 恢复队列时以最多 4 个固定 worker 并行读取任务文件，按文件名顺序依次校验和交给 visitor；内存只保留固定数量的在途记录，并复用每个 worker 的缓冲区。保持现有 JSON v1 文件、单写入锁、每条记录原子替换和 Windows 重解析点检查不变。
- 新增并行读取与顺序回调回归；保留首个回调错误、记录大小、JSON 校验、symlink 拒绝及 `LoadEachRaw` 回调生命周期测试。基准筛选调整为仅在选中子基准时创建对应历史夹具，避免检查 10k 时意外构造 100k 条记录。
- Windows 10k 原始记录读取单样本：串行约 2.61 s，4 worker 约 1.32 s；完整 `OpenDownloader` 样本约 1.41/1.64 s。WSL ext4 上 4 worker 的原始读取比串行慢约 20%，因此 Linux/其他平台保持串行；真实 NAS/SMB 效果未验证。
- v0.8.15 Go 1.22.2 状态字符串优化仍保留；WSL 10k 启动约 59–62 ms，100k 三次约 610/617/634 ms。系统负载和文件系统会影响绝对值，测试数据不能替代真实 NAS。
- 发布源码 revision `47b88bf564bd5961a8168c6bddbdce23c952c4c9`。独立 ZIP：10,078,700 字节，SHA256 `4EFBFACDC74F9522A7563A7336848B12DC620E7B9D8376B31643C415EE269465`；11 项 payload 哈希匹配。Linux AMD64 包内启动、认证 health、匿名 401、SIGTERM 正常退出及 ARM64 ELF 检查通过。
- Node 17 项、Windows 全模块 Go test/vet、三平台构建、WSL Go 1.22.2 全模块 race/vet 通过。没有使用真实百度凭证或执行云端操作。
- 验证明细：[v0.8.16-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.16-preview.1-validation.md)。

## v0.8.15-preview.1（减少大队列恢复后的枚举字符串常驻内存）

- 恢复任务记录时复用合法状态与 keep/delete 模式的常量字符串，避免 Go 1.22 JSON 解码为每条任务分别保留相同枚举值。持久化格式和业务行为不变；新增回归验证两个不同文件路径未发生输入缓冲区别名，且恢复后的枚举字符串共享。
- WSL Go 1.22.2，100,000 条合成任务：保留堆 323.4 → 307.3 B/任务（30.84 → 29.31 MiB），减少约 1.53 MiB/5.0%。最新三次启动基准 753.5/548.4/543.1 ms，波动明显；不据此宣称启动稳定提速，临时分配基本不变。Go 1.27 默认 JSON 解码实现的行为不同，回归和对照均以最低支持版本为准。
- 源任务改名/删除前会重新查询并核对 FsID 对应的完整 `FileInfo`；发现缺失或身份变化时持久化为 `changed/failed`，不发送源操作、不允许重试旧身份。用户已选择保留自动处理但在操作前复核身份；复核与实际云端请求之间仍存在接口无法消除的竞态。结果未知的目录继续隔离，不提供人工放行。
- Node WebUI/userscript 测试、Windows 全模块 Go test/vet、WSL Go 1.22.2 全模块 race/vet、三平台打包构建通过。Linux AMD64 包内启动/认证 health、匿名 401、SIGTERM 与 ARM64 ELF 检查见独立验证记录。
- 未使用真实百度凭证或执行云端操作；真实百度/NAS、目标文件系统和长时间运行仍待验收。
- 验证明细：[v0.8.15-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.15-preview.1-validation.md)。

## v0.8.14-preview.1（加速 Windows 队列恢复并保持重解析点检查）

- Windows 每条任务记录恢复改为通过 `FILE_FLAG_OPEN_REPARSE_POINT` 单次打开，再从已打开句柄检测符号链接；不先 `Lstat` 再 `Open`，缩短路径查找并消除两步之间替换链接的竞态窗口。其他平台行为不变。
- Windows 单文件 A/B（5 次、每次 5,000 次）中位数：旧 `Lstat + Open` 约 522 μs/次，新句柄检查约 191 μs/次，约减少 63%。完整 10,000 条 `OpenDownloader` 三次为 2.764/2.726/2.758 s，对 v0.8.13 一次 4.581 s 样本约快 40%。完整 100,000 条三次为 36.01/45.83/36.79 s；对 v0.8.13 一次 122.34 s 样本约快 70%，但 Windows 文件系统波动大、基线只有单次，数字只代表本机合成趋势。100k `-count=3` 整轮含临时文件夹具和清理约 253 s。
- 符号链接检测在 Windows 实际以已打开句柄 `Stat()` 判定；当前 Windows 用户没有创建 symlink 的权限，因此 Windows symlink 回归测试被 Go test 明确 skip。Windows 常规文件与任务存储全套测试通过，WSL Linux `openat|O_NOFOLLOW` 行为及全套 race 测试通过。真实 Windows 重解析点创建环境仍需补验。
- 源码 revision `23dd6efcb3b464bb4e4a9eed7c5ca0a5c644d256`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.14-preview.1.zip`，10,068,020 字节，SHA256 `FCE1A7119B84B43A776B3631D7909783479F528C11BA84827CF135357AE1CB4E`；11 个 payload 哈希全匹配。
- Node 17 项、Windows 全模块 Go test/vet、三平台构建、WSL 全模块 race/vet 通过。Windows/Linux 包版本信息正确；Linux AMD64 包内启动、认证 health、匿名 401、SIGTERM 退出、ARM64 ELF 检查通过。没有真实百度凭证或云端操作。
- 验证明细：[v0.8.14-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.14-preview.1-validation.md)。真实 NAS、目标存储、真实百度及长时间运行仍待验收。

## v0.8.13-preview.1（修复队列 Load 的记录缓冲区别名）

- 修复 `Store.Load()` 在 `LoadEach` 复用缓冲区后，把多条记录保存成同一底层字节切片的问题。先添加 RED 回归：三条记录加载后，ID 1 实际读成 ID 3；逐条复制后通过。下载器启动仍使用 `LoadEachRaw` 逐条解码，不增加启动时全量 map。
- Windows AMD64 单次合成基准：10,000 条 `OpenDownloader` 约 5.60 s、310.2 B/任务；100,000 条约 122.34 s、304.1 B/任务、保留堆 29.00 MiB。100,000 条端到端基准约 291 s，包含十万个独立记录文件的夹具生成；不能外推至 Linux/NAS。下一步要把枚举、打开、解码分项计时，再确定 Windows 大队列慢的主要成本。
- 源码 revision `cc38ea46cd8b257d4d1c8ed7c9ddd35c2bc1f347`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.13-preview.1.zip`，10,067,637 字节，SHA256 `5423F0BB38DF38AA05C0CD6AA67432D332F43C81496F6C479FF12F0FB7500400`；11 个 payload 哈希全部匹配。
- Node 17 项、Windows 全模块 Go test/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；WSL 全模块 race/vet 通过。Windows race 在本机 Cygwin GCC 被 Go 拒绝；切换 UCRT GCC 后链接仍失败，因此不计为通过。Windows 二进制版本/revision、Linux AMD64 包内启动与认证 health、匿名 401、SIGTERM 正常退出、ARM64 ELF 架构检查通过。
- 验证明细：[v0.8.13-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.13-preview.1-validation.md)。未使用真实百度凭证或执行云端操作；真实 NAS、真实账号和长时间运行仍待验收。

## v0.8.12-preview.1（减少历史任务进度排序除法）

- 进度排序中无活动 Task 的历史任务直接用固定比例：已完成文件进度为 1，其余为 0；仅活动传输根据实时字节数计算比率。排序结果与原浮点比率语义一致，边界测试覆盖完成文件、失败/待处理、目录、活动传输和零长度。
- WSL 对照 v0.8.11（两组各运行 100 次）：10,000/100,000 条任务的 100 项 progress 页约 449/12,468 μs 降至 368/10,983 μs，约快 18%/12%；分配量保持约 38,296 B/op、127 allocs/op。仍需扫描任务集并维护 offset+limit 大小的有界堆，复杂度不变。
- 顺带复测 D7 空闲 claim：10,000/100,000 条全部完成历史约 60.5 ns/op，0 alloc，耗时不随历史数量增长；NAS CPU 和活跃任务唤醒延迟仍未验证。
- 源码 revision `43d0831a4386c0a5eeeb0202f2cdb0b035bc1fec`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.12-preview.1.zip`，9,850,463 字节，SHA256 `75339B767C5650A2B302902F8536D2F31BEDC68A171CC1115D2CB3ACB8737A9B`；11 个 payload 全部匹配。
- Node 17 项、Windows 全模块 Go test/vet、WSL 全模块 race/vet 和三平台构建通过。Linux AMD64 包内混合完成/失败历史恢复、进度排序结果、认证 health、匿名 401 与 SIGINT 停止通过。
- 验证明细：[v0.8.12-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.12-preview.1-validation.md)。未接入真实百度凭证或云端；目标浏览器/NAS 仍需实际验收。

## v0.8.11-preview.1（复用队列历史读取缓冲区）

- `LoadEach` / `LoadEachRaw` 在一个回调扫描周期内复用单个 `bytes.Buffer`，按最多 1 MiB + 1 字节读取以保持记录尺寸上限；RAW 回调的字节切片生命周期明确为仅本次回调。队列 JSON、校验责任和错误传播不变。
- WSL 合成基准与 v0.8.10 对照：10,000 条 `LoadEachRaw` 临时分配约从 8.02 MB 降至 2.90 MB；100,000 条 `OpenDownloader` 约从 144.93 MB 降至 93.72 MB。启动时间样本接近，不据此宣称稳定提速；该优化降低临时分配，不改变恢复后常驻任务映射。
- 源码 revision `19b16974991bed8e456f485d5cb34a2eb595c0d6`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.11-preview.1.zip`，9,850,274 字节，SHA256 `F02E5C095746E9056495324B6426F01D8A48ACAC3B1E625A539694219906918F`；11 个 payload 全部匹配。
- Node 17 项、Windows 全模块 Go 测试/vet、WSL 全模块 race/vet 与三平台构建通过。增加回调后缓冲区复用、恰好 1 MiB 接受及超过上限拒绝测试。Linux AMD64 包恢复 10,000 条合成完成任务；认证分页、匿名 401、SIGINT 停止通过。
- 验证明细：[v0.8.11-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.11-preview.1-validation.md)。无真实百度凭证或云端操作；NAS/SMB 及长时间运行尚未验证。

## v0.8.10-preview.1（Linux 队列恢复使用目录相对打开）

- Linux taskstore 恢复时打开 jobs 目录一次，以目录文件描述符读取排序后的条目，并用 `openat` + `O_NOFOLLOW` 打开每个已校验文件名；jobs 目录本身也使用 `O_NOFOLLOW|O_DIRECTORY`。非 Linux 延续既有符号链接检查和路径打开。
- WSL ext4 三次/样本对照：100,000 条 `OpenDownloader` 约 900 ms → 606 ms；10,000 条 `LoadEachRaw` 约 33.3 ms → 28.5 ms。目标 NAS、SMB 与其他文件系统尚未实测，不能把该加速比例外推。
- 源码 revision `71f9c5f116fb4114fc8f6fcb0820ae21fc57c102`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.10-preview.1.zip`，9,849,908 字节，SHA256 `3834BE2FE5E96838B1A5205756E5B566C1DE58A041CD146867C89479B91CED43`；11 个 payload 哈希通过。
- 打包门禁 Node 17 项、Windows Go 全模块 test/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；WSL 全模块 race/vet 通过。Linux AMD64 包内恢复 10,000 条合成完成任务，认证 health/name 分页、匿名 401、SIGINT 停止通过。
- 验证明细：[v0.8.10-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.10-preview.1-validation.md)。未使用真实百度凭证或云端操作；目标 NAS 实测仍待用户环境。

## v0.8.9-preview.1（压缩云端扫描资格索引）

- `scheduler.eligible` 从 `map[int64]FileInfo` 改为指针索引：同一云端身份指向任务的持久化元数据，只有活动任务或源操作结果未知且扫描身份变化时才持有独立快照；所有入队、重试、认证续期和源操作仍对 `FileInfo` 做逐字段精确比较。
- WSL 单次 `BenchmarkReconcileRetainedHeap/tasks_100000`：v0.8.7 基线 51.19 MiB（536.7 B/任务），当前 36.84 MiB（386.3 B/任务），约减少 28%。这是合成任务保留堆样本，不代表 NAS RSS；启动逐条读取和完整 job 映射仍在。
- 源码 revision `8c5c96e2b3a778d73263635edd4bfb162bb643c5`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.9-preview.1.zip`，9,848,831 字节，SHA256 `C15241B9695DC92140BEB80E7DF59A640D3F5D0AA0EEF0081F8862D7EE15DF81`；清单 11 个 payload 全部匹配。
- Node 17 项、Windows Go 全模块 test/vet、WSL 全模块 race/vet 与三平台构建通过。包内 Linux AMD64 恢复 10,000 条合成完成任务；版本/revision、认证 health/name 分页、匿名 401、SIGINT 停止通过。
- 验证明细：[v0.8.9-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.9-preview.1-validation.md)。无真实百度凭证或云端操作；NAS 和真实浏览器验收仍待进行。

## v0.8.8-preview.1（减少队列元数据重复驻留）

- 恢复和扫描时让 `ServerFilename` 在确为路径 basename 时复用 `Path` 的底层字符串；已持久化任务与新扫描元数据完全相同时，`eligible` 索引复用原任务的不可变元数据。不同名称/路径和变更元数据仍独立保留，队列 JSON v1 与源项隔离/操作前身份核对语义不变。
- WSL 单次 100,000 任务扫描后保留堆：v0.8.7 基线约 51.19 MiB（536.8 B/任务），当前约 47.40 MiB（497.0 B/任务），下降约 7.4%。单次合成进程堆样本，不代表 NAS 的绝对 RSS；分配量和扫描用时没有显示同等比例下降，启动逐文件读取以及完整任务映射仍在。
- 源码 revision `c7a07c85cd222bd5672235ea23fdda86d46262ed`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.8-preview.1.zip`，9,848,923 字节，SHA256 `65F8D3F5A390688697C2F45924C0ED498A2DBCA83EA3DED376ED98143BADF0E3`；清单 11 个 payload 全部匹配。
- 打包门禁 Node 17 项、Windows 全模块 Go 测试/vet，Windows AMD64/Linux AMD64/Linux ARM64 构建通过；WSL 全模块 race/vet 通过。包内 Linux AMD64 恢复 10,000 条合成完成任务，认证 health 和 name 分页准确、匿名 401、SIGINT 停止通过。
- 验证明细：[v0.8.8-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.8-preview.1-validation.md)。未使用真实百度凭证或对云端执行读写；NAS 文件系统、真实浏览器及长时间运行仍待用户测试。

## v0.8.7-preview.1（缓存稳定排序的任务分页）

- 名称和大小排序的任务页首次按需构建 FsID 顺序索引；后续分页按 offset/limit 读取窗口。任务集合或元数据变化时索引失效并重建。进度排序仍使用实时进度和原有扫描堆算法。
- 全局任务状态计数通过 generation 惰性缓存；持久化任务变化、非根任务新增/状态变化会使摘要失效。返回的计数 map 每次复制，调用方不能污染缓存。队列 JSON v1、API/WS 格式及排序语义不变。
- WSL 内存合成基准（单次）：10,000 条 name/size 热页约 25–34 μs，冷建约 2.65–2.87 ms；100,000 条热页约 15–23 μs，冷建约 53–55 ms；100,000 条 progress 扫描约 7.32 ms；热状态统计约 0.8–1.1 μs。基准本身与目标 NAS 有差异，不承诺跨设备比例。
- 独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.7-preview.1.zip`，9,847,067 字节，SHA256 `23E152680A5EB7D7C99E301A2544FF3F56C49D6D7442ADB7803CC0496C4B704A`；源码 revision `236bc0796e261106f3c79b642b541854ee435e24`。ZIP 12 项，独立核对清单中的 11 个 payload 哈希全部匹配。
- Node 17 项、Windows Go 全模块测试/vet、WSL 全模块 race/vet、Windows AMD64/Linux AMD64/ARM64 构建通过。Linux AMD64 包从隔离副本恢复 100,000 条合成完成记录；认证 health 计数正确，name/size 分页顺序正确，匿名请求 401，SIGINT 停止且端口释放。HTTP 样本 name 冷/热约 83/0.9 ms，size 冷建约 57 ms；包含 WSL 文件系统与 JSON 响应开销，不代表 NAS。
- 验证明细：[v0.8.7-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.7-preview.1-validation.md)。未接入真实百度凭证或云端操作；目标 NAS/真实浏览器仍待测试。

## v0.8.6-preview.1（恢复队列避免重复 JSON 语法扫描）

- `OpenDownloader` 恢复时改用有界原始记录读取，再由原有 `json.Unmarshal` 完成语法和结构校验；`LoadEach` 与 `Store.Load` 的既有校验行为不变。新增损坏 JSON 的恢复拒绝/锁释放回归测试。
- 10,000 条合成记录对照中 raw 与 validated 样本时间有明显重叠，受缓存顺序和文件系统影响；只确认每条恢复记录少一次 `json.Valid` 全文扫描，不宣称端到端启动耗时稳定下降。
- 源码 revision：`e4ef546fdd26eb9d14b62168ecff8d9a07f654e3`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.6-preview.1.zip`，9,839,549 字节，SHA256 `9433D08218FD66755A20BCFDE5384015BCA5EE5F96E3323721EA57433C7E655D`。
- Node 17 项、Windows Go 全模块 test/vet 与三平台构建通过；WSL race 全模块与 vet 通过。Linux AMD64 包副本恢复 10,000 条合成记录，认证分页 10 条、匿名 401、SIGINT 停止和端口释放通过。验证明细：[v0.8.6-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.6-preview.1-validation.md)。无真实百度凭证或云端读写。

## v0.8.5-preview.1（Linux 逐任务读取少一次路径系统调用）

- `taskstore.LoadEach` 在 Linux 上通过 `O_NOFOLLOW` 打开记录，合并每条任务文件的 `Lstat` 防符号链接检查与后续 `Open`。符号链接仍被拒绝，并新增 Linux 直达 `Store.Load` 的回归测试；Windows/其他平台保持原来的 `safePath` 检查。
- WSL Linux 单文件对照基准：旧 `Lstat + Open` 为 2.34–2.80 μs/op、449 B、9 allocs；新 `O_NOFOLLOW Open` 为 1.30–1.33 μs/op、192 B、7 allocs。10,000 条合成历史的 `OpenDownloader` 五次单迭代为 63–84 ms、约 16.5 MB、200k allocations；这是本机 WSL 文件系统样本，不等同 NAS。
- Windows 全模块 test/vet、WSL 全模块 race/vet、Node WebUI/userscript 17 项及 Linux symlink 拒绝/普通记录测试通过；发布脚本三平台构建通过。
- 源码 revision：`c9d1b168ea38a573446abfb1990c97750e816c6b`。独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.5-preview.1.zip`，9,839,210 字节，SHA256 `1C33F73E93C8B4F6872D7527AAEC7C3BBB5A74CF5EB610BB6DE371BFDA895F7D`；ZIP 12 项中 11 个 payload 与 SHA256 清单全部匹配，Linux AMD64/ARM64 架构及 AMD64 版本标识正确。
- 包内 Linux AMD64 `start.sh` 从隔离的 `/tmp` 状态目录启动，恢复 10,000 条合成任务；health=`unconfigured`，状态总数/完成数 10,000，分页 10 条，匿名 401；SIGINT 后 `Service stopped` 且端口释放。验证明细：[v0.8.5-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.5-preview.1-validation.md)。
- 没有真实百度凭证或云端写操作。实际 NAS 启动性能、逐记录 JSON 解码成本和全部历史驻内存尚未改变。

## v0.8.4-preview.1（空闲 worker 候选堆）

- `claim()` 由全量 `scheduler.jobs` 扫描改为两个内存堆：就绪任务按 `Created`/`FsID` 排序，退避任务按最早 `NextAttempt` 唤醒。只有最新云端扫描确认且可运行的任务进入索引；完成历史不创建堆项，持久化 JSON v1 不变。
- 云端 reconciliation 清空并在原有全量扫描中重建候选索引；完成、取消、手动重试、认证轮换及 intake 隔离变化更新对应索引。认证变化清空旧扫描候选；intake unknown 隔离解除后已扫描任务恢复候选，不等待定时扫描。
- 10,000/100,000 条已完成历史的 idle `claim()` Windows 基准分别约 25.1/26.1 ns/op，0 B/op、0 allocs/op；前一版单次测量约 104.7 μs/5.68 ms。不同运行/CPU 的数字不作跨机器性能承诺。
- 新增排序/退避/认证失效和 intake 隔离往返测试；Windows 全模块 `go test`、`go vet`、Node WebUI/userscript 17 项通过，WSL 全模块 race/vet 通过。Windows `-race` 由 UCRT gcc 链接失败（本机既有工具链限制），不计为通过。
- 独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.4-preview.1.zip`，9,838,624 字节，SHA256 `83C13FFF7730C66E98EFDF0D25D676EEB7527F14659C2CF9497F19222C29CADB`；11 项 payload 哈希、Linux AMD64/ARM64 ELF 架构检查及 Linux AMD64 版本/revision 核对通过。
- 包内 Windows 程序从隔离副本恢复 10,000 条合成完成任务；认证 health 显示未配置授权，状态接口显示 10,000 总数/完成、10 条分页，匿名访问 401；Ctrl+C 后服务停止且端口释放。验证明细：[v0.8.4-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.4-preview.1-validation.md)。
- 没有真实百度凭证或云端写操作。真实 NAS 空闲 CPU、活跃候选集性能和唤醒延迟仍未验收；D6 的逐记录恢复 I/O 和 NAS 大队列启动验证继续开放。

## v0.8.3-preview.1（空闲 worker 事件驱动）

- 源码 revision：`9a4de892749e390f6a84840f6b719d850bbfcf06`。基线测得：在 10,000/100,000 条全部完成的合成历史上，单次 idle `claim()` 扫描约 0.105/5.68 ms，且旧 worker 用 100ms ticker 固定重扫。现改为状态/队列信号唤醒所有配置 worker；无可运行项时等待事件或最近一个可运行 retry 的 `NextAttempt` 定时器，不再周期轮询。
- `claim` 仍对每次状态变化做 O(n) 一次扫描，未添加内存索引；持久化状态、最早创建任务优先、认证/扫描资格及重试预算语义不变。
- RED/GREEN 覆盖配置 worker 扇出、最早 retry 截止时间认领、既有 1 秒退避、手动重试和最大并发。Windows Go 全模块测试/vet、WSL race/vet、Node 17 前端 17 项通过；三平台构建通过。
- 独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.3-preview.1.zip`，9,826,145 字节，SHA256 `EFB04D10A805C3781A732427C97A5CF4122AD8CA22D807E00F92EED3C3AB8CC7`；ZIP 解压后 11 项 payload 哈希全通过，Linux AMD64/ARM64 ELF 架构和 AMD64 版本标识通过。
- 包内 Windows 程序从隔离目录恢复 10,000 条合成完成任务；认证状态返回等待授权，认证分页为 10,000 总数/10,000 完成/10 项当前页，匿名状态接口 401；Ctrl+C 后服务停止。详细证据：[v0.8.3-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.3-preview.1-validation.md)。真实 NAS 空闲 CPU/唤醒延迟、百度账号和云端下载链路未测。

## v0.8.2-preview.1（按需生成非活动任务视图）

- 启动恢复仍将每个 job 保留在 `scheduler.jobs`，但不再额外创建每个任务的 `Task` 对象；`Downloader.tasks` 只保留正在下载时无法从 job 恢复的实时进度对象。任务统计读取 job 状态，页面和 API 在读取时生成视图。
- 分页排序现在用轻量候选项访问 job/活动任务字段，仅为返回页生成任务副本；10,000 条历史取 100 条基准约 38,248 B、108 次分配，与原分页基准的 39,152 B、108 次分配同一量级。
- 10,000 条启动基准的本次 Windows 单次运行约 4.97 秒、22.51 MB 分配、150,435 次分配；前一基线单次约 6.33 秒、26.65 MB、160,545 次分配。WSL 单次约 105 ms、19.61 MB，前一基线约 221 ms、23.33 MB。运行时间受文件系统缓存/环境波动影响，不据此承诺提速；分配量下降与“启动不重建历史 Task 副本”一致。
- RED/GREEN 覆盖重启后任务总数、状态、进度、时间、尝试次数、旧全量列表、分页排序和零历史 Task 常驻；Windows 全模块测试/vet、WSL 全模块 race/vet、Node WebUI/userscript 17 项通过。
- 独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.2-preview.1.zip`，9,824,042 字节，SHA256 `1bdd4ed0bf527410ac255fbb5c41f99ef00debfa7b046cd5cce66e5bd62019e5`；三平台构建及 11 项 payload 哈希通过。
- 包内 Windows 程序从隔离目录恢复 10,000 条合成完成任务；认证 health/page 正确返回版本、全局总数/完成数 10,000 和 10 项当前页，匿名接口 401；Ctrl+C 后服务停止、端口释放。验证明细：[v0.8.2-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.2-preview.1-validation.md)。真实 NAS 文件系统、峰值 RSS 和百度链路仍未验收。
- 源码 revision：`168222a5e1d591b9a33627215f9ca709de82ab49`。验证包没有真实百度凭证，未执行百度 API 或云端写操作。

## v0.8.1-preview.1（启动时逐条恢复任务）

- 队列恢复改用 `taskstore.LoadEach`：逐条按文件名读取/校验、解码并反射为任务，避免启动期间同时保留整份原始 JSON map 和运行期任务映射；公开的 `Store.Load()` 兼容接口仍保留。
- 新增 `LoadEach` 顺序、回调失败停止测试，以及无效记录拒绝启动并释放 writer lock 的下载器回归；新增 100/1,000/10,000 合成记录启动基准。多次单迭代耗时波动明显，不据此宣称启动加速；更新目标为降低临时原始记录映射的同时驻留。
- 包内 Windows 实际恢复隔离的 10,000 条合成完成任务，约 7 秒启动并开始监听；认证状态接口正确返回 10,000 总数/完成数和 10 条当前页，匿名接口 401。此为当前 Windows 本地文件系统合成验收，不代表目标 NAS 性能。
- Windows Go 1.27.1 全模块测试/vet、WSL Go 1.22.2 全模块 race/vet、Node WebUI/userscript 17 项、三平台交叉构建通过；ZIP 11 项 payload 哈希通过。
- 源码 revision：`c217a9bf31c1d2518655a232443cf1176deb8c4d`；独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.1-preview.1.zip`，SHA256 `8e051715edbbca0edfbc06d9cef3a553cf81db9ecbc85ea235835bbd3cbba858`。验证明细：[v0.8.1-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.1-preview.1-validation.md)。

### D6 后续：有界并行读取对照实验（未改变生产代码）

- 在相同 10,000 条小型 JSON 夹具上比较当前串行读取与每批 2/4/8/16 个并行读取；Windows 串行两次约 5.60/7.09 秒，4 worker 约 3.52/3.67 秒、8 worker 约 3.29/3.56 秒，单次额外分配约 1.5–1.8 MB。单次基准波动较大，不能当作目标部署的固定加速比例。
- WSL 访问同一工作树时串行约 54.5 ms，4 worker 约 83.3 ms，16 worker 约 55.7 ms；没有显示 Linux 本地文件系统的稳定收益。并行试验代码已移除，没有接入产品实现或打包。
- 结论：不将该并行方案默认用于 NAS/Linux 启动恢复。下一步需优先基于真实 NAS/目标文件系统测量；如果无法提供该环境，先评估持久化摘要/索引方案的迁移与崩溃一致性成本，再决定是否改变全量历史恢复结构。

## v0.8.0-preview.1（任务历史分页）

- 已完成任务列表服务端分页：两种管理页与 WebSocket 默认每页 100 项；文件名、进度、大小排序覆盖全队列，页面任务总数/状态数仍为全局统计。分页状态接口不再复制和序列化完整任务快照；健康检查也改为只统计任务状态。`/api/status` 无分页参数时保留原有完整列表响应。
- 任务历史仍全部保留；本轮未改变队列去重与云端操作语义。启动仍读取所有持久化记录，分页请求仍线性扫描任务以计算统计和选择排序窗口。真实目标浏览器长队列与 NAS 性能仍待验收。
- 本机 10,000 条合成任务单次基准：全量快照约 2.21 ms、3.64 MB、19,902 allocations；100 项分页约 0.69 ms、37 KB、108 allocations。此数值仅是当前 Windows 开发机的一次合成基准。
- Windows Go 1.27.1 全模块测试/vet、WSL Go 1.22.2 全模块 race/vet、Node WebUI/userscript 17 项、三平台交叉构建通过。Windows race 因本机 GCC/MinGW 链接器失败，未计为通过。
- 源码 revision：`293cdf0f9e7dd56db374fbfc7abd817fc663063a`；独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.0-preview.1.zip`，SHA256 `6ad2108907d2c1ab1930505b665a0d92388b54b7f0bad75866a6fd652c8c7dc3`。11 个 ZIP payload 哈希通过；包内 Windows 启动后验证匿名 401、认证 health/page 200、旧状态接口兼容、非法页大小 400、页面资源及 Ctrl+C 停止/端口释放。明细：[v0.8.0-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.0-preview.1-validation.md)。

## v0.7.9-preview.1（WebSocket 依赖维护与恢复回归）

- 将 `github.com/gorilla/websocket` 从 v1.5.1 更新到 v1.5.3；此版本修复子协议选择和随机值生成等问题。升级后的库不再声明 `golang.org/x/net` 依赖，`go mod tidy` 删除了原 v0.17.0 间接依赖，项目仍保持 Go 1.22 兼容目标。
- 同包纳入队列重启后复用已发布文件的回归，以及下载响应头停滞时从请求开始计时的超时回归。Windows 当前 Go 工具链、WSL Go 1.22.2 全模块 race/vet、Node 15 项和差异检查均通过。
- 源码 revision：`febb2d9086ed8429e85d8a0a0574fae91b28ee53`；独立 ZIP `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.9-preview.1.zip`，SHA256 `e686f85b612f37118ef281d03cdc5a1f8b5d0754d812d00d3a79550fce4e0782`。三平台构建、11 项 payload 哈希、Windows/Linux AMD64 版本/revision、ARM64 ELF 架构通过。包内 Windows 启动的匿名 401、认证 health/page 200、Ctrl+C 停止与端口释放通过。验证明细：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.9-preview.1-validation.md`。

## v0.7.8 后续：已完成文件凭据身份漂移回归（仅测试）

- 新增完成文件身份变化测试：旧版本已有 SHA256 完成凭据、本地目标内容有效时，云端 MD5 变化必须保留原文件，不能下载新内容，也不能执行源操作。
- 当前 transfer 实现符合该约束；Go 全模块测试、`go vet`、WSL 全模块 race 和 diff 检查通过。此项只增加回归覆盖，不改变运行时二进制，因此 v0.7.8-preview.1 仍为当前独立测试包，不重打相同行为包。

## v0.7.8 后续：发布文件后的队列崩溃恢复回归（仅测试）

- 新增持久队列恢复用例，模拟根任务已持久化为 `downloading`，本地最终文件和匹配的 SHA256 完成凭据已发布，但进程在开始网盘后处理前中断。重启并成功扫描后，零次重新请求文件流、原文件内容不变、删除操作恰好一次且任务恢复为 done。
- 定向用例、Go 全模块非缓存测试、`go vet ./...`、WSL `go test -race -count=1 ./...`、Node 15 项和 `git diff --check` 通过。该用例验证既有实现，不改变运行时二进制；本次不生成内容相同的新包，v0.7.8-preview.1 仍为最新独立测试包。

## v0.7.8 后续：下载响应头停滞超时回归（仅测试）

- 新增本地 HTTP 服务器不返回响应头的挂起场景，验证下载空闲计时从请求发起时开始，返回可重试的 idle timeout；已有响应体停滞和持续传输超过 idle 周期的测试继续覆盖另外两种流状态。
- 定向测试、Go 全模块非缓存测试、`go vet ./...`、WSL 全模块 race、Node 15 项和 `git diff --check` 通过。运行时逻辑原已实现，属于失败路径测试补强，不改变二进制且不重打相同行为包。

## v0.7.8-preview.1（身份变化任务恢复已交付）

- 源码 revision：`ead3278c9673c3abb4e758d347f34dd5aa380f34`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.8-preview.1.zip`；SHA256：`1b366832816eb3917ec014ecc5da3046bf450827534730d33d317b1eede60e3a`。
- 源项身份复核失败曾按普通 failed 保存；若下一次列表仍返回旧元数据，用户仍能重试旧任务，造成重复下载且必然再次拒绝源操作。
- 现在持久化 `source_state=changed`：阻止旧身份的手动重试，不隔离源路径；下次扫描确认同一 fs_id 的新元数据后，才替换为新任务。重启后仍保留不可重试状态。结果未知的 `source_unknown` 策略未变化。
- 新 RED/GREEN 覆盖旧列表状态下 Retry 返回冲突、进程重开后仍冲突，以及新目录元数据出现后自动重新入队并完成；Go 全模块测试/vet、WSL race 全模块、Node 15 项通过。发布脚本三平台构建与哈希检查通过；Windows/WSL Linux AMD64 版本/revision 和 ARM64 ELF 架构匹配。正式包 Windows 启动验证认证 health、匿名 401、页面和资源、无百度授权时云目录 409，以及 Ctrl+C 停止和端口释放。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.8-preview.1-validation.md`。

## v0.7.7-preview.1（源项身份复核已交付）

- 源码 revision：`f7b220b333ae98c6cf98140cf1b6b6aec0760a6d`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.7-preview.1.zip`；SHA256：`b918662e5cd783b0f90568af8ab936efe42b76d9112b1a263ca53ca4b4d98522`。
- 用户确认路线 1：保留自动源改名/删除，但操作前重新核对身份。持久化任务现在通过 `fs_id` 读取最新文件元数据；身份字段变化或源项已不存在时不发送改名/删除，任务失败并保留本地下载结果。
- 可比身份字段：`fs_id`、完整路径、名称、大小、`server_mtime`、目录标记和 MD5。API 错误仍走现有重试/认证分类；请求已发送后的未知结果仍持久化隔离，无人工放行入口。
- 尚存边界：身份读取与后续改名/删除不是原子操作，读取完成后云端仍可能立即变化；测试与本地 API 夹具不能证明百度服务端真实并发行为。
- RED/GREEN 覆盖元数据查询参数/解码及源路径变化时不执行删除；Go 全模块测试/vet、WSL race 全模块、Node WebUI/userscript 15 项通过。三平台构建、ZIP payload 哈希/目录一致性、Windows 与 WSL Linux AMD64 版本/revision、ARM64 ELF 架构通过。后续补充了 Windows 正式包实际启动、认证/匿名 HTTP、管理页/脚本资源、未授权云目录拒绝和 Ctrl+C 退出检查。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.7-preview.1-validation.md`。

## 总目标

按已给出的 A/B/C/D 优化路线持续推进，阶段性交付独立可测试版本。首版不是整个目标完成。设计见 [reliability-design.md](reliability-design.md)，当前计划见 [reliability-plan.md](reliability-plan.md)。

## v0.7.6-preview.1（管理页大目录分页优化）

- 源码 revision：`9e018c2a4854c1f6ca1b6f07a5813ef5b32837ee`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.6-preview.1.zip`；SHA256：`cfa810c5b3d0d6f04a5398d1e420e26e1d1a9c67f5844c65501ba299bb79ca9b`。
- `/api/cloud` 过去先完整遍历网盘目录，再裁剪当前 100 项页面；目录越大，每次翻页都重复读取全部目录。现在直接按 Baidu `start/limit` 读取请求窗口，并额外读取 1 项判断是否有下一页，成本与当前页接近。
- 上游列表没有可依赖的准确总数，因此 `total` 明确返回 `null`，管理页显示“总数未知”和是否仍有下一页，不伪造总数。页内路径校验、重复 FSID 防护和既有隔离行为保留。
- 集成回归用 250 项模拟目录请求第 2 页，确认仅向上游发一次 `start=100&limit=101` 请求；Node UI 回归检查未知总数提示和翻页 offset。
- 真实百度目录分页/并发增删一致性尚未验收；变化中的目录采用 offset 分页，翻页期间新增/删除可能改变后续页内容。
- 发布脚本 `go test ./...`、`go vet ./...`、Windows AMD64/Linux AMD64/ARM64 构建通过；WSL `go test -race -count=1 ./...`、Node WebUI/userscript 15 项、payload 哈希、ZIP 内容一致和 Windows 版本命令通过。Windows race 运行受本机 GCC/MinGW 链接器失败影响，未作为通过证据。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.6-preview.1-validation.md`。
- 接续已确认的优化：网盘源项在改名/删除前重新核对身份；保持未知结果隔离且无人工放行入口。

## v0.7.5-preview.1（配置与 API 响应校验测试包已交付）

- 源码 revision：`9061e8aa2badb83bfaaf5e00de4812927c6042c4`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.5-preview.1.zip`；SHA256：`8e6d6c80310fa681a828ef230a4729121afc5af5f721b7d35652256dcae6d57c`。
- 数值环境变量显式设置非数字时启动失败；百度目录列表和下载链接响应超上限时拒绝，避免截断后的合法 JSON 被误认成完整响应。超限 503 仍保留原可重试分类。
- Node WebUI/userscript 14 项、发布脚本中的 Go 全模块 test/vet、Windows AMD64/Linux AMD64/ARM64 构建通过；WSL Go 1.22.2 全模块 race 通过。11 项 payload 哈希、ZIP 与发布目录一致，AMD64 版本/revision 精确匹配，ARM64 ELF 为 AArch64。
- Windows 与 WSL Linux AMD64 正式包副本均实际启动：认证 health=200、匿名=401、版本/revision 匹配；Ctrl+C 后服务停止且端口释放。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.5-preview.1-validation.md`。
- 无真实百度凭证或云端操作；厂商 NAS、ARM64 实机、多日与断电仍未验收。未知结果目录继续隔离，无人工放行。

## v0.7.4-preview.1（Compose 自定义宿主端口/扫描间隔测试包已交付）

- 源码 revision：`5cc32059ad3f7042353b232d9ac6dc40fa5e4ca0`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.4-preview.1.zip`；SHA256：`9766ac64e5598b7da67cb3da2ce02d8cc0342d1466ab6790dab5710f6c977f7b`。
- 发布 Compose 允许在 `.env` 设置 `HOST_PORT` 和 `SCAN_INTERVAL`；默认回环端口仍为 18089，扫描间隔 30 秒。配置展开测试和正式包副本内实际 Compose 容器运行通过，认证 health 200、匿名 401，运行日志确认自定义扫描间隔，版本与 revision 匹配。
- Node WebUI/userscript 14 项、Go 全模块 test/vet、Windows AMD64/Linux AMD64/ARM64 构建通过；发布包 11 个文件哈希匹配，ARM64 ELF 为 AArch64。验收记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.4-preview.1-validation.md`。
- 无真实百度凭证或云端操作。厂商 NAS、真实百度、ARM64 实机、多日和断电仍未验收；未知结果继续隔离，无人工放行。未 push/deploy，主工作区干净。

## 数值环境变量格式错误检查（已纳入 v0.7.5-preview.1）

- `SCAN_INTERVAL`、`MAX_PARALLEL`、`MAX_RETRIES`、`RETRY_BACKOFF_SECONDS` 显式设为非数字时，原先静默采用默认值，导致启动范围校验无法发现配置错误。现在仅在变量未设置/为空时采用默认值；显式无效值会进入校验失败路径并阻止服务启动。
- 新增配置测试，覆盖四个变量的无效格式与未设置默认值；先确认新用例在旧行为下失败，再修改解析逻辑。`go test ./internal/config`、`go test ./...`、`go vet ./...` 通过。
- 实现与独立包验证见上方 v0.7.5-preview.1；未连接百度账号或执行云端操作。

## 百度 API 响应大小上限校验（已纳入 v0.7.5-preview.1）

- 目录列表（16 MiB）与下载链接（1 MiB）原先只读取上限内字节，却未识别响应实际超限；若有效 JSON 后跟随足够多的空白，截断内容仍可解析并被当作完整成功结果。现最多读取上限加 1 字节，超过即失败，避免把超限响应当作完整列表/链接。
- 两项本机 HTTP 回归先在旧行为下失败，修复后通过；另验证超限 503 仍保留可重试分类。`go test ./...`、`go vet ./...`、`git diff --check` 通过。超限目录结果失败时，不会被当作空/完整目录交给后续处理。
- 实现与独立包验证见上方 v0.7.5-preview.1；未连接百度账号或执行云端操作。

## v0.7.3-preview.1（Linux 目录项持久性测试包已交付）

- 源码 revision：`3426b016baf5acf4bc89a2d27713b87707984a6a`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.3-preview.1.zip`；SHA256：`378105077648a0be42c5523ec9290305fb7d79596d55dcca2461390649bc9a04`。
- Node WebUI/userscript 14 项、Go 全模块测试/vet、Windows UCRT 与 WSL 全模块 race/vet 通过；Windows AMD64/Linux AMD64/ARM64 构建通过，包内 11 个 payload 哈希与清单一致，ARM64 ELF 为 AArch64。
- Linux 对状态 rename、关键目录创建和最终文件硬链接同步父目录；失败会阻断后续源操作。回归覆盖首次发布与重试时目录同步失败，确认不发送网盘源请求。Windows 不宣称目录项掉电持久性。
- Windows Start.ps1 与 WSL Linux 原生 start.sh 均用正式包副本实际启动；认证健康/页面 API、匿名 401、版本与 revision 验证通过，服务停止后端口释放。全程无真实百度凭证或云端写操作。
- 真实 NAS/设备缓存、物理掉电、ARM64 实机、多日与真实百度验签仍未验收；未知结果目录继续隔离且无人工放行。验证详见发布目录 `v0.7.3-preview.1-validation.md`；未 push/deploy，主工作区保持干净。

## 源码 Docker 镜像版本元数据（源码检查点，待后续独立包）

- 源码 revision：`7357d7de61641136d79d27048ae382323be12476`。源码 Dockerfile 支持 `BUILD_VERSION`/`BUILD_REVISION` linker 参数，Compose 与 `.env.example` 接线；README 给出从 Git 取得并传入版本及 revision 的构建方式，开发构建默认仍为 `dev/unknown`。
- `docker compose config` 验证显式参数可传给 build args；Go 本机 linker 输出验证通过。使用现有 WSL Docker 真实构建源码镜像，并在专用容器验证 `/api/health` 精确返回版本与完整 revision、匿名返回 401；验后移除本轮专用容器与镜像。
- 独立发布包使用 `release/Dockerfile` 启动已注入版本的预构建二进制，此变更针对源码仓库多阶段 Dockerfile，未改变当前 v0.7.3-preview.1 包或其文件；后续预览包与其他产品改进合并交付。真实 NAS 不在本地 Docker 验证范围内。

## 源码 Docker 直接端口映射修复（源码检查点）

- 源码 Dockerfile 原来没有覆盖应用默认 `SERVER_HOST=127.0.0.1`；直接 `docker run -p` 虽然映射端口，容器服务却只监听容器 loopback。先用原镜像实际复现：日志为 `127.0.0.1:8080`，宿主映射的健康请求失败。
- 容器内默认改为监听 `0.0.0.0:8080`；README 的直接运行示例只发布到 `127.0.0.1` 并设置管理员密码，移除没有被应用当作本地队列读取的 `/NAS_Download_Queue` 挂载。
- 修复后实际构建/运行源码镜像：认证 health=200、匿名 health=401，版本与完整 revision 匹配；日志确认容器监听 `0.0.0.0:8080`。Compose 配置解析也断言容器监听与宿主回环发布地址；测试容器/镜像已清理。没有配置百度凭证或访问真实账号。
- revision：`93b5f4805b1c0870250c514f0e7dce26eb084b03`。该源码 Dockerfile 与 README 修复尚未进入独立预览包；当前 v0.7.3-preview.1 不变，后续合并应用功能后再打新包。厂商 NAS 网络/防火墙验收仍未进行。

## 源码 Compose 与部署说明对齐

- revision：`2ae69e40cb0d96b8e3321571afd752577d743a7f`。源码 Compose 现读取 `HOST_PORT`、`QUEUE_DIR`、`SCAN_INTERVAL`，`.env.example` 不再列看似可配置但实际被 Compose 忽略的本地目录/服务端口/监听地址；README 更新网页设备授权、Compose 构建和 keep/delete 说明。
- Compose 使用 `.env.example` 展开验证默认远端队列、30 秒扫描、宿主 `127.0.0.1:8080` 通过；注入自定义队列、77 秒间隔、19190 端口及 build metadata 的展开断言也通过。`git diff --check` 通过。
- 移除了旧 Kubernetes `LoadBalancer` 样板：该示例会公开管理服务，并把远端队列路径误作为容器本地卷。README 现明确 Kubernetes manifest、卷权限和集群网络未经维护/验证。未改变当前 v0.7.3-preview.1 包。

## v0.7.2-preview.1（下载 Token 查询参数修复测试包已交付）

- 源码 revision：`855ab0a8b4cf427e4c417f314db69e5f5b3de59a`；包 `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.2-preview.1.zip`，10,000,484 字节；SHA256 `e3dd0e74c6e8af62d0f9e03ae2a20c93eb69cff8dbf30d9a4762234a3a19e9a3`。
- 干净提交执行打包门禁：Node WebUI/userscript 14 项、Go 全模块测试/vet、Windows AMD64/Linux AMD64/ARM64 构建通过；包内 11 项哈希再次核对一致。Windows 与 Linux AMD64 --version/revision 匹配；ARM64 ELF machine=AArch64。
- 包内 Windows Start.ps1 实际启动：认证 health、首页、intake、status=200，匿名 health=401，源码版本/revision 精确匹配；固定假密码、无百度凭证、百度 HTTPS 指向拒绝连接代理。Ctrl+C 后 Service stopped，端口确认释放。
- 从包副本运行 Linux AMD64 原生 start.sh：认证 health=200 且版本/revision 匹配；匿名 health=401；SIGTERM 后 Service stopped、进程退出 0。测试仅在隔离 WSL home 目录进行，状态 owner/mode 不覆盖发布目录。
- 新 Token 链接查询参数的七项实际 HTTP 回归及 Windows UCRT/WSL Go1.22.2 全模块 race/vet 在打包前通过；进程强制终止后未知操作保持隔离的回归也随源码包含。
- 包内代码尚未对真实百度签名链接验签；无百度真实下载、厂商 NAS/ACL、ARM64 实机、多日或断电验收。未知目录继续隔离，没提供人工放行。验证明细在发布根目录 `v0.7.2-preview.1-validation.md`；未推送或部署。

## 下载链接查询参数修复（源码检查点，待后续包）

- 本机真实 HTTP 回归先 RED 复现：裸拼接 Token 遇到查询分隔符/片段会改变或丢失凭据；strings.Contains 把相似参数/路径误判为已有 Token，编码参数名则被重复追加。不能据此声称当前百度链接实际使用了这些形式。
- 改为解析 URL 的真实 access_token 键，仅编码新追加值；保留链接中已有 Token（含显式空值）与已有 RawQuery 原文，不重新编码签名参数。解析错误沿用脱敏与 timer/context 清理；Range、认证拒绝与重试预算不变。
- 七项实际 HTTP 用例 GREEN：特殊字符、fragment、相似参数、路径同名文本、已有 Token、编码参数名、显式空值，同时检查既有 %2f/%20 签名原文保留。Windows UCRT、WSL Go1.22.2 全模块非缓存 race/vet 均通过，diff 检查通过。
- 独立只读审查无确定 important/critical，复跑新用例及既有 URL 脱敏、取消、Range 和 idle timeout 通过。本轮未访问真实账号，尚未验证百度服务端签名验签；后续独立包纳入。当前 v0.7.1-preview.1 不含此修复，旧包不改动，未知目录仍隔离，无人工放行入口。

## 源操作请求中进程强制终止验证（测试检查点）

- 新增 crash_source_test.go，补足此前正常 Shutdown/合成记录重开之外的真实 Process.Kill 窗口。keep/delete 分别启动子进程，真实本机 HTTP 服务收到 rename/delete 请求后保持响应未完成；父进程核对 sending/source_unknown 已落盘且最终文件内容完整，再强制结束子进程，不执行优雅退出。
- 重开真实 Downloader 成功取得写锁；fresh scan 后仍 source_unknown，保留原模式，CanCancel/CanRetry=false，Cancel/Retry 返回冲突；直到重新打开的实例关闭，源操作请求计数始终为 1。父子进程所有 provider 请求均改写到本机夹具，仅使用假凭证。
- Windows 定向 race 及连续 5 次 race、WSL Go1.22.2 定向 race、Windows 全模块非缓存 test/vet、diff 检查通过；独立只读审查无确定 important/critical，并独立复跑定向测试通过。
- 本轮仅新增测试，没有发现需修改产品代码的缺陷，不重打相同行为包。当前可测版本仍为 v0.7.1-preview.1，ZIP SHA256 未变。证据只证明进程被强制终止后的恢复，不证明断电、文件系统丢写或真实百度服务端操作结果；不增加人工放行入口。原 main 保持干净，未推送/部署。

## v0.7.1-preview.1（普通用户容器运行测试包已交付）

将可选 RUN_AS、普通用户目录准备/迁移说明纳入独立包。程序行为沿用已验证的 v0.7.0，保留 HTTPS 来源、认证与未知隔离规则；旧包不改动。

- 源码 revision：`b5ed21a7050896e57c5647c5339fbfc2f7cbb48b`；发布根目录 `v0.7.1-preview.1.zip`，10,000,002 字节；SHA256 `8ceb6f5047fa597e5a13fdd6c6bc5556e637fcfea7d038840b50e2aae483c798`。
- 干净提交打包，Node 14 项、Go 测试/vet、三平台构建通过；最终 ZIP 解压 11 项哈希、ARM64 ELF machine=183、Windows --version/revision 核对通过。本轮未修改 Go 程序，未重复全部 race 测试。
- 实际包 Dockerfile 构建 AMD64 镜像，并用包内 Compose 加专用镜像/network none 测试 overlay 运行，未通过命令行覆盖 --user。RUN_AS=1000:1000、exec id、inspect、认证 health 证明身份/版本正确；无宿主端口映射，临时 ext4 状态目录 0700、manifest/锁 0600，owner=1000:1000。
- 同一目录第二容器退出 1 且报告 already in use；正常 stop/start 后 health 恢复，两次 stop 均 exit 0。两个专用容器和专用镜像标签已精确清理，保留日志；没有 prune 或操作其他容器。
- Windows verification 副本实际 Start.ps1 启动，health、首页、intake/status HTTP 200，匿名 401；无百度凭证，HTTPS 指向拒绝连接代理。Ctrl+C 后 Service stopped（外层 PTY exit 1），测试服务已停止。初次复制活动 verification 目录遇到 writer.lock 锁定，改用未运行的正式包副本构建，未带入活动状态。
- 发布包保持不可变，验证明细在发布根目录 v0.7.1-preview.1-validation.md。真实百度/NAS ACL、ARM64 实机、多日与断电尚未验收；未知目录继续隔离，不提供人工放行。持续优化目标仍进行中，未推送或部署。

## 源码镜像实际构建与部署配置修复（源码检查点）

- 复现源码 Compose 漏掉 BAIDU_REFRESH_TOKEN/ADMIN_PASSWORD：配置中设置假值却未转发。现补为可选变量，实际 Compose config 精确值通过；空值仍保留原行为。
- 新增根 .dockerignore，排除已知本地 .env/Git/下载/私有状态和输出。独立 git archive 加假标记，实际 scratch COPY/export 先证明标记可见；补规则后 .env/.git/downloads 被排除，根 .env.example、go.mod 和嵌入 UI 保留。没有发送工作区真实配置或数据。
- 使用原源码 Dockerfile 和原基础镜像地址实际构建成功，未拿替代镜像冒充。源码 Compose 以普通 UID 1000:1000、network none、临时 ext4 挂载运行，指定管理员密码有效，health has_refresh_token=true（仅假 Token），/app 与程序 0755、manifest owner/mode 正确；停止 exit 0。
- 源码镜像按原构建方式仍显示 dev/unknown；本轮没有改变 Go 程序、版本注入或基础镜像，也未打新包。证据在发布目录 source-build-20261023，厂商 NAS、真实百度、ARM64、多日和断电仍待验收。
- 独立只读审查未发现确定 important/critical，前后导出/配置/容器元数据独立核对通过，diff 检查通过。专用容器确认 exited 后精确清理，专用镜像标签已删除，保留测试日志与假标记副本；未执行 prune 或操作其他容器。

## NAS 普通用户部署与实际容器验证（源码检查点，待后续包）

- 源码镜像 runtime WORKDIR 从 /root 移至 /app，CMD 使用绝对路径；两份 Compose 增加可选 RUN_AS=UID:GID，默认 0:0 保留现有行为，env 示例与迁移说明已补充。不自动修改已有下载目录 owner/ACL。
- 本轮现场发现 WSL 内 Docker client/server 29.7.2、Compose 5.5.0 可用（此前 Windows CLI 不可用的记录不代表 WSL）。从 v0.7.0 正式包副本使用原发布 Dockerfile 实际构建 AMD64 镜像，本地已有 Alpine 3.22；未安装宿主 Docker，apk 证书包仅在专用镜像构建中安装。
- 专用测试容器 network none、无百度凭证，以 1000:1000 在临时 ext4 bind mount 启动，管理 health 正确；状态目录 0700、manifest/锁 0600，owner=1000:1000；第二实例 exit 1，停止/重启后健康恢复、正常停止 exit 0。
- 只读挂载及普通 0500 目录分别报 read-only/permission denied、exit 1，目录未生成状态。实际 Compose run 未传 --user，inspect/exec id/health 证明 RUN_AS 被应用，使用专用测试 overlay 固定镜像与 network none，无发布端口；默认及非默认身份的两份 Compose config 均解析通过。
- 证据位于发布目录 nas-permission-20261023；源码多阶段镜像未构建，不能扩大发布镜像证据。厂商 NAS ACL、SMB/FUSE、ARM64、真实百度及多日仍未验收。Go 程序与 v0.7.0 ZIP 未变，新的 RUN_AS 行将进入后续包。
- 独立只读审查未发现确定重要问题，两份 Compose 空/指定身份均独立解析通过，差异检查通过。五个专用测试容器确认 exited 后精确清理，专用镜像标签已删除；未执行 prune 或操作其他容器，验证输出保留。

## v0.7.0-preview.1（HTTPS 终止代理支持测试包已交付）

- 源码 revision：`eaf332c9cfe2ed77f79da6d3b02bbed499a72b81`；ZIP：发布根目录 v0.7.0-preview.1.zip，9,999,005 字节；SHA256 `19173bbc81dcfffd25d5e5950ee41bc05cca9d33c7c959969e3a2f78af3226c4`。
- 干净提交 Node 14 项、Go 测试/vet、三平台构建通过；最终 ZIP 解压 11 项哈希、ARM64 ELF machine=183 和 Windows --version/revision 核对通过。
- 仅在 verification 副本运行 Windows Start.ps1 -PublicOrigin 与 WSL Linux AMD64 start.sh+PUBLIC_ORIGIN。临时本机 TLS 代理逐一转发到实际包内进程，刻意改写后端 Host/伪造 Forwarded 头；认证 health/POST 200、WSS 状态帧、恶意 Origin HTTP/WSS 403、匿名 401 均通过。证书只在测试客户端信任，未安装到系统。
- 两平台预置合成 sending/source_unknown 记录：隔离状态与精确大整数 ID 保留，can_cancel/can_retry=false，TLS 入口操作 409。配置固定 HTTPS 后，直接 HTTP 浏览器 Origin 403；无 Origin 的认证 CLI 仍可读状态。没有百度凭证，HTTPS 指向拒绝连接代理。
- TLS 探针已退出；Windows Ctrl+C 后 Service stopped（外层 PTY exit 1），Linux 精确核对进程后 SIGTERM、Service stopped、exit 0，测试服务均已停止。正式包无验证状态，旧包不变；详情见发布目录 v0.7.0-preview.1-validation.md。

- 新增可选 PUBLIC_ORIGIN 固定外部 HTTPS 来源。原行为保持为按实际 TLS/Host 校验；显式配置由 Gate 验证后放入私有 request context，外层 API 与 WebSocket 共享 CheckOrigin。拒绝 cross-site，认证仍必须，不读取 Forwarded/X-Forwarded-* 作为授权依据。
- Config/main、原生 PS/sh、根/发布 Compose 和 env 示例接线。配置拒绝 HTTP、用户密码、路径前缀、query/fragment、非法/空端口；规范化默认 443、前导零端口，支持标准 IPv6 方括号。
- 先 RED：HTTPS Origin 经 HTTP 后端被 403 拒绝。修复后真实本机 TLS 代理 → HTTP applicationHandler 的管理 POST 与 WSS 状态帧通过，代理刻意重写后端 Host；匿名 401、恶意 Origin 的 POST/WSS 403。TLS 客户端仅信任夹具证书，没有跳过校验。
- 独立审查发现 :443 与浏览器 Origin 省略端口不一致，先 RED 后修，四项端口/IPv6 回归通过；复核无新确定重要问题。Windows UCRT/WSL 全模块 race/vet、Node 14 项、PS/sh 语法与 diff 检查通过；端口补项后两平台 main/management/config race 与全模块 vet 再通过。
- 设计与步骤见 [reverse-proxy-plan.md](reverse-proxy-plan.md)，使用见 release/TESTING.md；新配置已纳入 v0.7.0-preview.1。未安装代理软件，未操作真实 NAS 或账号；本机协议验证不替代浏览器原生 Basic 框、厂商代理/证书部署验收。

## v0.6.3-preview.1（自动扫描与迟到认证拒绝恢复测试包已交付）

- 源码 revision：`5f0da58ba09c940e9933caf3644d6c031f1bc61c`；ZIP：发布根目录 v0.6.3-preview.1.zip，9,992,945 字节；SHA256 `b68fb9c02f7a00331892bae3c379209fa73039af831a9db0221db69ad64dd96b`。
- 干净提交 Node 14 项、Go 测试/vet 和三平台构建通过；最终 ZIP 解压 11 项哈希及 ARM64 ELF machine=183 检查通过。Windows/Linux AMD64 包内脚本运行，版本/revision/等待授权正确，认证页面与状态可读，匿名 401。
- 两平台验证副本预置合成 sending/source_unknown、大整数 ID 记录；恢复后仍显示 source_unknown=1，任务 ID 精确，can_retry/can_cancel=false，操作均 409。无真实百度凭证，HTTPS 指向拒绝连接代理。正式包与旧版本不含本轮验证状态。
- Windows Ctrl+C 后 Service stopped（外层 PTY exit 1）；Linux 精确核对进程后 SIGTERM、Service stopped、exit 0，服务均已停止。明细见发布目录 v0.6.3-preview.1-validation.md；原 main 工作区干净，无推送或部署。

- 主程序实际 applyCredentialUpdate 回调、runScanner、后台 Manager.Run 与下载器集成发现时序问题：轮换成功并完成新扫描后，旧下载才返回 401，complete 又把任务设为 auth_wait；续期扫描通知已被消费，需要等下次定时扫描。
- 先 RED 后修复：只在拒绝带非空旧凭证指纹、当前 managed 新凭证有效、该根任务拥有完全匹配的新扫描资格时恢复 pending；否则保持 auth_wait，不消耗失败预算。同一凭证再次拒绝不会形成无预算重试。
- 主程序原回调提取为共享 applyCredentialUpdate，main 与测试使用相同函数，逻辑不变。扫描周期设为 3600 秒，两种认证时序均无需手动扫描即可在测试限时内完成，新 Token 下载精确 abcdef、源操作一次；Shutdown 后管理器 closed，同目录可重新打开证明锁释放。
- 七项准入用例覆盖新扫描匹配、无扫描、版本变化、已过期、相同 Token、非托管与缺失指纹；后六项仍等待。既有 unknown/cancel 处理优先级不变。
- 独立审查指出扫描时间戳可能误认旧扫描，已改为在新 Token 的 list 请求处设可取消 barrier，等其实际结果发布再返回旧 401；移除修复重新 RED、恢复后 GREEN。两平台全模块 race/vet、Node 14 项通过；最终测试同步与退出补项后两平台 main/downloader race/vet 再通过。
- 集成测试不含进程 main() 的监听/信号路径或真实百度 OAuth；文件流为本机 HTTP，其他百度接口是替身。包内进程启动/停止证据见本节交付记录，不据此扩大为真实凭证续期验收。旧包保持不变。

## 凭证轮换与下载并发集成验证（未改产品、未重新打包）

- 新增实际 credentials.Manager、Downloader、私有磁盘存储与本机 HTTP 文件流的跨模块测试，使用与主程序一致的凭证更新/认证拒绝回调；测试显式控制扫描，没有运行主程序扫描循环。
- 当前旧下载返回 401，实际后台 Manager.Run 被拒绝通知唤醒，完成一次刷新；另覆盖先轮换成功、旧下载随后返回 401，迟到拒绝不撤销新凭证。两种场景均保留失败预算、等待新扫描，随后以新 Token 下载精确 abcdef，并仅以新 Token 后处理源项一次。
- 直接读取真实 credentials.json 对应存储，核对轮换后的 Access/Refresh、同账号绑定和 Generation=2；未输出凭证。Windows UCRT 与 WSL 全模块 race/vet、diff 检查通过。
- 独立只读审查未发现确定重要问题；按审查建议补上超过 worker 轮询间隔的观察窗口及扫描资格为空断言，两平台定向 race 再通过。该观察不替代主程序自动扫描接线验收。
- OAuth Refresh/Account 和百度文件管理是替身，文件流为真实本机 HTTP；不等同于真实百度续期、多日运行或跨重启凭证读回验收。现有 v0.6.2-preview.1 产品源码不变，继续使用该独立测试包。

## v0.6.2-preview.1（分块短流恢复测试包已交付）

- 源码 revision：`88807c8302134223195b9918e29dfa7f22f5b92f`；ZIP：发布根目录 v0.6.2-preview.1.zip，9,992,101 字节；SHA256 `323bbdb948d800fdbd98bf7c0c89205a5ab07af871a5f06913072c5dab9b1ddb`。
- 干净提交打包 Node/Go/vet 门禁、三平台构建、11 项解压哈希和 ARM64 ELF machine=183 检查通过。Windows/Linux AMD64 包内脚本实际启动，健康版本/revision/等待授权正确，认证主页、入队页、状态 API 200，匿名 401。
- 运行仅在 verification 副本，无百度凭证，HTTPS 指向拒绝连接代理。Windows Ctrl+C 后 Service stopped（外层 PTY exit 1）；Linux 精确核对进程后 SIGTERM、Service stopped、exit 0。验证服务已停止，正式包无运行数据；详情见发布目录 v0.6.2-preview.1-validation.md。

- 实际 HTTP 分块响应没有 Content-Length，正常 EOF 但不足网盘元数据大小时，旧实现只报普通长度错误，持久化队列直接 failed，不进入后台重试。已先 RED 复现。
- 现该短流错误保留 io.ErrUnexpectedEOF 错误链，复用既有有界退避和预算；不生成完成凭据、不发布、不执行云端后处理。超长响应、错误范围、声明长度不匹配的拒绝逻辑不变。
- 新增真实本机 chunked HTTP/临时磁盘/持久化调度器测试：保留 3 字节后以 Range bytes=3- 恢复为 abcdef，只后处理一次；持续短流耗尽一次重试预算后 failed，无最终文件、无源操作。Windows UCRT 与 WSL 全模块 race/vet、Node 14 项通过。
- 独立只读审查及非缓存短流/Range/源操作定向测试通过，未发现新增确定重要问题；错误范围、超长数据、取消及磁盘写入错误不会被该分支覆盖。git diff --check 通过。
- 补充实际关闭/重开持久化下载器验证：retry_wait、启动次数和重试截止时间保留，直接读回临时内容 abc；截止时间过去也不发起文件流请求；新扫描后 Range bytes=3- 续传，持续短流按已保存预算耗尽，无最终文件/源操作。两平台全模块 race/vet 通过；最后补充内容读回断言后两平台定向 race 再通过。独立审查未发现确定重要问题。
- 没有访问真实百度账号或部署；v0.6.1 原包保持不变。

## v0.6.1-preview.1（校验取消修复测试包已交付）

- 源码 revision：`d488e25bbe2e66fcad3924c4a8b7d4a9bde3b703`；ZIP：发布根目录 v0.6.1-preview.1.zip，9,991,746 字节；SHA256 `55559ad63316664253e0c2a1756a1090efdeb6b8438aa489de7c3768024d177e`。
- 干净提交打包门禁、三平台构建、11 项解压哈希和 ARM64 ELF machine=183 检查通过。仅在 verification 副本运行，Windows/Linux AMD64 启动、版本/健康、认证页面/API 200、匿名 401 通过；无百度凭证、HTTPS 指向拒绝连接代理。
- Windows Ctrl+C 后 Service stopped（外层 PTY exit 1），Linux 精确核对进程路径后 SIGTERM、Service stopped、exit 0。服务均停止，原 main 工作区干净，无推送/部署；详细记录在发布目录 v0.6.1-preview.1-validation.md。

- 三处 SHA256 校验接入任务 context，分块读取前后检查取消；包括最后一次读取同时返回 EOF 的情况，取消后不生成有效摘要。
- 已复现完整临时文件恢复校验被取消后误走重置分支、将文件截断为 0 的问题；现收到取消直接退出，保留绑定的临时文件。正常摘要和非取消恢复规则不变。
- 先 RED 后 GREEN：读取取消、最后读取取消、打开文件前取消，以及真实 64 MiB 临时文件保留均有回归测试。Windows UCRT、WSL 全模块 race/vet、Node 14 项和独立只读审查通过。
- 取消检查在读取块之间生效，无法打断已阻塞在操作系统内的磁盘 Read。未访问真实百度账号，unknown 目录仍隔离，无人工放行。

## v0.6 实际浏览器补充验证（未改产品、未重新打包）

- 从 verification 副本运行实际 v0.6 包，使用隔离合成任务、无百度凭证。内置浏览器直连无法操作 Basic 原生框，保留该未验收项。
- 经临时本机 HTTP 转发入口，在只读资源请求中完成实际 Basic 挑战、清除拦截后，真实任务页通过浏览器认证缓存建立 WebSocket；状态帧显示 source_unknown 与禁止重发，实际截图保存在发布目录 v0.6.0-browser-reconnect.jpg。
- 服务停止后显示断线/3 秒重连，服务重开后无需刷新自动恢复并保留隔离任务。轮询页、实际扫描提交、fragment 只导入和未授权手动预检拒绝也完成实际页面验证。
- 详细条件、证据和边界见 [browser-validation.md](browser-validation.md)。不把受控 HTTP 入口证据扩大为原生登录框、TLS 反代或真实百度/NAS 已验收；所有测试服务已停止，原 ZIP 不变。

## v0.6.0-preview.1（源后处理完成与重放保护测试包已交付）

- 源码 revision：`62586afd2809e1b54d5072a95eed783224830dad`；ZIP：发布根目录 v0.6.0-preview.1.zip，9,991,187 字节，SHA256 `b2c5fdc8d4b212f86c02b2fc6233fcd6cab0dc417b801cf4fcc5ea275a8b5a88`。
- 干净提交 Node/Go/vet 门禁、三平台构建、11 项解压哈希和 ARM64 架构检查通过。Windows/Linux AMD64 包内程序从验证副本实际运行，合成 sending 恢复为 source_unknown；状态/健康正确，取消与重试 409，匿名 401。没有配置百度凭证，HTTPS 指向拒绝连接代理。
- Windows Ctrl+C 日志停止（外层 PTY exit 1）；Linux 精确核对进程后 SIGTERM、Service stopped、exit 0。所有验证服务已停止，正式包无测试状态。完整证据在发布目录 v0.6.0-preview.1-validation.md。

- Rename/Delete 改为严格同步单项完成解析：顶层成功不足以证明完成，异步 taskid/业务 111、缺字段、多项、截断/读取失败、网络/5xx/重定向保持未知；禁止 POST 跟随重定向，原 307 反例重放 10 次已修。
- 根任务发送前原子保存 source_state=sending/source_unknown；确认成功先保存 confirmed/done。回复或凭据保存丢失、崩溃重开保持 unknown，不自动/手动重发；发送前保存失败无请求。
- 隔离覆盖扫描/claim、Cancel/Retry、管理入队预检/登记及浏览资格。独立审查发现的同 ID 换路径覆写已先 RED 后修，关闭重开仍保留旧路径；同路径换 ID 也不可绕过。
- 根任务本地完成不再提前显示 done；网页状态与健康新增 source_unknown，显示禁止重发。旧无字段记录可读，旧二进制不能处理新不确定状态；升级前停旧实例并备份状态目录。
- 两平台全模块 race/vet 和 Node 14 项通过；最后补项后受影响 baiduapi/downloader/intakeapi/main 两平台 race/vet 再通过，独立最后复核无新确定重要问题。设计/证据见 [source-operation-plan.md](source-operation-plan.md)。
- v0.6.0-preview.1 已将下述业务错误恢复一并纳入；无真实账号操作，真实百度/NAS、同步响应兼容和异步完成查询仍未验收。

## 百度业务错误恢复源码检查点（已合入待发布 v0.6）

- 核对官方 SDK 后补 20016/20017/31045 的带指纹认证分类、31034 的现有有界重试；任务层验证 auth_wait 不耗预算、限流退避后恢复一次完成。
- 区分业务 errno 与 OAuth error_code 的 111：业务运行状态不再误撤销凭证；Move 无 taskid 但出现运行迹象也按异步未知隔离，文件/目录均不登记、不重发、不人工放行。
- 先 RED 后 GREEN，独立 review 发现的 Rename/Delete 单项命名空间遗漏已修并复核。两平台全模块 race/vet 通过；最后补项后两平台 baiduapi/downloader race、全模块 vet 再通过，Node 13 项和 diff 检查通过。
- 细节与后续：[provider-error-recovery.md](provider-error-recovery.md)。下一步专门核查源 Rename/Delete 的严格完成响应和 POST 重放边界；本轮分类通过不等于该契约完成。无真实账号操作，未发新包，当前独立测试版仍为 v0.5.0-preview.1。

## v0.5.0-preview.1（油猴选中项衔接已交付本地测试包）

- 源码 revision：`f5abf5500c6bdf2a7076113de09cff057fc0a70d`；ZIP 位于发布根目录 v0.5.0-preview.1.zip，9,982,087 字节，SHA256 `db5b7a164a41e60c4ea85964404e2d783f0d829024e2992c7cd8328f5ddeefaf`。
- 从干净提交打包 Node/Go/vet 门禁、三平台构建、解压 11 项哈希及 ARM64 架构检查均通过；油猴已纳入包及清单。
- Windows 与 Linux AMD64 包内启动、管理认证及新版嵌入 JS、未授权 cloud 409 验证通过。Windows Ctrl+C 日志停止（外层 PTY exit 1），Linux 精确核对进程后 SIGTERM、exit 0。验证目录与正式包隔离，所有服务已关闭；详见发布目录 v0.5.0-preview.1-validation.md。

- 原占位脚本已替换：从旧 context/已知 Vue selectedList 提取明确选中路径，多来源不一致/空选/无路径失败，不回退 FILEINFO；沙箱从 unsafeWindow.document 读取页面 Vue 对象。
- GM 仅存管理地址，默认 localhost；拒绝远端 HTTP、URL 凭证和附加参数。点击先展示管理地址/路径，再用 fragment 交给管理页；没有 API、Cookie、Token 或管理员密码桥接。
- 管理页在 API 请求前清除并解析 fragment，只填输入并提示核对账号，仍需手动预检/勾选/确认；无效载荷和无法清除时不导入。
- Windows/WSL Go 全模块测试和 vet、两平台 Node 13 项、独立只读 review、diff 检查通过。内置浏览器隔离夹具验证导入/URL 清除/账号提示/未自动预检；夹具服务已停止。
- 计划及兼容边界见 [userscript-plan.md](userscript-plan.md)。真实百度网页、真实油猴沙箱及所有 SPA 视图未验收。unknown 目录隔离规则不变；异步完成查询仍待可靠契约。
- 已交付新独立版本 v0.5.0-preview.1；已有 v0.4.0-preview.1 不变。继续推进真实适配证据与可靠异步完成查询，不据本轮夹具宣称整项目完成。

## v0.4.0-preview.1（管理页入队与任务操作已交付本地测试包）

- 源码 revision：`a232efd4b84e0d16fb6c30e58a59d19d0e15bb86`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.4.0-preview.1.zip`，9,976,240 字节。
- ZIP SHA256：`a0b5027ef2bafc7d1d953a9abda61f01b9739a265b85450dbce8cd016248a3ee`。三平台构建、解压 10 项哈希及 ARM64 架构检查通过。
- Windows 与 WSL Linux AMD64 均从 verification 副本运行包内启动脚本；无百度凭证且 HTTPS 指向拒绝连接代理。新入口匿名 401、认证后页面/脚本/记录/健康 200、未授权 cloud 409；Windows 另核对预检 409、跨站 403、release 404。
- Windows Ctrl+C 后 Service stopped（外层 PTY exit 1）；Linux 核对 /proc/exe 后 SIGTERM、Service stopped、exit 0。全部验证服务已关闭。完整证据在发布目录的 v0.4.0-preview.1-validation.md。

### 服务端入队与管理页已实现

- 设计：[intake-design.md](intake-design.md)。预检、确认、只读核对和独立 intake/ 原子日志；固定队列目标和任务模式，严格路径/身份/冲突，认证 epoch/进程绑定的短时预检。
- MoveFile 同步无覆盖、严格单项响应、禁 307/308 POST 重放。每项 sending 先落盘再发送，落盘后再查认证；未知结果隔离目标，重启不自动重发，同批逐项报告。
- 扫描和源后处理与移动按 cloud 读写锁协调；隔离目标不注册/认领/后处理，不影响无关项目。已登记根任务与完成凭据之间崩溃时保留 pending，不把刻意隔离误判成源丢失。
- 目录只有已持久化同步完成凭据时，才可按源/目标身份恢复；异步受理和无凭据未知目录持续隔离。丢失异步标记的崩溃窗口已有 RED/GREEN 回归，记录 async taskid 供后续处理。
- 本地真实 HTTP 验证 move 编码与请求参数；故障和磁盘测试覆盖部分失败、新冲突、认证撤销、发送前保存失败、回复丢失、重启隔离、目录凭据保存丢失及成功恢复。独立 reviewer 最后复核完成凭据边界通过。
- 最终 Windows UCRT / WSL 全模块 race、vet 通过；最后补的 Move 本地 HTTP wire 契约测试在两平台 baiduapi race 复核通过。前端原有 5 项执行通过，git diff --check 通过。
- 已接入受管理认证保护的 /intake、云端列表、预检、确认、记录和只读核对路由；页面展示固定目标/策略，确认必须勾选，提交前消费本地预检，异常不自动重发。
- 用户明确决定：缺少完成凭据的 unknown 目录继续隔离，暂不提供人工放行。已同步设计、计划和测试指南，API/UI 无人工覆盖入口。
- 实际本地 HTTP → 下载器 → 磁盘日志 → 重启读回验证通过，重复提交只移动一次，大整数 ID 保真；新路由匿名 401，Node 7 项、Windows/WSL 全模块 race/vet、独立审查通过。浏览器隔离夹具验证选择/预检/明确确认/unknown 提示；不能等同管理登录与真实百度验收。
- 异步任务可靠完成查询、油猴适配和真实百度/NAS 链路仍待完成；没有操作真实账号，不以本次交付代替 D 的全部验收。

- 计划：[experience-plan.md](experience-plan.md)。先补已有队列管理，入队/油猴仍待实施；原脚本经核查只是打印 FILEINFO，不能可靠认定选中项，没有移动操作。
- 两种任务页增加立即扫描和根任务取消/重试、按任务防重复提交、状态冲突反馈、取消未排空时等待可重试；显示根任务固定后处理策略、启动次数和下次重试时间。
- HTTP/WS 新增精确 task_id 字符串、can_cancel/can_retry。操作入口不从 JS Number 构造 ID，保留旧 fs_id 数字兼容查询；能力根据服务端认证、扫描资格和 active worker 计算，Retry 再次检查认证。
- 大于 2^53 的任务标识实际 HTTP/WS 往返和精确路径重试命中（409 而非 404）通过；取消未排空/源扫描资格/认证过期能力与前端五项脚本测试通过。
- Windows UCRT、WSL 全模块 race 和 vet 通过；独立 reviewer 未发现确定 important/critical，git diff --check 通过。GetTasks 的队列/任务/客户端锁顺序已复核。
- 独立包 v0.4.0-preview.1 已构建并验证，旧版 v0.3.0-preview.1 保持不变。
- 下一步继续油猴的可靠选中项适配和安全衔接，异步状态契约需独立证据；不把本次管理页交付作为 D 全阶段完成。

## v0.3.0-preview.1（阶段 C 已交付本地测试包）

- 源码 revision：`fec54acc83a0015e1ddcba5b398b4543c2b189a6`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.3.0-preview.1.zip`（9,828,655 字节）。
- ZIP SHA256：`6171ce7a98f7e30e67e2031913d52e8567cc1c215c7dcf4fcc9679d4cc93eb86`。三平台构建、解压 10 项哈希、ARM64 架构检查通过。
- Windows 包内启动、匿名 401、认证页面/API、Origin/WS 403、真实 WebSocket 设置持久化、第二实例拒绝、重启密码文件不变和锁释放验证通过；两次 Ctrl+C 均有 Service stopped，外层 PowerShell/PTY exit 1 不当作原生 exit 0。
- Linux AMD64 包内 start.sh 实际运行、Basic 健康/页面和 Bearer WebSocket、匿名 401 验证通过；SIGTERM 后 Service stopped，进程 exit 0。包验证只在 verification 目录，不改最终 ZIP/供用户使用的解压目录；未提供真实百度凭证且强制拒绝连接的回环 HTTPS 代理。
- 验证明细位于 releases 同级 `v0.3.0-preview.1-validation.md`。真实百度、目标浏览器原生 Basic+WS、NAS/Docker/ARM64、多日/断电和反代未验收。服务已停止，未推送或部署，原 main 工作区保持不变。

- 集成网页设备授权、凭证保存/应用/续期、管理 Basic/Bearer 和 Origin/WS 校验；无 Token 也可启动管理入口，下载先等待授权。随机管理员密钥放在私有 admin-key.json，日志不输出密码。
- 下载器按认证状态和有效期限制扫描、新任务及源操作；auth_wait 持久化且不消耗下载失败预算，恢复后必须新扫描。Token/有效期变化递增 epoch，旧扫描结果不能解锁执行；旧请求认证拒绝带凭证指纹。
- Manager/Scanner 与 HTTP 请求均纳入取消生命周期，后台退出后才关闭共用存储。健康结果区分管理连接、账号可用、扫描结果；主界面按实际认证状态显示。
- 本地真实 HTTP/WS 测试验证整站匿名 401、Bearer/Basic、Origin 403、WebSocket 状态；本地主程序 HTTP 授权测试经过实际 Manager/Downloader/文件存储，成功应用但不回传 Token。设备 session 的 interval/TTL 由服务端持有，新会话取消旧轮询。
- Windows UCRT / WSL 全模块 race、vet 通过；最后新增 main/OAuth 测试两平台定向 race 通过；Node 前端脚本执行 3 项通过；PS/sh 语法和 git diff --check 通过。独立 reviewer 最终未发现新的确定 important/critical。尚未将这些本地验证当作真实百度/真实浏览器验收。
- 更新原生启动、Compose 环境与测试指南；交付目录、源码 revision、哈希和包验证见本节开头。

## 阶段 C：认证核心源码检查点（历史，3c4dfc8）

- 设计与计划：[auth-design.md](auth-design.md)、[auth-plan.md](auth-plan.md)。OAuth 增加 Context、请求/响应上限、拒绝重定向、稳定且不含 Token/原始响应的错误；保留旧函数签名兼容现有调用。
- 新增 credentials 管理器和独立 credentials.json / account.json。手动授权先校验账号再保存；自动刷新先保存轮换结果再校验；启动优先恢复已保存凭证，避免环境旧 Token 覆盖。只持久化应用标识和 Token，不保存 App Secret；对外 Status 不含凭证。
- 串行应用与续期、提前刷新、暂时失败退避、账号绑定、迟到旧 Token 拒绝隔离。失败只能保留现有 Ready，只有成功验证并保存后的 publish 才能重新授予 Ready。
- RED/GREEN 回归覆盖：取消执行中的刷新不永久阻断续期；BindAccount 暂时失败保留候选；退避期间到期及时撤销；手动失败唤醒后台并真正重试；退避中的真实认证拒绝立即撤销；暂时刷新失败不能复活已被拒绝的凭证；轮换后账号查询取消仍可恢复。
- 独立 reviewer 发现的恢复与调度问题均修复并定向复核，最后完整 credentials 非缓存测试通过；主线程最终 Windows/WSL 全模块 race 与 vet 通过。没有访问真实百度账号。
- 当前仅是核心模块和下载器存储桥接，尚未接入 main 的网页授权/后台生命周期，尚无管理认证、Origin/WS 校验、下载 auth_wait 门禁和新健康状态；不能据此认定网页登录/自动续期已经在运行中的程序生效。
- 没有为这个中间源码检查点重复打包。当前可测试版本仍为下面的 v0.2.0-preview.1；完成 C 集成、HTTP/WS 冒烟与包验证后再交付下一独立版本。原 main 工作区保持不变，没有推送或部署。

## v0.2.0-preview.1（阶段 B 已交付本地测试包）

- 源码 revision：`4a1b7e398f87d7a6855beaeaefe606350b4346d7`；包：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.2.0-preview.1.zip`。
- ZIP SHA256：`40386eae3747042106fbea49016efe0082e8a8b705703eee705be9971d6ad138`。三平台静态构建通过，最终解压 10 个文件哈希通过，ARM64 ELF machine 核对通过。
- 最终 Windows UCRT/WSL Go1.22.2 全模块 race、go vet、git diff --check 通过，PowerShell 与 sh 启动脚本语法通过。
- Windows 包内 Start.ps1、版本/并发配置、页面/资源/状态/扫描/未知任务 404 路由通过。相同目录第二实例退出 1；WebSocket 保存模式后 Ctrl+C 日志 Service stopped；重启同一目录并再次读取模式验证持久化与锁释放。
- Linux AMD64 包内 start.sh 实际启动，通过本机健康检查，SIGTERM 后 Service stopped 且进程 exit 0。
- 打包后运行均仅用假 Token，百度 HTTPS 强制走拒绝连接的回环代理。验证目录的模式切换只影响测试目录，不在最终 ZIP 或供用户使用的解压目录内。
- Docker/ARM64 实机、真实百度账号/NAS、多日运行、突然断电未验收；没有推送或部署，原 main 工作区未修改。

- 目标：持久化根任务与默认模式、有界 worker、取消/重试、故障退避和服务优雅退出。设计与实施记录：[queue-design.md](queue-design.md)、[queue-plan.md](queue-plan.md)。
- 存储采用逐任务原子 JSON，不增加 SQLite 依赖；避免每次变化重写整份历史。OS 文件锁保护新版同一下载目录单写入，进程退出自动释放。损坏记录/未知版本/队列绑定变化拒绝启动。
- 根任务固定入队时的后处理模式；默认模式单独持久化。重启中的任务等待扫描确认后恢复，根任务离开队列时转为需人工核对的 failed；不依据旧路径执行源操作。
- MAX_PARALLEL 默认 2，MAX_RETRIES 默认 3，退避从 5 秒开始、上限 5 分钟。失败预算与启动次数分离，重启/取消不会耗尽失败重试预算。
- 全链路 context 取消；元数据请求总超时 30 秒；流下载空闲 90 秒超时，持续传输可以超过空闲时限。保留 HTTP 429/5xx、断流、网络超时错误链，其他错误不盲目自动重试。
- 增加根任务取消与手动重试 API，状态输出 is_root / attempts / next_attempt / download_mode。扫描请求合并，不为每次点击创建 goroutine；SIGINT/SIGTERM 停止 worker、扫描和 WebSocket。
- 故障测试：进程锁排他与崩溃释放、重启扫描门禁、队列/设置往返、并发上限、固定策略、取消源操作保护、退避预算、完整错误链、3000 子项失败后重开、状态锁阻塞时 Shutdown deadline。
- 独立 reviewer 两项 important（非 JSON 503/读取错误误分类、超大错误记录导致重启失败）已修并有回归测试；最新复核无新增 important/critical。补充了持续有数据的下载总耗时超过 idle limit 的测试。
- 包验证明细见 releases 同级的 `v0.2.0-preview.1-validation.md`；根队列持久化与网络故障链路是本地真实 HTTP/磁盘测试，不等同于百度 E2E。
- 下一阶段继续 C：OAuth 应用/保存/续期、管理认证、健康状态；随后 D：入队与任务管理体验。整体目标仍进行中。

## v0.1.0-preview.3（已交付历史版本）

- 最终源码 revision：`5a270f1b2050e1fa8483af3960c16a17951eea0e`。
- 当前交付：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.1.0-preview.3.zip`。
- 当前 ZIP SHA256：`8be839e5664818a7063db68cd6b1811af61db6c0d9129e49f1a3a2d632146440`。
- 最终代码 `go test -race -count=1 ./...`（Windows UCRT）、`go test ./...`、`go vet ./...` 通过。包内 10 个文件哈希通过；实际 Start.ps1、Windows 本地 HTTP 页面/API/资源/手动扫描通过并已停止；Linux AMD64 包内版本命令运行通过，ARM64 ELF machine 检查通过。Docker/ARM64 实机与真实网盘/NAS 尚未验收。
- 下面 preview.1/preview.2 记录为内部候选历史；使用此节的 preview.3 包和哈希。

- 工作树：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-codex`；分支 `codex/downloader-reliability`；基线 `331a8b1`。
- 配置：统一 LOCAL_DOWNLOAD_DIR，保留旧变量兼容。独立监听地址默认回环，容器宿主端口默认回环。
- 传输：Range 忽略后从头请求；验证 Content-Range；元信息绑定临时文件；完整长度、同步、关闭后生成 SHA256 凭据；无覆盖发布；无凭据旧文件报告冲突。
- 目录：子项失败汇总，全部成功才处理顶层；不逐项改名；旧标记子项阻止整体后处理。
- 后处理：显式失败可重试，复用经过 SHA256 检查的本地文件；检查 filemanager 子项 errno。
- 并发：扫描执行前认领顶层 fs_id；客户端替换加锁；任务返回不可变快照；扫描时间用 atomic。尚无限并发队列。
- 分页提前：通过 SDK 源码确认 start/limit 参数，循环读取；重复 FSID 或无成功标志/无列表/HTTP 错误直接失败，避免空目录误判。
- 新增本地 HTTP/磁盘故障测试；先观察 RED 再实现。RED 覆盖忽略 Range、错误范围、配置兼容、短流、目录误成功、无凭据旧文件、后处理失败、完整大小失败残片、1000 项分页、错误目录响应、旧标记子项和官方 server_mtime 字段。
- 独立只读 reviewer 报告的问题已修复并经其非缓存测试复核；无剩余确定的 critical/important 问题。断电、远端并发修改、硬链接/NAS 与配置根路径符号链接属于明确边界。

## 已执行验证

- Windows Go 1.27.1：`go test -count=1 ./...`、`go vet ./...`、`git diff --check` 通过。
- Windows race：默认 GCC 为 MSYS 编译器，初次失败；改为 `C:/msys64/ucrt64/bin/gcc.exe` 后 `go test -race -count=1 ./...` 通过。
- WSL Go 1.22.2：`go test -race -count=1 ./...` 通过，验证最低项目版本与 Linux 文件行为。
- 打包脚本与 Windows 启动脚本 PowerShell 语法解析通过。
- 最新并发扫描/快照测试已通过 Windows/WSL 全模块 race 复核。
- 内部 preview.1 三平台构建通过；解压后 10 个文件哈希通过，Windows/WSL AMD64 --version 通过，ARM64 ELF machine=183 通过。Windows 回环 HTTP 健康/页面/资源/状态/手动扫描通过；全部百度请求强制走拒绝连接的回环代理，不访问真实账号。
- preview.1 冒烟发现 url.Error 会把带 Token 的请求 URL 带入日志；增加先失败的凭据泄漏测试，脱敏各 API 的请求错误。最终对外交付 preview.2，内部 preview.1 保留为不可变历史，不建议测试。
- 最终 preview.2 源码 revision：`d56d114dcbdab79b720ed40a815d17ecc4f5110a`。三平台构建通过，解压后 10 个文件哈希通过；实际运行包内 Start.ps1，Windows 回环 HTTP 健康/页面/资源/状态/手动扫描通过，网络错误日志不再出现假 Token。WSL AMD64 --version 与启动脚本语法检查通过。冒烟进程已停止。
- 交付包：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.1.0-preview.2.zip`；SHA256：`d21a8e936955925575aaa76c45ce694de87533acaacfb493ba1cdc9a9e1efcae`。
- 包含 Windows AMD64、Linux AMD64/ARM64 二进制、两种原生启动脚本、Docker Compose/运行镜像配置、测试指南、版本来源和 SHA256 清单。ARM64 只完成交叉构建与 ELF 架构检查，Docker 运行配置只完成静态检查，未运行 Docker。
- 原项目 main 工作区仍为基线且干净；优化源码位于隔离分支，未合并/推送。

## 下一阶段

继续 D：任务取消/重试等管理界面和入队体验。先核查原油猴脚本实际能力、选中文件数据来源与新管理认证/Origin 边界，再确定可验证的入队流程。A/B/C 的本地实现和预览包不是全目标或真实网盘无人值守验收完成。

## 未验收

真实百度账号下载、真实 NAS/容器运行、突然断电、多日稳定性、非本地文件系统挂载。未推送、未部署、未修改真实网盘。
