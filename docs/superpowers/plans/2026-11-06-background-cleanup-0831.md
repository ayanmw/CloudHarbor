# v0.8.31 后台清理批次：设计与实施计划

## 已核实缺口

CleanupBatch.vue的submit在浏览器for循环逐项POST /api/cloud/mutations；stopForLifecycle在pagehide停止后续，不能跨清理页继续。CloudCleanupWorkspace的reconcile只刷新重复/空目录候选。扫描/哈希是后端worker，不能把它们与前端清理循环混称后台任务。当前安装06:55:36尚无后台清理队列；本计划不是已实现声明。

## 目标与边界

用户核对固定清单后，后端逐项清理，页面离开不停止。一个worker，复用MutateCloud的维护协调、线上来源/保留副本/内容核对、隔离与缓存更新。每项成功更新本地缓存，重新进入文件页从线上读取；页面活跃时按观察结果刷新，不用历史成功推断线上当前完整。可收起并从Header/清理历史返回；停止只挡后续项，不把已发请求说成撤销。

一次最多1000项，超限显式拒绝，不截断、不把本页选择叫全盘选择。只支持已有duplicate/empty两类，不增加其他删除策略。固定manifest包含精确ID、路径/名字、类型、大小、远端mtime/MD5；重复项包含保留对象与预期SHA256。保留对象不能同时在待删集合中；同ID或同路径重复待删、相互冲突的保留身份/内容预期拒绝。预期hash不是授权，发送前仍线上核对。账号、应用和缓存namespace由后端已验证会话取得，不信客户端声明。

## 证据与幂等

批次ID32小写hex；固定清单规范编码并保存SHA256。客户端预览观察UTC单独标来源，不当成百度响应时刻；CreatedAtUTC为本机接收记录，放SQL列，不纳入请求内容digest，使同ID相同清单重试不因接收时间变化失败。相同ID/digest只读回原记录，不重复创建/执行；相同ID不同digest拒绝。记录首次时刻，不用后来事件改写过去。

SQLite任务、逐项投影、追加事件与remote_operations关联持久化。入队queued，发送前将remote prepared intent与item.operation_id同事务提交；必须显式检查stop_requested及owner。禁止先发请求再补关联。结果明确confirmed保留回执；failed/unknown停止后续。有confirmed但本地协调失败时保留确认状态与警告，仍停批次。重启先按现有journal恢复prepared→unknown，再恢复批次：缺可信结果保持unknown隔离，不根据目标不存在推定删除成功。未发送项可在合法账号/应用与明确恢复规则下继续，未知项没有强行放行按钮。

关闭进程需停止准入并等待当前项收尾；进程中断后重建事实，不能把历史running显示为当前活worker。旧浏览器清理历史只有逐项remote日志，不虚构历史批次或清单。实际schema现场为42，后续存储阶段需43迁移与manifest提交恢复验证；本轮契约阶段不改变schema。

## 分阶段执行与门禁

1. taskstore/cleanup_manifest.go与测试：精确字符串编码、固定清单/hash、不可变输入、合法/非法组合和保留冲突；无远端或真实DB。
2. schema43及存储：批次/项目/事件、首次接收时间、幂等create/read/stop；迁移、写入失败回滚、重启与证据隔离。旧数据不回填批次。
3. prepared关联+worker：单准入、账号/应用/epoch/维护锁、回执原子关联、重启unknown、停止/关闭/迟到结果，mock provider验证逐项成功后更新缓存。
4. 鉴权/同源HTTP：创建/读取/列表/停止，精确ID、请求上限与失败结果，不开放无凭据重发。
5. Vue清单确认→后台批次详情/跨页进度、Header入口、停止说明；保留分页/错误/空态/过时/401隔离；移除浏览器发送循环，仅在新全链路可用后切换。
6. 全量/独立复审/产物/数据核对后同版测试包。真实账号删除不在当前只读验证授权内，不操作真实网盘；真实E2E另有隔离数据及授权边界，mock不替代。

任何阶段基础通过不称后台清理已完成。当前已发布v0.8.31保持可用，代码完整前不更新运行包。

## 前置隔离复核进展（2026-11-06）

已用模拟服务复现并修正两处缺口：启动恢复不再以当前token过滤历史unknown；MutateCloud对所有变更统一核对源与目标隔离，并阻止操作包含隔离子项的祖先目录。旧代码在模拟测试实际发送了rename/move/delete及父目录copy；修正后均在provider调用前拒绝。字符串前缀相似但无父子关系的目录不误阻止。

凭据重启、复制、未知变更及保留副本专项通过（30.283s），downloader vet通过；补充四类操作的无关隔离对照后专项通过（13.996s）。均为临时库/mock，不是全仓回归或真实百度验收；尚未打包。后续继续schema43与批次存储，前置修复不等于后台清理完成。

## 批次存储基础进展（尚未接执行器）

已新增schema43及42→43迁移，固定清单/首次接收UTC/digest与账号、应用、cache作用域独立保存，逐项pending投影及append-only事件同事务创建。相同ID/内容读回原批次，不同内容冲突；停止queued批次持久化为stopped，重复停止不重复事件，running停止只请求挡后续。精确纳秒接收UTC使用RFC3339Nano保存，与客户端预览UTC分开。

专项覆盖重开、幂等、清单冲突、注入项目插入失败后的三表回滚、不可改写清单/首次时间、不可修改/删除事件、无效operation外键；迁移保留旧任务且不补造旧批次，数据库已提交而manifest仍旧的恢复亦通过。最新专项8.123s，taskstore vet通过；全量回归另行登记。真实SQLite未打开或迁移。

当前仅有create/read/stop存储入口；列表分页、项目证据读取、prepared与项目关联、worker及HTTP/Vue尚未实现。不可将pending持久化视作已具备后台清理能力，也不提前覆盖安装包。

## 发送前原子关联进展

新增PrepareCleanupOperation存储准入：核对后端账号/应用/cache作用域、stop、固定清单digest及项目对象/路径，要求前序全部confirmed且当前无sending项。remote prepared intent及其事件、项目operation_id/sending和批次running/事件在同一个SQLite事务提交；重复项目拒绝，不把重试当成发送许可。CreateRemoteOperation复用同一事务插入函数，保持原有入口行为。

专项9.417s及vet通过，覆盖成功、停止、错误owner、对象变化、注入项目投影失败导致远端意图回滚和重复准入。当前还未接MutateCloud/worker，未实现sending结果投影及重开恢复；接口返回成功只代表发送意图已落库，不代表已经发往百度或删除完成。全量taskstore原进程继续运行，不用专项替代全量结论。

原taskstore全量回归已正常结束，通过281.462s；该二进制覆盖schema43/批次创建停止基础，编译早于本轮prepared关联改动。新增prepared关联另有9.417s专项及vet，抽取的原remote-operation入口13.091s回归通过。下轮完整门禁仍需使用最终源码重跑，不把旧全量范围扩称最终全量。

## 结果投影与重启恢复进展

新增ReconcileCleanupOperation读取持久化remote结果，在同事务更新项目、批次与带ordinal/operation_id/远端响应观察时间/原因的追加事件；不是由网页回调声明成功。prepared仍不允许投影为完成；Open先执行既有remote恢复，再恢复sending项目，unknown不重发。confirmed缺响应时间拒绝；confirmed缺本地协调完成凭据保留项目confirmed但批次failed，不继续；完整凭据才允许下一项或完成。停止在途后保留实际结果并挡后续；结果事件插入失败回滚项目及批次，可安全重做本地投影而不重做远端写入。

专项9.019s及追加停止/事件失败专项6.956s通过；重复投影不重复事件，五种重开结果均禁止重发。schema43尚未发布，事件结构在同一次未发布迁移内补充，不碰真实库。仍缺执行器、worker前置失败记录、API/页面和最终发布门禁，不能称后台清理已交付。

## 清单读取与发送前失败

CleanupManifest按后台已验证scope读取，核对持久化owner、批次ID、项目数、完整规范编码和SHA256，返回首次接收UTC；PrepareCleanupOperation复用该验证，避免显示与执行的完整性边界不同。FailCleanupPreflight只接受固定脱敏原因码，pending/无发送关联/前序已确认/批次可执行时才记录；项目failed、批次failed与事件同事务，不补造operation_id或远端响应时间。重复相同拒绝幂等；sending不能被改写为未发送失败。

首轮关联/恢复/读取专项17.147s与vet通过；追加事件失败回滚、sending拒绝、错误owner和潜在敏感原始错误理由拒绝测试；最终Cleanup专项另记。任务执行器及分页详情/HTTP/Vue仍待接入，无真实操作或安装替换。

## 单项执行接线进展

executeCleanupItem从已绑定账号/应用/cache作用域读取固定manifest，转换empty/duplicate到现有MutateCloud；私有context关联在最终线上核对之后、provider调用之前提交PrepareCleanupOperation，并在epoch读锁下校验准入。结果从remote journal投影，取消的调用仍以独立有限时上下文保存实际结果。未发送的取消/授权等待/维护冲突保持待处理，其他前置失败用固定原因码记录；ErrIntakeStale可能来自身份或授权变化，使用observation_changed，不能一律声称源文件变化。

模拟执行专项11.027s：空目录成功/身份变化/未知/预先停止/核对期间停止、provider收到请求前prepared与running已经落库、重复文件成功及保留身份变化拒绝；重复执行不再发送。原保留副本内容/身份及隔离回归31.444s，相关vet通过。无真实远端写入。

先前原taskstore全量进程正常结束307.767s，证据在发布目录v0.8.31-cleanup-storage-tests.jsonl；覆盖编译时的schema/关联/结果恢复，不包含此后清单读取与preflight新接口，不能当最终全仓门禁。尚缺后台调度循环、列表/详情存储、API、Vue/Header接线及完整发布。

## 后台循环与批次列表进展

新增作用域隔离的CleanupBatchPage（上限50）和五状态项目计数，以及NextCleanupItem持久选择；按SQLite落库顺序调度，系统时钟回拨不重排队列。只选择queued、未stop、前序confirmed、无sending的项目；选择不等同准入，仍由Prepare同事务核对。分页/选择/停止/作用域专项6.107s及vet通过。

CreateCloudCleanupBatch只接收ID/清单，由后端填scope及首次UTC，成功后启动唯一后台循环。循环串行复用单项执行，等待授权/维护时两秒检查，成功不额外降频；unknown/失败批次不再选中。Shutdown取消后等cleanup WG退出再关store，启动与关闭通过mutex协调，禁止Wait期间新增worker。

worker/执行专项14.936s与相关vet通过，两项成功仅两次写、unknown仅一次且同批第二项不发、重复start不多开、显式Shutdown无关库竞态。均为临时库/mock。重启后自动启动入口尚未接；不能据此宣称安装已具备后台批次。仍需项目/事件详情分页、HTTP、Vue/Header和发布门禁。

## 主程序恢复启动和详情分页

主程序在凭据/上传隔离初始化且管理服务监听成功后调用ResumeCloudCleanup；worker每次检查当前授权，Store.Open不发远端请求。后台生命周期现复用既有StartBackground/q.wg，撤去本功能初稿的独立WG及Shutdown等待分支，沿用统一关闭准入与收尾。临时库重开后未授权不发送，授权就绪才执行的专项7.806s通过；main编译/BuildInfo专项2.108s通过，原worker/单项执行15.007s通过。

新增CleanupItemPage和CleanupEventPage：作用域与manifest完整性核对、每页上限100，项目保留固定身份/预览时间和当前状态/operation_id；事件按精确字符串sequence游标读取，分别展示本机记录UTC与可空远端响应UTC，不补造停止事件的云端时刻。详情/游标/选择/恢复专项5.965s与相关vet通过。

尚未接HTTP和Vue/Header；实际可见体验和最终门禁仍未完成，无真实账号写操作、无安装覆盖。

## HTTP与作用域查询接线

/api/cloud/cleanup-batches支持GET列表/POST创建；/{id}、/items、/events只读，/stop仅POST。沿用applicationHandler的management/same-origin封装；所有响应no-store。创建最多1MiB、1000项、禁止未知字段与客户端owner；数字身份必须字符串，入后端前验证固定清单。后端wrapper统一从当前已验证scope读取/停止，前端不指定作用域。列表50/详情100上限，事件游标与返回序号为精确字符串。失败不返回原始错误；提交失败提示按相同ID查询/重试，禁止换ID盲重发。stop确认请求已处理，不对已完成批次虚报stop_requested。

intakeapi全量2.298s、HTTP专项2.047s、主路由鉴权/跨域专项2.322s、相关vet及diff检查通过。覆盖精确大ID、伪造scope拒绝、非法清单/尾随JSON拒绝、精确事件cursor、秘密错误脱敏、各路由401及写入跨域403。均为mock/进程内路由，无真实网盘服务。仍需网页提交与详情/Header任务接线及最终发布。

## 网页切换进展

CleanupBatch已移除逐项POST mutations循环，确认后冻结纯身份清单，使用getRandomValues生成32hex ID（不依赖仅安全上下文可用的randomUUID），一次POST后台批次。ID同步进当前URL；提交响应缺失先按ID读取，不新建ID；需要重试时用户显式提交相同冻结body。历史/批次读取/事件隔离迟到响应，pagehide只停止观察不停止后端，不虚构离页远端unknown回执；BFCache恢复只读。未知记录保留隔离，停止请求发给后端，并读取真实状态。400明确拒绝可重新预览，401清除观察并要求登录。

批次显示全量状态计数、25项分页、精确事件cursor与下一批、远端操作链接、读取UTC/旧快照和最近25批次；不把本页计数当总数。状态确认增加后刷新候选，返回文件页仍重新读线上。后台record补全五状态计数，与列表共用字段，存储专项6.490s通过。

前端原单项循环测试按新合同改写，保留重复/空目录/授权/迟到响应覆盖；19专项通过，含一次固定清单、响应丢失、只读恢复、后端stop、相同body重试及刷新失败旧观察。571全前端通过后又补历史加载态/迟到响应等局部修正，最终类型/Vite及19专项通过，最终全量仍须随Header整合重跑。早期fixture缺证据导致失败并遗留测试进程，已确认命令行后仅停止该自建Node进程；修正fixture后正常无force-exit通过。

Header入口/独立复核/完整门禁及打包仍缺，无实际视觉/真实百度验收，不更新安装目录。

## Header与独立复核

Header按需增加第六来源：最近清理批次/总批数、精确批次URL和服务端读取UTC。明确只展示最近一批、不证明worker存活，也不把读取UTC当事件时刻。空/错误/旧观察保留既有语义，只发GET，不自动刷新或操作。27 Header/清理专项、573全前端、全仓vet及legacy测试通过；实际视觉仍未验收。

独立静态审查发现并红测复现：非空目录和隔离拒绝误入可重试冲突，导致queued饥饿；retained未检查隔离，存在未决删除随后完成导致两份都丢失风险。修正专用拒绝分类，记录directory_not_empty/operation_conflict预检失败；来源/目标/保留副本统一检查未知操作。进一步覆盖历史下载后处理的源ID/路径/改名目标，以及intake未决来源ID/路径/目标。为避免改变原候选调度语义，新增仅供MutateCloud的isCloudMutationBlocked，intake来源投影在q.mu下与目标同步替换；原eligibility保持目标语义。

临时服务红测先证明上述四类历史证据可被错误当保留副本并实际发delete；修复后执行/worker/隔离49.156s通过。四类真实临时库重开、原ID改路径、原/目标路径与相似无关路径恢复专项及保留副本专项16.484s通过。独立复审静态确认此前Important均收敛，没有新必须修改项；审查者未执行测试。

完整Go -p1全量于08:04启动后保持原进程，当前已进入taskstore测试，尚未结束；部分修复晚于该轮编译，不能以该轮结果替代最终源码门禁。无真实账号操作或安装覆盖。

## 发布前缓存警告补强

故障注入确认：增量缓存删除失败、invalidate成功时，原逻辑仍写local_reconciliation_complete导致清理batch completed。现仅cleanup私有context且Warning非空时扣留该继续凭据，confirmed回执保持；即使warning事件无法保存，重启也无继续依据。普通网盘变更语义不变。红测后修复专项9.788s与vet通过，独立静态复审确认已报问题全部收敛。

同版替换事务fixture检查通过（独立临时根并自动清理）；当前运行exe不存在，47真实数据文件建立只读哈希基线，两SQLite quick_check ok，未复制账户或迁移真实库。两轮Go全量仍为原活进程，最终产物待门禁后生成。

## 最终同版交付（2026-11-06 08:37:21北京时间）

六阶段源码接线及本机发布门禁已完成；上文“待接/未发布”为过程记录。573前端/173旧页/3脚本、13个Go测试包及vet、三平台构建通过。最终downloader422.252s、taskstore282.732s，其他cached如日志记录。独立产物14项/13载荷、999源码快照、实际编译身份/格式/CGO核对通过；47数据文件不变、两SQLite quick_check ok，schema真实库尚未启动迁移。新staging清理，未自动启动服务。真实视觉/百度写入/恢复/NAS验收保留，不称整版或长期目标全部完成。版本保持v0.8.31，不创建新标签。

## 交付后综合HTTP验收补强

cloud_cleanup_lifecycle_test.go新增真实loopback HTTP/gate+真实Downloader/SQLite+严格模拟provider，confirmed/unknown/stop-inflight三场景及终态重开/相同ID幂等通过。intakeapi全量16.718s、vet、独立静态复核通过；仅TestGoFiles，不更新运行包。明确不覆盖真实浏览器、provider写入或sending中强制崩溃窗口；细节见acceptance-audit当前补充节。

## 强制中断窗口补强

cloud_cleanup_crash_test.go实际helper Kill而非Close/Shutdown：prepared已提交未发送、服务端已接收未响应两窗口，分别0/1次接收，重开unknown/第二项pending/原关联及nil响应时间、凭据替换后隔离及无重发通过。组合专项12.253s、vet和只读静态复核通过，子进程无遗留。只增TestGoFiles，不改已交付程序，不把进程Kill当作断电/NAS/真实云端验收。
