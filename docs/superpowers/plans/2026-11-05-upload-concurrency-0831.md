# v0.8.31 受控上传并发设计与实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. 主目录内联实施，不创建worktree、不自动提交。

**Goal:** 网页可选择1或2个并行文件上传，默认1；文件数据可并行，命名空间写入串行，暂停/未知/重启证据保持可靠。

**Architecture:** 先拆分上传共享扫描租约与独占命名空间阶段，再引入有界worker池。目录创建、预创建/合并/确认仍走统一维护与cloudMutex；不得用两个worker绕过扫描和目录隔离。

**Tech Stack:** 现有Go context/同步原语、SQLite、Vue/TypeScript，无新依赖。

**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-C第2项及本文设计。

## 当前源码证据

- uploads.Open只启动一个worker；claimUploadAttempt在SQLite原子核对queued、文件unknown和未完成目录，activeAttempts按item维护。
- UploadStagedFile在文件传输全过程调用reserveCloudInventoryScan并持有inventoryScanStarting；第二个上传无法取得相同预约。
- uploading阶段释放cloudMutex/下载维护；submitting与finalizing阶段重新BeginMaintenance并加cloudMutex。BeginMaintenance遇到已有维护直接返回错误，不能作为两个上传间的等待锁。
- 父目录创建先于文件扫描预约，使用MutateCloud；必须涵盖并发文件创建同一父目录的竞态。不能先给整个文件持path-sensitive读锁再嵌套父目录MutateCloud，既有注释明确指出等待writer时可能死锁。
- 单SQLite连接不是并行HTTP的障碍，数据库事务仍短暂串行；HTTP调用不能持Manager.mu或SQLite事务。
- 目录worker、未知路径跨重启恢复、可信会话与暂停语义已交付，必须保留。

## 用户规则与显示

上传并发默认1、仅允许1/2，在上传设置中修改并保存SQLite，重启继续使用。降低并发不强杀已准入请求，等待其返回后按新上限调度；提高到2才增加后续准入。不增加分片内部并发，避免请求数成倍增长，不承诺带宽提升。

页面区分配置上限、实际活跃文件和等待阶段；历史state不能当活worker。目录创建仍串行，不算文件字节。unknown停止同批新文件和下一次会改变命名空间的阶段，已准入请求可返回可信结果，不按“停止”伪造取消。已有完成、重试、暂停和恢复统计口径不变。

## 协调机制

### 上传组扫描租约

独立实现同一Downloader的上传组租约：第一个上传在queue.mu→inventoryScanMutex的既有锁顺序下取得扫描预约，后续上传仅加入该组；最后一个退出才释放预约。扫描/外部变更看到预约时仍遵守现有互斥，不把任意inventoryScanStarting视为上传组。租约以内部context token关联所属Downloader和有效生命周期，不能由HTTP参数创建。

上传组内部的父目录MutateCloud可在有效token下复用扫描预约，但仍执行账号核验、持久扫描游标冲突检查、下载维护、身份复核、日志与缓存更新。外部MutateCloud/扫描不能冒充组成员。任何路径失败、暂停、取消和Close都必须释放自己的引用，不能释放其他文件持有的预约。

### 命名空间阶段排队

组内目录创建与文件submitting/finalizing共用可取消的独占阶段门禁，先取得门禁再BeginMaintenance/cloudMutex，释放顺序相反。不得持cloudMutex等待门禁，不得在Manager.mu或SQLite事务内等待HTTP、门禁或下载排空。认证/上下文取消后等待者退出；已发送请求不因降低并发或普通暂停被硬取消。

已有目录观察采用已交付的观察锁和隔离复核。缺失父目录的创建与同名冲突依旧通过在线事实确认，不能因另一个上传抢先创建就把未知请求自动当成功。目录worker复用同一组/阶段机制，避免文件正在传输时目录因扫描预约被误标失败。

### 文件调度与批次隔离

仅在前述门禁通过测试后启用两个文件worker。queued→attempt开始仍原子，文件不可重复claim；目录准入保持同批串行和已完成门禁。Manager状态map全部沿用锁保护。

同批文件变unknown时，禁止新的文件claim，并在后续安全回调前重新检查。已经通过远端提交准入的请求允许返回并保存真实回执；尚未准入的关联操作进入有证据的等待/暂停状态，不伪造未知完成。补传、控制、取消、批次重试与worker并发时仍需身份/状态CAS。

## Global Constraints

- 固定v0.8.31；无新标签/工作树/自动提交或push；不测试真实账号写入。
- 默认1、最高2个文件；不并行上传分片、不忽略provider限流或扫描冲突。
- unknown不能重发或强行放行；当前/历史、本地/线上和UTC证据分开。
- 同版只覆盖程序，真实状态/下载文件保留；不自动启动服务。

## Review Focus

1. 两个文件同批不同状态：一个unknown时另一个未准入合并必须等待，已准入回执必须可保存。
2. 两个文件缺同一个父目录：不重复mkdir，不把未知目录当已存在解除隔离。
3. 租约持有人退出/错误/Close：其他文件仍在传输时扫描不能取得预约。
4. 维护/路径迁移writer等待与嵌套读锁：无死锁，不错误执行本地迁移。
5. 并发从2降1、暂停、接收和取消同时发生：不杀在途、不重复claim，SQLite重开可正确恢复。

## 实施步骤

- [ ] 1. 新增downloader/upload_admission.go及测试：两成员共享预约、最后退出释放、非成员/扫描不能加入、等待取消及失败清理；先写失败测试，保留既有扫描/变更门禁测试。
- [ ] 2. 改造UploadStagedFile、EnsureUploadDirectory、MutateCloud的组内预约与阶段门禁。使用阻塞模拟provider证明两个数据流可重叠、mkdir/预创建/合并不重叠，测试同父目录、unknown、扫描中断恢复和路径writer交互；先保持单worker。
- [ ] 3. uploads新增持久化并发设置与有界调度，拆出单文件执行体，不复制整段worker。测试两个claim唯一、上限/动态降低、Close等待、目录串行、unknown对新claim及下一阶段的阻塞、暂停/取消/重试/可信会话恢复。
- [ ] 4. HTTP设置仅接受1/2，网页展示当前上限与生效语义，保存失败保留原值、401/迟到回执不串页；不把已保存配置当实际活跃数。真实worker快照独立于SQLite历史状态。
- [ ] 5. 前端全部、目标Go/竞争检查、独立审查、模拟端到端/桌面窄屏，再执行完整同版发布门禁；记录数据哈希与未验收的真实百度/NAS/原生picker边界。

## 当前状态

2026-11-05：完成源码调查和实施设计。尚未修改上传并发、扫描租约或worker数量，当前安装仍为上一轮完整目录上传构建。禁止仅增开goroutine就宣称并发已可用；下一步从租约失败测试开始。

## 实施记录：共享预约与阶段排队基础

2026-11-05，源码增量、未接线/未发布：新增upload_admission.go，内部context token限定所属Downloader及单个成员生命周期。queue.mu→inventoryScanMutex沿用既有顺序；第一个成员取得扫描预约，加入者共享，成员退出幂等，最后成员/阶段退出才清除预约。外部扫描预约不能被当作上传组，foreign/expired/cancelled token不能进入阶段。

新增可取消独占namespace gate；等待期间不持queue.mu/inventoryScanMutex，获得门禁后再次验证token。阶段持独立引用，原成员提前退出也不释放仍在使用的扫描预约。此helper只排队，不替代调用方的BeginMaintenance/cloudMutex/账号/SQLite证据校验。

TDD先观察方法缺失编译失败，后实现。7项测试连续5次通过：共享/最后释放、外部扫描与foreign/expired、真实阻塞等待取消、阶段保留预约、并发成员串行、维护/关闭拒绝、等待中成员失效无泄漏。既有扫描准入/维护/上传/目录隔离定向回归12.094s通过；Go vet通过，diff检查通过。未运行race、完整Go/前端/可见UI或真实百度/NAS测试。

当前helper尚未接入UploadStagedFile、MutateCloud和目录runner；不影响已交付单worker行为。下一步接组内预约与远端写阶段，再以阻塞模拟provider证明数据流重叠、命名空间阶段串行，最后才开启双worker和网页设置。不把基础测试当作并发功能完成；当前安装仍为完整目录上传构建。

## 实施记录：实际上传/目录与阶段接线

2026-11-05，源码增量，未发布：UploadStagedFile和EnsureUploadDirectory接useUploadLease；嵌套父目录操作借用活token，过期/foreign不回退普通准入。MutateCloud仅对内部上传组mkdir复用预约及独占namespace gate，其他请求保留外部扫描准入；提交/合并callback持阶段门禁，释放cloudMutex/维护后再释放阶段。等待门禁后及云锁内重新核对账号代次。

同组两个文件缺同一父目录时，grouped mkdir在维护/云锁内重新核对在线唯一目录，可返回existing但不伪造操作ID。unknown目标仍先拒绝，已有文件/大小写别名/身份不一致不放行。父目录创建仍先于文件长读锁，保留原防止嵌套RLock与writer等待死锁的边界。

阻塞provider测试先复现第二上传报“扫描已在运行”，接线后数据请求可真实重叠且命名空间写请求最大并发1。共用父目录测试又复现同组维护误拒绝：增加上传维护来源标记，仅活group允许取得路径读资格，提交仍排队；外部请求和外部维护继续拒绝。过期token不回退普通mkdir。

接线检查发现普通文件可绕过已隔离但在线存在的父目录，专项先失败（已发出1次写请求），现上传前与提交云锁内检查target/祖先隔离，不发写请求；附近注释记录原因。

验证：两种真实并行调用模拟（根目录、共用缺失父目录）连续3轮通过24.514s，共用父目录只创建1次；上传租约/阶段、维护来源、过期ctx、文件会话/账号、目录隔离、扫描准入及维护定向回归22.946s通过；新增维护来源与过期ctx单独通过；Go vet/diff检查通过。未运行race/完整Go/前端/浏览器或真实百度/NAS验收。

阶段1/2基本完成；仍为单worker，未新增并发设置或修改真实运行程序。下一步有界调度/SQLite设置/同批unknown安全回调/Close等待，再接网页与独立审查、完整打包。底层并行模拟不等于当前安装版本已启用双文件上传。

## 实施记录：持久化设置与有界文件调度

2026-11-05，源码增量、未发布：concurrency.go新增默认1/仅1或2的SQLite配置与变更事件（原值/原更新时间/新值/UTC同事务），提交成功才更新内存。重开恢复设置；非法值拒绝，事件失败整笔回滚。ConcurrencyStatus是当前内存生命周期快照，区分已准入未返回和回执收尾，占槽包含两者；不是SQLite历史状态推测。

Open启动两个固定worker，默认准入仍限制1；claimNextUpload在同一Manager.mu内选queued并原子claim，避免两个worker读中同一项。设置降低不强杀在途，占槽未低于新限制前不再准入。目录执行器用独立进程内所有权保持全局串行，目录不消耗文件容量口径。

原单文件执行体移到worker.go，持有attempt所有权直到结果事务/清理结束；结果持久化与claim同锁协调，unknown落库之前不释放槽。持久化失败在释放槽前关闭新准入，但不取消同伴的已发送请求；可信同伴回执仍落库。短SQL事务持管理锁，文件删除在锁外完成。

同批unknown/未完成目录在submitting/finalizing阶段回调前复核，未进入该阶段的同伴暂停且暂存保留，后续queued不claim；已经准入并返回的真实结果正常保存。仍需审查真实precreate/session/CreateAdmission证据回调与该阶段边界的一致性，不能仅以模拟phase callback当作所有网络准入点已验收。

TDD先复现默认缺方法、单worker不能同时启动两项、同批unknown后同伴未暂停，再实现。3个配置/限额/回滚测试，4个调度/未知/Close/目录串行测试，1个并行回执保存失败及重开测试通过；上传模块完整回归31.753s、vet/diff检查通过。最后故障测试验证unknown1/confirmed1/queued1且无第三次执行。没有race/完整Go/前端/真实账号/NAS验收。

当前源码已具备双worker调度（默认限1），HTTP设置与网页尚未开放，运行目录未覆盖。下一步完成真实远端准入证据检查、设置HTTP/UE、独立审查与完整发布，仍留v0.8.31。

## 实施记录：SDK实际准入屏障与在途回执

2026-11-05，源码增量，未发布：新增mutation_admission.go，在precreate Intent、PartAdmission及CreateAdmission保存前，同一Manager.mu下核对当前批次unknown/未完成目录、全局停止准入、暂停和关闭。决策与SQLite准入事件共用锁，不能只依赖此前phase callback；未知状态事务不能插入检查与准入事件之间。

使用实际baiduapi.Client.UploadLocalFile及模拟RoundTripper，精确在phase callback返回后注入同批unknown：修复前预创建仍发送1次，合并也发送1次并confirmed；修复后submitting/precreate为0，uploading为precreate1/part0，finalizing为precreate1/part1/create0，文件均暂停。实际请求计数证明拦截在所需边界，非意外提前失败。

已准入请求的响应继续记录：rapid/create回执在同批unknown或manager关闭后可保存；已经recordUploadAttemptReturned的所有者拒绝迟到回调。观测事实与发送准入分开，不用阻塞新请求的条件丢弃旧回执。两组测试包含9个子场景通过；上传模块完整回归35.815s、vet/diff检查通过，新增分片场景及计数加强后定向复跑通过。

剩余：上传父目录mkdir走Downloader.MutateCloud，尚需同级实际准入屏障/证据关联，不能把SDK三类请求测试当作父目录分支已覆盖。之后继续HTTP/网页配置、独立审查、完整发布；未运行race/全量Go/真实provider/NAS，未更新安装程序。

## 实施记录：父目录mkdir实际准入关联

2026-11-05，源码增量，未发布：baiduapi新增内部context UploadNamespaceAdmissionGuard，上传attempt context携带guard。Downloader仅在有效上传组mkdir路径、远端操作ID已生成且全部前置检查后调用；guard核对active attempt、批次unknown/未完成目录、暂停/关闭/全局停止准入和请求目录确为文件父级。

上传SQLite先记录namespace_admitted，含batch/item/attempt/remote operation ID、请求路径及UTC，明确local_send_intent不代表远端完成；Manager.mu保留至队列SQLite的prepared远端意图提交，随后在provider HTTP前释放。这样两库的记录可以关联，同时批次unknown状态不能在检查到prepared保存之间插入。prepared失败也必须释放锁且不发请求；不持Manager.mu跨网络。

TDD：上传guard方法缺失先失败；Downloader接线前模拟复现guard=0而mkdir已发送1次。接线后guard拒绝时provider写0、远端意图0；允许时释放回调观察prepared已存在，provider观察锁已释放；prepared失败时释放且写0。2个上传层和3个Downloader层专项通过，上传模块全量36.760s、相关vet及diff检查通过。原有无guard的内部直接调用保持兼容，普通管理API不借用上传组权限。

父目录与SDK准入链路现已分别覆盖，但未以这些模拟结果宣称真实百度/NAS或完整并发产品已验收。下一步HTTP/网页并发设置及完整功能独立审查，之后完整Go/前端/发布门禁；未更新运行程序或真实数据。

## 实施记录：网页设置、证据停止与回调归属

源码已接通默认1/最高2的SQLite设置、两worker有界准入及GET/POST配置；网页按需显示配置上限、实际准入和收尾数及UTC，保存失败保留快照且不自动重发，导航/BFCache迟到响应失效。前端459项、类型/构建通过；模拟桌面/390px操作通过，截图接口不可用，未完成截图视觉验收。

独立审查发现证据INSERT失败至worker返回间仍可能新准入，现同一Manager.mu下设置全局停止；父目录prepared失败经释放回调在解锁前记录停止。有效在途回执仍可持久化。另补传输观察所有权校验，先复现returned/finished/different三种迟到回调均被接受，再修复为INSERT前拒绝且不污染当前准入。旧统计fixture建立合法所有权，不清空停止标志。独立只读复核通过。

上传完整回归36.825s通过；根HTTP配置接线测试2.024s、相关vet与diff检查通过。下一步完整同版发布门禁及独立产物/数据核对；未跑race、真实百度/NAS/原生picker，不将模拟结果当真实服务验收。
