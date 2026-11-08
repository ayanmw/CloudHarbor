# 上传会话证据与可信恢复

> 原目录内联，按已授权路线图推进；固定v0.8.31，不worktree/提交/真实账号操作。

## 核实与设计
当前SDK哈希→precreate→upload parts→create；Manager只保存阶段/字节/请求统计，缺uploadid及完整manifest，不能声称跨重启分片恢复。
官方源码核对： https://github.com/baidu-netdisk/baidu-drive-sdk-go/blob/main/baidudriver/api/upload_precreate.go 和 upload_slice.go 明确uploadid、缺失分片序号及逐片MD5。开放平台文档本轮无法直接读取；未核实会话TTL/过期及同part重复提交规则，不从其他工具猜测合同。

Task1先补证据链，不启用会话重用：SDK预创建返回完整有效计划后，在首片/最终创建前同步持久化；本机完整SHA256、分片MD5/大小、远端目标、uploadid、缺片清单、凭证指纹（不是稳定账号身份）、应用Key摘要、观察UTC与本地记账UTC独立。每片仅在MD5匹配后记小型回执，绑定该计划摘要与attempt，不能把HTTP读取量充当确认。复用SQLite events，不新增表；原始uploadid仅私有数据库，网页/日志不展示。
任一证据写入失败停止后续副作用；最终create admission/unknown保持既有隔离。缺证据的历史项不补造。上下文observer将SDK事件绑定当前Manager item/attempt，不改Runner外部签名；downloader派生上下文必须保留该observer。

## Tasks
- [x] SDK会话与逐片确认回调、完整SHA256；写失败/坏计划/坏MD5/秒传先红后绿。
- [x] Manager按item/attempt同步追加证据，计划/回执关联验证、跨重启/失败持久化测试与worker接线；不展示原uploadid。
- [x] 分片准入持久化、准入协议标记及只读journal分类，先红后绿；跨重启/失败/跨尝试核对。
- [x] 稳定账号/本机应用配置作用域与同句柄暂存重哈希基础；不单独授权恢复。
- [x] 新旧attempt关联、无活跃旧worker及最终创建准入/确认；依据provider拒绝处理无效会话，不假设TTL、不自动fallback或重发未知请求，再接可信恢复。
- [ ] 网页恢复证据与保守文案、可见验收、完整门禁和同版交付，保护账号数据。

## Ledger
起点：高级搜索版8a68f174566e69cd已发布。当前任务不触碰真实运行数据；Task1/2未完成前不覆盖程序。

2026-11-05 Task1/2：SDK同步observer和同趟完整SHA256，Manager按活动attempt/item/remote/size校验、单计划/小回执私有events追加接通；不修改items状态或sent，不改变Runner签名。plan digest预计算，回执所需片索引集合O(1)，不重复序列化整份manifest。计划/分片写失败不更新内存承认，停止后续调用。SDK预创建缺失/null block_list旧缺陷先复现（仍create且无错）再修复；明确[]有效，坏MD5无确认，秒传无会话记录。多分片含服务端已有块的完整SHA256/MD5清单验证通过。SQLite跨重启、目标/attempt/哈希/重复拒绝、写失败回滚及worker→SDK mock闭环通过，公共Batch不含原uploadid。只读静态审查无确证问题；baiduapi全包3.869s、uploads全包11.417s、主程序29.161s通过，新增uploads组3次重复3.290s及相关vet通过。
Task3尚未启用：需先记录每个分片准入/在途边界，缺确认不能推定从未发送；稳定账号/app作用域、暂存重哈希和TTL/重试合同未核实。凭证指纹只是实际token的SHA256，观察UTC为本机接收/核对时刻，不是服务端签名时间；SQLite recorded另存。继续以SQLite序号表达顺序，不从时间差补造离线时长。未发布、未操作真实账号或启动服务；运行ZIP仍DC5ACF195DA6067F49468B5EEC9D9B0DC792FB39CC15A96B8B75467F0B99A95A。

补充接线验收：最初downloader -run Upload没有匹配测试，仅算编译，不算功能通过；随后新增TestUploadStagedFilePreservesAttemptSessionObserver，使用真实Downloader/SDK与mock provider，覆盖startIntake/path-sensitive派生上下文、维护锁、SDK两检查点及最终metadata核对，2.150s通过。未触达真实百度。发布与实际会话恢复仍待后续Task3/4，不以当前证据追加代替恢复功能。

2026-11-05 分片准入：SDK在pacer/HTTP之前同步记录Admission，失败不发片；该事件仅本机意图，不证明已到达百度。Plan新增PartAdmissionVersion=1，新协议才可解释缺失准入；旧计划不补造。recorder必须先持久准入再收确认，失败不改内存，重复/错序拒绝。新增只读session_journal按item/attempt核对完整plan、序列、时间格式、state/sent、片号/MD5/摘要，再分NeverAdmitted/Pending/Confirmed；有错返回空分类及错误，不接入恢复。明确空缺片清单与旧协议分别保守处理。
验证：准入缺实现先观察编译失败后实现；SDK/Manager测试更新为计划→意图→确认，写失败阻止HTTP、重启Pending、错序/坏元数据/旧协议拒绝通过。跨尝试重启和观察时钟回拨用例保持数据库序号顺序，过去/当前尝试不混算。主程序25.492s、baiduapi/uploads全包（部分缓存）、真实Downloader→SDK mock接线1.125s及vet通过；新journal组3次重复1.778s，SDK准入/回执组3次0.146s。独立只读审查无确证问题。无schema变更/无真实账号或服务操作，运行ZIP仍DC5ACF195DA6067F49468B5EEC9D9B0DC792FB39CC15A96B8B75467F0B99A95A，未发布。
后续恢复必须重新核实稳定账号/app和暂存全哈希、确认旧attempt无活跃执行者、核对最终提交隔离与快照版本，不能直接把只读journal当恢复许可；Pending可能已发送也可能未发送，绝不因无回执重发。

2026-11-05 作用域/载荷核验：源码核实credentials.Manager在Provider.Account成功并BindAccount后才publish Ready；taskstore账号摘要来自稳定UK，token摘要仍单独记录。Downloader在读取绑定前后检查授权epoch，再在SDK ctx中记录AccountScope及本机authorization_gate_checked_at_utc；SDK在打开文件/请求前核对当前token/app摘要。缺稳定scope的历史计划不可通过作用域比较，同账号/同配置应用的token轮换可比较，不能据此断言provider issuer、应用权限或uploadid仍有效。应用Key变化原先未使epoch失效，新增回归先复现后补入epoch条件。空应用Key显式scope原先可发请求，边界回归先失败后拒绝，旧无scope token-only新上传保留兼容。
载荷verify使用同一打开句柄核对普通文件/大小/所有分片MD5/完整SHA256及实际总长，支持逐4MiB检查点中断并在成功后回起点。该函数尚未接入恢复，不以独立读回替代持有句柄后的执行校验。测试覆盖同尺寸改动、末片改动、空文件、SHA/MD5不符、取消与中途停止。Scope入库深拷贝和UTC规范Digest、跨重启读回三次通过。
验证：SDK/API全包3.962s、uploads全包11.524s，downloader上传接线/未绑定/哈希中换凭证零HTTP/应用变更epoch2.910s；此前授权相关组3.322s、主程序28.628s及credentials21.163s通过。downloader上传组3次7.391s、scope跨重启组3次1.721s及相关vet通过。主体静态审查无确证问题，随后追加空Key边界回归通过。未发布/未操作真实账号/未启动或结束服务，ZIP仍为DC5ACF195DA6067F49468B5EEC9D9B0DC792FB39CC15A96B8B75467F0B99A95A。
下一步不能把旧预创建计划改写成新凭证事实：需桥接旧plan/attempt与新执行scope，保留各自UTC/凭证摘要。最终create的准入和回执需先持久化，再接恢复；Pending不自动重发、unknown最终提交继续隔离。

2026-11-05 普通分片最终创建证据：新增CreateAdmissionVersion=1，省略的旧零值保持历史digest，旧journal只读但不能推断未发送create。SDK在create HTTP/pacer前同步记录意图；有效回执保留实际路径、fsid、大小和本机观察UTC，自动改名允许同父目录；异常回执、回执丢失及持久化失败保持unknown。Manager在所有计划缺片确认后才允许create意图，写失败不更新内存；拒绝错序/重复/其他父目录和create后的分片。journal按SQLite序号读回CreateTracked/Admission/Receipt，不改变item状态或已完成字节，回执观察仍须独立metadata核对。
验证：先观察新增SDK测试因检查点字段缺失编译失败，再实现；baiduapi全包3.834s、uploads全包14.341s，Downloader实际SDK mock接线2.638s通过；跨重启/准入写失败/回执写失败/旧协议/自动改名组3次1.662s，SDK边界组3次0.395s；相关vet和diff检查通过。适配新增检查点时保留纯分片测试原断言，worker正常路径增至5条证据。未发布、未操作真实账号或服务；运行目录仍为前一高级搜索构建。
剩余：秒传precreate分支尚无创建意图/回执journal，需独立补齐，不伪造uploadid；旧attempt到新执行的桥接、实际分片恢复和网页恢复证据尚未接通。仅普通分片create边界完成，不将本次基础设施称为已实现跨重启续传。

2026-11-05 最终核对补充：真实Downloader→SDK mock新增metadata路径变化与文件ID变化场景，均保持ErrUploadUnconfirmed、创建仅一次、原create回执仍可观察。起初怀疑调用方缺FsID比较；回归直接通过后追源码，确认baiduapi.GetFileInfo已校验返回ID，未增加重复生产判断。新增journal矛盾用例覆盖错误状态/大小/父目录/摘要、重复准入、无准入回执及create后分片；均返回空分类和错误，不能输出可恢复证据。
验证：CreateJournal组3次2.198s；API全包1.132s、uploads全包13.359s；Downloader上传/账号作用域相关组3次19.142s；API/uploads vet和diff检查通过。全部为本机mock/SQLite证据，未使用真实账号，未改运行包或启停服务。下一项为precreate可能直接完成秒传的请求意图/回执证据，必须在发precreate之前落盘，不能等return_type=2后补造“请求前意图”；普通会话及秒传保持独立类型和因果关联。

2026-11-05 precreate/秒传证据：独立UploadPrecreateIntent记录路径、大小、完整SHA256、所有块MD5、token/app摘要、可选稳定账号scope与本机观察UTC；无uploadid，不伪装成分片会话。SDK在precreate HTTP/pacer前同步观察；写失败不发请求。return_type=2回执验证canonical路径/同父目录/正fsid/大小后关联intent digest，回执写失败或响应丢失保持ErrUploadUnconfirmed，无重发。
Manager在活动attempt下保存私有precreate事件，写失败不推进内存。只读journal按SQLite序号区分仅意图/已观察秒传回执，校验item/attempt、状态/字节/摘要/时间格式。普通分片Plan新增可省略PrecreateDigest；新SDK必须匹配durable预创建的载荷、凭证和scope且无秒传回执。读回还验证预创建序号早于会话计划；旧省略字段保留历史digest，不补造请求事实。原分片observer不混入precreate形状，worker同时传递两个类型上下文。仍不改变任务完成判定和自动恢复行为。
验证：SDK新增测试先观察全部分支失败（请求先于意图），接线后通过；覆盖成功、意图写失败、回执写失败、响应丢失、错误父目录，均不重发。SQLite写失败/跨重启/重复意图/错误父目录/缺关联/不同凭证/孤立会话与worker秒传/普通双路径通过。API全包3.903s（最终缓存）、uploads全包15.324s、主程序25.435s、Downloader相关组5.607s、vet和diff检查通过；SDK边界3次0.127s、预创建/关联/worker组3次6.877s。
未发布、未操作真实账号或启停服务，无schema变更，运行目录继续为高级搜索构建。剩余旧attempt到新执行scope的桥接、无活跃旧worker核验、实际分片恢复、回执身份重新核对和网页恢复证据；precreate仅意图仍可能已秒传成功，绝不以无回执直接再发precreate。

2026-11-05 尝试生命周期：worker领取queued项改为同一SQLite事务写hashing状态及attempt_started，记录当前/前次attempt ID与相同UTC；提交成功才更新活跃映射。领取重新检查同批unknown，取消/暂停后的过期候选不会被覆盖。已有活跃attempt拒绝替换。前次ID仅历史关联，不是复用uploadid或请求重放授权。
runner返回后先追加attempt_runner_returned（本机生命周期观察，不是云端完成），再释放活跃映射并走原最终状态事务。写失败停止worker，保留原任务状态/证据，清除已实际返回的内存活跃标识以免显示假worker。失败不能宣称任务confirmed。重启后只关联旧起点，不补造旧runner正常结束；uploads.Open仍依赖主程序已持有账号目录writer锁，未扩展为多进程并发保证。
验证：新增生命周期接口缺失先编译失败再实现；领取证据失败回滚、活跃互斥、结束证据失败不承认/重复拒绝、同批unknown阻止领取、跨重启前次ID/无虚假结束、真实worker结束写失败无假活跃等组3次5.026s通过；uploads全包13.290s、vet和diff检查通过。主程序前一轮25.957s通过，最终生产改动后的重跑另登记。未发布、无真实账号/服务操作。
剩余：生命周期历史不等同恢复准入；旧plan/attempt→新scope的持久桥接、同句柄载荷复核、只发送从未准入的缺片、create未知隔离及线上回执核对仍须在实际恢复路径接通，尚未启用自动分片续传。
最终复核：主程序在最后生产改动后重跑24.901s通过；未启动服务。

2026-11-05 SDK实际恢复与桥接准入：提取新建/恢复共用uploadSessionParts，保持同一暂存句柄贯穿校验、分片与merge。新增ResumeUploadLocalFileMeasured：核实当前client scope与旧账号/app、源attempt/序号/协议、确认分片集合；同句柄完整SHA256/各MD5核验后必须同步记录新scope准入，且必须配置分片/create observer。跳过已确认片、不发precreate、保留旧plan digest/uploadid；provider拒绝旧session时直接返回，无fallback新上传。确认片仅影响进度，网络统计只统计本次剩余请求。来源Pending/create是否可恢复仍由durable owner判断，SDK不从调用者给的确认集合推断线上状态。
新增Manager resumeAdmissionRecorder独立核验：当前活跃attempt/前次ID、source journal序号/摘要、无Pending/无create准入、当前账号/app及精确NeverAdmitted集合；桥接事件单独保存新scope/UTC，不重写旧凭证或观察时间，写失败不承认/重复拒绝。该callback和SDK恢复尚未接worker调度；当前运行行为仍是原流程，不宣称跨重启续传已可用。
验证：SDK入口首次因共用分片函数缺失编译失败，再实现；缺session observer用例先观察仍发upload/create错误，再补强制准入。成功/已确认片跳过/意图写失败/同尺寸改文件/换账号/缺observer/拒绝session无fallback覆盖，SDK组3次2.862s；API全包1.098s、uploads前轮14.004s；Manager桥接正常/待确认片/已准入create/序号变化/账号变化/写失败组3次1.849s通过。主程序28.787s、Downloader原上传接线相关组5.909s通过（桥接模块加入前）。最终API/uploads及vet另登记。无真实账号、启停服务或发布。
下一步：worker选择源journal并传递恢复上下文、Downloader维持当前授权epoch/维护门禁后调用SDK、桥接后初始化新attempt分片记账与跨恢复链读取、暂停/重启/最终回执核对及网页恢复详情；未接通前不出上传恢复测试包。
最终验证：桥接模块加入后API全包缓存通过、uploads全包12.775s通过，API/uploads/downloader vet与diff检查通过。

2026-11-05 队列/Downloader恢复接通：worker在原子领取后解析历史来源；有完整可恢复journal时携带source上下文及桥接/分片观察器，空哈希尝试可沿前次ID回溯，有预创建意图但无完整session、Pending或create准入均隔离unknown，保留已有sent。SDK桥接落库后新attempt观察器从桥接继承旧计划/已确认片，不伪造新precreate；journal递归验证来源、摘要/序号/作用域/剩余集合，限制因果顺序并拒绝循环，恢复后继续追加到当前attempt，可再次恢复。
Downloader在现有账号epoch、路径/队列维护和最终metadata核对路径调用SDK恢复；请求使用当前token，历史scope保持原值。发现原新上传父目录创建路径会在恢复校验前重建已删除目标，回归先复现1次POST，再修复为恢复父目录缺失直接unknown且零写。新上传的原父目录创建行为保持。
验证：worker真实SDK mock跨SQLite关闭/重开，4MiB+3字节文件第0片历史确认后只发第1片+create（无precreate）；合并前中断再试只create、哈希准入前中断后追溯旧session、Pending零HTTP隔离均通过。恢复链读回完整2片和create回执。Downloader当前凭证/稳定scope/四检查点及metadata实际接线通过。API全包缓存、uploads全包24.512s，最终文案后重跑21.351s；跨恢复/桥接组3次20.933s；主程序26.377s及最终缓存通过；Downloader上传/授权组6.981s，父目录修复后相关组5.532s；vet/diff通过。
前端暂停/排队文案改为恢复前核对会话、未知隔离，进度明确含可信恢复确认；原文案断言先失败后按新合同更新，upload-progress 4项通过，全前端最终结果另登记。未打包、未进行可见浏览器或真实百度验收，未触碰真实账号/服务。剩余恢复状态和证据的专门网页展示、未知回执身份只读核对、更多故障/暂停链路、完整发布门禁及同版交付，不能将本机mock当provider session TTL保证。
最终前端复核：393/393通过，13.923s；没有进行浏览器可见验收或打包。

2026-11-05 网页恢复详情：新增只读GET /api/uploads/{batch}/items/{item}/session，通过现有管理鉴权；核对batch/item，拒绝POST/未知query，不返回uploadid、hash清单、token/app/账号摘要。输出最近/前次/桥接来源尝试、要求片/确认/缺回执/未准入计数、字符串序号、证据UTC及读取UTC；回执身份仅为历史观察。活跃标志来自读取时本机映射，不冒充持续实时状态。
文件行增加有边框的详情按钮，按需展开/刷新两张表；批次/文件切换、关闭、401、控制禁用及pagehide/BFcache迟到结果隔离。响应身份/UTC/精确序号/计数守恒校验，异常保留上次结果，不自动重试。区分请求仍可能执行与无活跃执行的缺回执。重启历史说明指向文件详情，不把重新排队称已恢复。
相关修复：已存在plan但关联precreate丢失，原readSessionJournal传递sql.ErrNoRows可能被当无会话。增强回归先失败，再将缺预期precreate/桥接源转换为证据不一致，阻止fresh fallback；真正无journal仍NoRows。
验证：详情接口缺实现先编译失败后实现；归属/只读/方法/参数、六类状态和私有字段不泄露组3次1.862s；上传UI按需/精确序号/跨文件/计数矛盾/关闭与BFcache迟到401等31项通过。最终Vue396/396、vue-tsc/Vite通过；uploads全包19.242s、主程序24.833s（含新route鉴权）、相关vet/diff通过。
可见验收：127.0.0.1:18091静态构建+纯虚构API，未加载账号；桌面与390px窄屏展开/刷新、表格及序号可见，窄屏document宽375/client375，宽表格容器内滚动。截图releases/v0.8.31-upload-session-desktop.png和mobile.png。tab关闭、viewport重置；退出helper后发现Python子进程残留，已核实命令行并结束准确PID68764，18091无监听；临时脚本删除。未发布、无真实账号或服务操作。剩余未知回执线上核对、故障恢复策略、整版门禁和同版交付；mock不等于真实provider/NAS验收。

2026-11-05 有回执的未知项线上核对后端：SDK VerifyUploadReceipt要求已保存canonical回执和稳定原账号/app作用域，当前授权ctx匹配client，只GET一次filemetas/fsid，核对实际path/size/type/ID，不按名搜索/换对象/新建/上传。WithContext沿用现有取消/pacer。记录当前scope、provider文件mtime及本机观察UTC，mtime不是核对时间。UploadReceipt.Valid抽出供create和独立核对共同使用。
Downloader走startIntake生命周期及当前账号绑定/epoch，核对前后检查；旧token轮换与同账号区分，晚到新epoch拒绝，不改云端namespace。
Manager新增可配置Verifier及POST /api/uploads/{batch}/items/{item}/verify-receipt，经原管理鉴权。只接受unknown、无活跃worker且有普通create或rapid回执的项；保存请求ID/来源sequence/原scope/回执/开始UTC再读线上。成功时当前scope/原回执一致且ctx有效，确认状态与线上观察证据同事务提交，再回收自身暂存并解除批次unknown阻塞。缺回执/读失败/错账号/本地提交失败均不放行、不重发。记录失败状态但不持久化任意callback错误原文，避免秘密泄露。清理失败保留confirmed并交给既有启动回收。
生命周期：Close与核对加入wg在同一互斥下协调；核对绑定caller和Manager取消，关闭等待inflight结束后关SQLite，关闭期间不得确认。原创建回执是完成凭据前提；不为只有意图/丢回执的项提供人工强行放行。
验证：SDK接口缺失先编译失败再实现；成功/路径/大小/类型/ID不符、换账号/应用、缺scope、取消、坏回执（授权前零HTTP）3次2.835s；Downloader账号gate/晚到凭证epoch1.968s，最终相关组7.852s。Manager普通/秒传成功、读失败/换账号/缺回执/commit回滚/关闭不确认、HTTP方法/参数/归属组与Close在途等待、取消后跨重启仍unknown且失败证据存在组3次2.481s。API全包1.131s、uploads全包24.933s、主程序25.765s（新路由鉴权）和vet/diff通过。
尚未提供网页核对按钮及核对证据摘要，未做该动作可见验收或真实百度测试，未发布/启停真实服务/触碰账号数据。下一步接网页明确动作与结果、刷新及迟到回执保护，再走整版门禁。此后端可在完整创建回执与新线上身份一致时确认本机任务，绝非无凭据人工放行。

2026-11-05 网页回执核对动作与持久结果：只在unknown、无活跃上传且有create/rapid回执时显示“核对线上回执并确认完成”，说明只读原fsid、通过后本机完成、无其他unknown时批次可继续。不允许缺回执强行放行。成功验证精确batch/item/state回执后刷新详情/批次/统计；失败、网络中断、错身份/矛盾结果保持未确认且需刷新，不自动重发。关闭/切对象/禁用/BFcache使请求失效；已确认完成的回执不会被矛盾刷新降为待核对。补齐仍在进行/历史观察的文案。
详情新增持久核对摘要：none/started/failed/verified，分别记录本机记账UTC与网盘响应的本机观察UTC，重启后可读。摘要核对事件形状、请求/attempt、来源/回执/作用域，verified不可由残缺证据宣称。普通/秒传已确认结果跨重启保持confirmed及同一观察时间。
验证：网页按钮缺失用例先失败再实现；资格过滤、单次POST及成功刷新、精确大序号、无回执隐藏、错身份/网络/409、关闭/BFcache迟到401、完成结果和持久UTC显示通过。上传UI35项；最终Vue400/400（13.186s）及vue-tsc/Vite通过；uploads全包19.625s、主程序25.163s、核对/摘要/关闭组3次2.777s与vet/diff通过。最后仅调整回执说明的文本后重建并再跑400项。
可见验收：localhost纯虚构API，桌面手动展开→核对→完成反馈→刷新读回核对摘要/UTC及批次完成通过，截图releases/v0.8.31-upload-receipt-desktop.png。窄屏DOM宽度375/375无整页横溢；截图出现浏览器缩放渲染异常，未将该图当可见窄屏通过，已删除该图。tab关闭、viewport重置，核实绝对脚本路径后结束mock PID、18091无监听、临时脚本删除。无真实账号/服务操作。
整批fresh-context只读审查未发现确证阻断问题；结论限源码，不替代provider/NAS。当前进入全Go/发布门禁，尚未覆盖运行程序，最终交付及数据指纹写外部validation。
