# v0.8.31 空目录上传设计与实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. 使用主目录内联执行，不创建worktree，不自动提交。

**Goal:** 目录上传能够保留用户明确选择的空目录，批次历史、取消和重启都能正确表达目录结果，不将目录伪装成零字节文件。

**Architecture:** 文件传输保持现有Runner与会话协议。目录作为独立持久化对象保存，不使用文件暂存/分片/秒传协议；单worker先处理目录创建，再处理同批文件。复用MutateCloud的命名空间协调与远端操作证据。

**Tech Stack:** 现有Vue/TypeScript、Go、SQLite；不增加前端框架或运行时依赖。

**Spec:** 本文设计部分与docs/ai/netdisk-product-roadmap.zh-CN.md的P0-C。

## 已核实的当前实现

- UploadWorkspace.vue以File.webkitRelativePath/name生成files清单，没有目录身份信息。
- uploads.Input/Item、Create、Receive、worker、Retry/Cancel与恢复均围绕文件；10000上限目前是文件数。
- UploadStagedFile能创建文件的缺失父目录，但没有文件的空目录不经过该路径。
- recoverInterruptedUploads把submitting/finalizing恢复为unknown，不能把目录重启恢复误接为可重传文件。
- HTTP创建清单拒绝未知字段；新协议必须先有后端支持，旧客户端保持可用。

## 用户交互与兼容性

保留普通文件/目录选择。另提供“选择完整目录（含空目录）”，仅在支持只读showDirectoryPicker且安全上下文时可用。用户主动点击后读取选定根目录，枚举期间显示文件/目录计数，可取消；任何枚举失败不创建远端批次，不提交残缺清单。明确显示选定根目录、文件数、空目录数与总文件容量。

普通FileList无法证明完整空目录清单，提示不包含空目录，不能自动声称保留全部目录。不要求用户为既有普通上传修改NAS访问方式；完整目录能力需要HTTPS或浏览器认定的可信本机上下文。无权限、用户取消、目录枚举中断分别处理。句柄只留当前页面，不持久保存用户本机访问权限。

依据：https://developer.mozilla.org/en-US/docs/Web/API/Window/showDirectoryPicker （安全上下文、浏览器支持有限、用户激活、默认read）；https://developer.mozilla.org/en-US/docs/Web/API/FileSystemDirectoryHandle/values （异步目录条目）。具体浏览器验收需独立记录。

## 持久化与操作语义

采用独立upload_directories表（batch、id、relative_path、state、updated_at_utc、actual_path、operation_id、error），避免改写现有文件项/会话/统计的含义。旧数据库通过CREATE TABLE IF NOT EXISTS兼容；目录事件与状态同事务。API创建新增可选directories，响应新增directories；旧files与items保持原语义，允许纯目录批次但总清单上限仍为10000项。

规范化路径规则沿用文件规则，目录/文件重名冲突和“文件作为父级”均拒绝；目录祖先重复可规范化去重，但不得静默改变文件清单。由服务器验证，不信任浏览器。上传根目录也是相对目录，必须保存，以便纯空根目录可创建。

目录按深度、路径稳定执行。存在同名文件明确失败；已存在目录通过在线核对记录为existing，显示“目录已存在”，不能写成“本次创建成功”。缺失目录先落sending证据，再调用既有MutateCloud；confirmed需有远端身份/操作关联。写入中断、响应未知或回执保存失败保持unknown，阻止关联文件继续，重启不自动重发。只有明确未发送/失败且可重试才允许重试；目录不进入文件uploadid恢复、字节速度与ETA。

目录请求发送后停止不强制认定已取消；仅停止尚未发送目录。取消和清理不删除网盘目录，目录无本机.data可回收。真实账号未核验前目录worker同样等待。创建成功使相关缓存失效/刷新；目录任务不绕过下载队列协调和路径门禁。

## Global Constraints

- 固定v0.8.31；同版不移动标签；不操作真实账号做开发测试，不复制账号数据。
- 不覆盖用户网盘文件，不把unknown视为可重试，不用存在路径推断本次创建完成。
- SQLite时间记录UTC，区分本次响应、历史完成与当前在线观察。
- 页面离开/迟到枚举/请求不修改新页面；显式确认前不产生远端写入。

## Review Focus

纯空根目录；文件与目录同名/父子冲突；枚举权限中断；mkdir成功但保存失败；重启时sending隔离；目录完成但文件接收缺失；旧客户端/旧数据库；浏览器不支持与HTTP NAS访问。

## 实施与验收顺序

- [ ] 1. internal/uploads新增directory_store.go/directory_store_test.go：创建与读取目录清单、事务事件、合法路径/重复/父级冲突、纯目录批次、总量上限、旧库兼容；先失败测试，再最小实现。调整http.go接收可选directories，旧files协议不变。
- [ ] 2. 新增directory_worker.go/directory_worker_test.go与downloader/upload_directory.go：独立目录runner，复用MutateCloud协调、账号门禁与操作证据；测试同名文件拒绝、已存在目录、成功、明确拒绝、unknown、存储失败和重启不重发。目录未完成/unknown时关联文件不得准入。
- [ ] 3. 接入取消/重试/批次汇总：尚未发送可停止，inflight不误报取消，unknown不可重试；目录不算上传字节，不读取/回收文件暂存，不暴露文件会话入口；HTTP和SQLite重开测试。
- [ ] 4. frontend新增完整目录枚举与预览组件及测试：只读枚举、总数边界、取消、深层空目录、权限失败不提交、离页迟到结果；UploadWorkspace在后端支持后再显示完整目录入口，回执按身份映射验证，避免依赖随机UUID排序。
- [ ] 5. 批次详情独立显示目录结果，文件统计保持原口径；类型检查、前端全部回归、目标Go测试、桌面/窄屏/键盘与模拟目录链路。独立审查后跑完整打包门禁并核对运行数据，覆盖同版测试包。真实百度/NAS另列未验收。

## 当前状态

2026-11-05：完成源码与浏览器接口调查、设计及分步计划；尚未实现目录协议，运行目录仍为已交付的可读性构建。不能将此计划写成已具备空目录上传。后续从步骤1开始，当前不修改产品API或运行数据。

## 实施记录：目录存储与执行门禁

2026-11-05，源码增量，未发布：directory_store.go新增目录表/事件表、规范路径与稳定排序、目录清单同事务保存及sending重启隔离；store.go保留Create旧协议，内部createManifest支持文件与目录，Get读取目录快照。目录创建与初始事件保存失败整批回滚。worker候选与claim原子准入均阻止未确认目录下的文件执行；Get显式关闭文件rows后读取目录，适配单SQLite连接。

步骤1仅存储部分完成，HTTP协议仍不接受directories（避免目录runner尚未接通就准入任务）。批次List目录汇总、目录执行与操作证据、取消/重试、前端和正式发布仍待做。此分阶段顺序不改变最终目标。

测试先以缺少createManifest/Directories失败，随后新增sending重启测试复现未隔离，再实现恢复；7个目录测试通过，覆盖纯目录、非法/冲突、合计上限、去重、事务故障回滚、重启UTC证据、旧库文件状态/事件不变、文件执行门禁。CGO_ENABLED=0上传模块完整回归22.203s通过，go vet通过，diff检查通过。所有SQLite均在t.TempDir，无真实账号/服务/API或运行数据操作。未以此宣称完整空目录上传可用。

## 实施记录：目录串行执行与回执

2026-11-05，源码增量，未发布：新增directory_worker.go，由既有单worker优先执行目录，受账号readiness、文件unknown与目录批次状态门禁约束；发送意图及回执均与事件同事务提交。确认回执校验请求路径、正整数远端ID、UTC及新建操作ID；已存在目录单列existing，不伪造创建凭据。非法/丢失回执进入unknown，明确未发送/拒绝为failed；failed/unknown阻止同批后续目录。回执写入失败保持durable sending并停止worker，重启由已实现恢复逻辑隔离，不能作为当前活跃worker证据。

新增downloader/EnsureUploadDirectory，检查父目录、同名文件、身份与账号代次，复用MutateCloud的SQLite操作日志、下载维护、缓存更新与隔离；创建回执之后再读取目录身份，不能唯一确认则unknown。main适配目录runner，未改变旧文件runner/上传并发。未知mkdir即便路径线上已出现仍保持隔离，不按存在路径放行。

TDD：先观察缺少DirectoryRunner/执行入口及EnsureUploadDirectory导致失败，再实现。目录存储7项+worker7项通过（含意图失败不发送、回执失败留sending、未知重启不重发、错误成功回执不放行、账号未就绪、已有目录、明确失败阻塞）。模拟downloader两组测试覆盖已有目录/同名文件、创建成功/丢回复、未知路径再次调用不发送；最终复跑通过。上传模块全量20.573s通过，相关Go vet通过，主程序go test . -run '^$'编译通过（无主程序测试执行），diff检查通过。

尚未开放HTTP directories；取消/重试与批次目录汇总、前端完整目录选择及详情、独立审查/完整包仍待做。未运行完整downloader回归，未替换安装程序，未调用真实百度或操作真实账号数据。

## 实施记录：目录控制、汇总和HTTP协议

2026-11-05，源码增量，未发布：directory_control.go增加独立directory_summary（total/queued/sending/confirmed/existing/failed/unknown/cancelled），Create/Get/List保持原文件total/completed/字节含义。List仅取目录状态汇总，不读取目录全部清单；关闭原文件rows后再查询，避免单连接占用。

Cancel在同一SQLite事务中保存queued目录的取消事件/状态和既有待执行文件取消；sending/unknown/confirmed/existing/failed目录保持原状态，不删除远端目录。目录取消事件失败会回滚文件与目录全部变更。Retry只重排failed目录，并保存原更新时间/操作ID；证据失败回滚，unknown/sending/cancelled保持。原有文件失败重试仍沿用暂存检查与会话规则。

HTTP创建开放可选directories；纯目录批次可创建，需已配置目录runner，缺能力返回409且不落库。旧files清单可继续使用，不生成虚假目录汇总；未知请求字段仍拒绝；目录ID不能用于内容接收。main已在上一轮安装目录runner，此轮未改动远端执行语义。

TDD：控制/汇总测试先以DirectorySummary缺失失败，随后实现。7个控制/HTTP测试覆盖执行中取消后正常保留返回回执、只取消queued、取消证据失败整批回滚、重试只处理明确失败并保留原时间、重试事件失败不改变状态、纯目录HTTP及列表汇总、旧协议与缺能力拒绝。上传模块完整回归24.025s通过，go vet及diff检查通过。累计目录专项21个。尚未完整downloader/真实百度/NAS验收。

步骤1至3后端基本接通，步骤4浏览器完整枚举/预览与目录结果UI尚未接入，步骤5端到端/审查/正式包待做。当前安装仍为已交付可读性构建，不对真实运行数据库执行迁移或服务启动。

## 实施记录：网页清单与审查收敛

网页支持只读完整目录选择、枚举进度、取消、根目录/文件数/目录数/空目录数/容量预览与明确确认；普通FileList上传兼容。不支持的浏览器/上下文明确说明。完整枚举保留原始File与相对路径映射，纯目录也可提交；按集合验证目录响应，迟到枚举在历史导航/pagehide后失效。权限/遍历失败不提交残缺清单，保留原选择但取消确认。目录结果独立表格/筛选/分页/操作记录，目录失败可重试，停止确认分别显示文件和目录范围；纯目录批次隐藏0/0文件传输区。

类型/构建、全前端453项通过，其中新增4个枚举测试、6个页面测试。模拟浏览器确认7目录筛至1项、390px停止确认框与body无横向溢出、取消后焦点返回；截图接口不可用，未完成视觉截图验收；原生目录选择器由模拟句柄覆盖，真实浏览器授权/原生选择仍未验收。临时服务与脚本已清理。

独立审查发现并修复两个当前进程隔离缺口：存在目录查询未与namespace writer协调、mkdir成功但回执保存失败未立即隔离。新增回归先失败后通过；observe持cloudMutex并前后核对隔离，调用MutateCloud前释放；mkdir准入与回执失败隔离复用既有网盘路径。创建已确认但后续身份未知时保持隔离。

复核发现上传目录未知隔离跨重启缺少桥接，现从上传SQLite的unknown/sending恢复路径，main用atomic readiness关闭准入直到恢复成功；不改写真实confirmed远端回执。SQLite重开测试通过，独立复核未再发现具体问题。上传模块全量23.820s、相关vet、主程序编译通过；最终新增HTTP→共享worker→SQLite重开测试通过（无文件runner调用）。目录专项现23项，downloader定向及copy回执故障回归通过。完整发布门禁尚待执行；真实百度/NAS和原生picker另列未验收。
