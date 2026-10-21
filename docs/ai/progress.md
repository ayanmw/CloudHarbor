# 持续优化进度

## 总目标

按已给出的 A/B/C/D 优化路线持续推进，阶段性交付独立可测试版本。首版不是整个目标完成。设计见 [reliability-design.md](reliability-design.md)，当前计划见 [reliability-plan.md](reliability-plan.md)。

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
