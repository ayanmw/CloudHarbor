# .160 下载路径与迁移工作台实施计划

> 使用superpowers:executing-plans，原目录内联执行；先行为回归失败再实现。无自动Git提交/推送或worktree。

Goal：提供独立的路径设置和当前迁移工作台，沿用现有可靠持久化及迁移边界。
Architecture：PathSettingsWorkspace、PathChangeImpact、LocalMigrationStatus按现有接口分工；path-settings-types只处理实际DTO。共享侧栏和Go受保护路由复用，不新建配置/文件系统框架。
Tech Stack：现有Vue3/TypeScript/Vite、Go、SQLite，不增依赖。
Spec：docs/superpowers/specs/2026-11-03-path-workspace-160.zh-CN.md。

## 全局约束和审查重点

- 当前线上监控路径、当前本地路径、草稿目标和历史迁移分开；未知结果不能凭相同路径冒认本次成功。
- 保存需固定影响digest，输入/设置/任务变化使旧确认失效；任务运行锁和.env只读以服务端最终判定。
- 当前迁移ID绑定后才导出证据；精确身份/计数不转Number再冒充原值。
- 单在途/离页/401/迟到隔离，失败显示历史且不沿用旧可执行权限；不自动重发写操作。
- 不在真实目录上迁移或删除验收；模拟/临时目录不代表真实NAS/SMB。

## 任务

- [x] Task1：阅读main.handleManagedPaths/handleLocalDirectoryMigration、downloader/managed_paths.go、local_migration.go及旧cloud全部路径控件，建立能力/DTO/阶段表。新ui-path-settings.test.mjs先失败验证独立入口、路径分列、env锁定、空/失败；再接/settings/paths、Sidebar/main.ts/webui/main.go并保留权限回归。
- [x] Task2：先覆盖影响预览目标/任务/策略、50项分页、digest、输入改变与迟到响应、身份精度，再接PathChangeImpact。需要字符串兼容DTO时先Go HTTP大ID回归失败后实现；不改旧默认格式。
- [x] Task3：先覆盖当前路径与草稿分离、运行锁/环境锁、迁移选项、固定确认、重复提交、200/202/409/断网未知不重发，再接保存流程。核实已有可用运行信号，必要时最小只读字段由后端同锁计算，最终保存锁保留。
- [x] Task4：先覆盖none/running/failed/completed及实际phase、计数精度、UTC证据、读取失败/旧轮迟到/跨重启历史区别，再接LocalMigrationStatus和单在途轮询。不添加新迁移算法或磁盘操作。
- [x] Task5：先覆盖未知响应恢复、绑定ID证据导出/错误/迟到/401、历史明细入口，再完成旧能力对照。一次最终独立审查、桌面/390px/键盘模拟、Go全包/vet/前端/旧页/脚本/三平台门禁及ZIP快照校验，才打包.160。

Ledger：.159五项完成并冻结，最终ZIP哈希见releases同级.159-validation.md。当前仅建立.160规划，未改该批产品代码/创建测试包或访问真实目录。下一步Task1能力对照和失败测试。

## 能力对照与实施记录

旧cloud路径控件/后端已核实：GET settings/paths含queue_dir/local_download_dir与两个环境editable标志，不含可靠的运行锁。GET带queue_dir/offset/limit=50提供影响任务、has_more及digest；POST带路径、migrate_files及queue_change_preview_digest。200保存路径，202返回migration+paths；409可能携带最新impact。后端还检查未完成intake、维护锁、目录扫描与全盘缓存扫描；digest只证明未执行任务快照，不是远端身份或移动凭据。

当前迁移GET返回none或running/failed/completed及具体phase；latest与历史事件/整笔导出是独立接口。原cloud当前迁移轮询/迁移历史及导出控件保留；本版不删除旧功能。运行锁字段在Task3按实际锁顺序实现，不用环境editable或空分页冒充空闲。

Task1：先观察6条路径Vue/展示/锁定/失败/迟到/401回归全失败及Go入口404，再接PathSettingsWorkspace.vue、path-settings-types.ts、/settings/paths共享嵌入路由。云端与本地值原样分列，环境锁只读明确，读回UTC与当前配置/历史证据区分，空/坏响应不推断workDir默认。pagehide/BFCache及认证失效隔离旧请求。仅GET，不包含设置写入。

共享Sidebar补直达入口后发现首页App有独立侧栏，先加首页导航回归复现缺失，再补App路径与存储链接。主HTTP回归增加匿名303与登录Cookie后200/no-store/嵌入资源检查，未知路由仍404。最终TypeScript/构建、前端155/155（路径6/6及首页导航回归）、Go资源1.356s、管理/全局历史/AppConfig5.721s、diff通过。

当前1/5，后续影响预览/保存确认/当前迁移及证据导出未完成，不发布开发页。.159冻结。无服务启动、真实目录/凭据操作、Git提交或worktree。下一步Task2精确影响DTO、50项分页及digest上下文回归。

Task2精度：先复现identity_format=string仍输出数字，再新增GET可选字符串DTO，保留旧默认数字与原digest，非法/重复格式400。HTTP临时存储回归覆盖9007199254740993及MaxInt64、旧格式和digest一致性；不改POST/409默认结构或SQLite。只转换已分页<=50项，不改变任务快照算法。

Task2界面：7条影响预览回归先失败，再接PathChangeImpact.vue、规范非根云路径验证与草稿目标输入。明确源/目标、未执行根任务、精确字符串ID/状态/次数/固定策略、每页50项、读取UTC及digest。分页面需同digest/count和有序ID边界，任务变化/分页失败保留旧行但使确认失效；输入/配置/离页变化取消并隔离迟到。ready事件会在失效/请求开始时清空，实际保存由Task3消费。预览仅GET，不自动修改设置/移动文件。

追加认证回归先复现影响401没有登录跳转，再统一requireLogin，旧预览清除与重新登录一致。最终TypeScript/构建、前端163/163（路径14/14）、Go资源1.229s、管理/全局历史/AppConfig/ManagedPaths及大ID回归6.931s、go vet .、diff通过。

当前2/5，未打包。.159冻结，无真实目录/凭据/服务启动或Git提交。Task3待核实：影响digest仅绑定当前/目标云目录和未执行任务，不覆盖本地路径；保存/迁移确认需防止其他管理员修改当前本地目录后误迁移新的来源，采用与最终路径锁一致的设置前置校验，不能只靠页面旧值或查询到执行间隙。

Task3后端前置保护：先观察HTTP expected_paths被拒为未知字段及运行锁缺失回归失败，再为SetManagedPathsConfirmed新增可选路径快照，旧调用不传保持兼容。HTTP expected_paths传入当前queue_dir/local_download_dir；在pathOperations写锁内重新读取后比较，过时本地来源在创建目标前409拒绝。增加入锁后路径变更回归，确保不是HTTP层检查。

GET新增queue_busy/queue_busy_reason，与TryBeginMaintenance复用同queue.mu判断active/maintenance/unavailable；查询不取得维护状态。页面观察busy时禁用草稿和影响预览，坏/缺失运行元数据不能推断空闲；影响GET读到新busy也不生成可用确认。相关回归先失败后通过。

发现并复现旧迁移发布→释放maintenance的交接窗口：它不持有pathOperations，可能发生在初始路径读取之后。增加精确阶段测试钩子与回归先观察旧快照被接受，再维护准入后重新核对路径，变化返回ErrManagedPathsChanged且释放维护锁，不创建目标。说明注释保留于代码，旧有扫描/入队/状态目录约束不改。

验证分层：共享维护判断改动后下载器全包264.359s、主程序全包26.649s与vet通过；随后交接窗口补丁由路径/迁移定向19.328s、主HTTP/配置/管理定向10.359s、vet覆盖。最终前端165/165（路径16/16）、TypeScript/构建、Go嵌入资源1.187s、diff通过。最终发布时仍须对最终源码执行完整门禁，不把前一源码全包结果冒充最终包验收。

Task3暂不勾选，当前完整交付仍2/5；已补后端前置和运行锁展示，剩余本地目标、迁移选项、固定确认、单次保存/200/202/409/未知结果及恢复。无真实目录/账号/凭据操作或服务启动、Git提交；所有写入回归仅临时合成目录。.159冻结，不发布当前开发页。

Task3前端推进：新增本地下载目标与迁移选择，固定来源/目标/迁移标志/digest的确认对话框，勾选核对后单次POST，并附expected_paths。取消不写入；配置/草稿/影响变化撤销确认。环境锁、读取失败、运行忙碌与提交中禁用。200确认保存、202只表示受理且尚未完成、409要求刷新重新预览；POST断网或坏响应标记结果未知，不自动重发并暂时锁住再次保存。成功或明确拒绝后GET权威配置，不把POST简略路径DTO当成完整可编辑元数据。

回归：本地输入缺失两条测试先RED，再实现确认/未知结果GREEN；补200/202/409三种结果及读回验证。TypeScript/构建、前端170/170（路径21/21）、Go嵌入资源1.166s与diff通过。仍是本地模拟，未启动服务或操作真实目录。当前迁移状态组件和未知结果核对恢复尚未接入，Task3保留未勾选，避免把局部保存界面当作完整交付。下一步Task4当前迁移展示，然后补Task3/5完整恢复与单次提交并发回归。

用户确认扫描策略：默认扫描至完成，遇到明确限制响应或确定可复现规律才降频；本工具自设请求预算不得表述为百度官方额度。已有默认扫描至完成回归本轮通过，不擅自改变既有用户显式有限预算。

Task4推进：先以两条回归观察迁移组件缺失RED，再实现LocalMigrationStatus。只读GET最近持久化记录，明确它可能来自重启前且不证明本页提交。展示来源/目标、实际六阶段、文件/字节表、开始/更新/结束UTC（不展示Go零时间）、ID与失败原因。running成功读取后2秒单在途轮询，失败停止自动轮询并保留旧记录标记；pagehide取消和generation隔离，BFCache恢复重新读。保存结束触发迁移刷新，已观察记录状态/ID变化触发当前配置重读。

补充回归覆盖超出JS安全整数拒绝展示（不把舍入数当证据）、离页迟到不覆盖恢复记录；最终前端174/174（路径25/25）、TypeScript/构建、Go嵌入资源1.259s、diff通过。迁移计数旧API为int64数字；目前超范围明确报错，精确字符串兼容DTO和导出绑定留Task5完善。现阶段无真实目录迁移/账号操作/服务运行。Task4暂不勾选：仍需none/全部阶段/认证与提交期间刷新竞态完整回归，以及最终UI验证；Task3/5未知恢复也未完成，继续保持同一.160批次、不发布局部包。

Task3/5推进：先复现迁移状态在POST未返回前变为completed，触发配置GET增加generation导致成功回执被丢弃（RED），再用migrationChanged在saving期间不发配置刷新；POST结束本身读回配置。回归GREEN，保留原因注释。这是当前功能接线竞态修复，未改后端迁移逻辑。

Task5导出：先复现缺少导出入口，再按当前显示的精确迁移ID请求/by-ID/export，不使用易变latest导出接口。单次在途，导出失败单独展示不污染状态读错；401停读跳登录；离页/ID变化使旧导出失效。下载保留原始JSON文本字节，解析仅用于核对migration.id，不将int64证据重新序列化成Number。补完整历史旧入口。构建、前端176/176（路径27/27）、Go资源1.209s、diff通过。仍未打包，无真实目录或账号操作。Task5尚缺未知结果可操作恢复、成功导出/迟到/401完整回归、精确计数兼容、最终UI与全门禁。

Task5精确计数：local-migration GET增加可选counter_format=string，仅覆盖四个计数的JSON类型，默认旧数字格式不变。非法/重复格式400在访问store前拒绝。Go先见转换helper不存在RED再实现，验证9007199254740993及MaxInt64字符串不舍入。Vue显式请求字符串，canonical非负十进制与MaxInt64边界校验，显示原字符串，不转换Number。前端补MaxInt64精确展示、旧数字拒绝仍通过。构建/TypeScript、前端177/177（路径28/28）、主路径/迁移HTTP定向2.646s、Go资源1.240s、diff通过。尚未完整Go门禁或真实服务验收；未打包。后续优先完成未知结果恢复与迁移轮询/导出边界，之后最终审查及可见UI。

Task5未知结果核对：先复现503被当成确定拒绝（RED），修正5xx/408为结果未知，保留提交目标与迁移选择，锁住重复提交。新增显式只读核对按钮，读回当前设置并刷新迁移状态；精确路径相同/不同只作为当前配置观察，均明确不证明这次请求完成或文件迁移完整，不自动放行/重发。提供完整历史入口。前端178/178、TypeScript/构建、Go资源1.267s、go vet .及diff通过。最终仍缺202回执ID的可见绑定、成功导出/迟到/401及轮询边界完整验证、可见UI/最终review/全Go与三平台打包门禁，不能据此宣布.160完成或发布。无真实账号/目录写入、服务启动、Git提交。

Task5回执绑定：先复现202后无可见回执ID（RED），再保留本页提交UTC、32位迁移回执ID；最新记录按精确ID提示一致/不是本次，读错显示旧证据。不用同路径冒认。202坏ID归未知。补导出成功原始JSON含MaxInt64逐字节保持及固定文件名，401停止后续读取并要求登录。最终前端181/181（路径32/32）、TypeScript/构建、Go资源1.259s、diff通过。导出成功为JSDOM Blob/FileReader模拟，不能冒充浏览器下载/NAS验收。未发包。下一步补轮询单在途、none与阶段覆盖及离页导出迟到，随后冻结功能进入整体review/UI与发布门禁。

Task4/5边界验证与整体审查：新增轮询单在途/离页停止、none及全部六阶段、离页导出迟到回归通过。一次fresh-context reviewer review_160复现P2：保存/核对refreshKey在旧GET在途时被loading guard吞掉，旧none/terminal返回后永不再查。主流程新增RED观察reads=1而期望2，再加合并refreshPending，finally结束后只补一次读取，stop清除。修复后前端185/185及构建通过。review未发现其余同等级明确问题。

可见UI：本机mock18091仅内存配置，桌面输入→确认核对/禁用按钮→勾选保存→200读回验证；390x844观察迁移计数列重叠，修复fixed表格和单元换行后截图复核通过。截图位于releases同级.160-confirm.png/.160-mobile.png。tab15已关闭，viewport恢复，fixture160.cjs已删除，自有6358进程终止且端口无监听。未操作真实账号/目录/凭据。全Go session82055仍活跃，样式曾在其运行期间重建，故最终包仍须完整重新门禁/资源检查；不声称最终发布验收。本轮功能停止扩展，后续整理门禁和打包。.160未发布。

发布门禁推进：release/TESTING.md已加入.160五项及验收边界；Task3/4按实际完成勾选，Task5保留最终打包校验待办。全仓go vet、旧WebUI172/172、userscript3/3、diff检查通过。go test -p1 ./...的session82055仍在运行，已确认go.exe63824及其downloader.test.exe8516存活且CPU持续推进；主程序27.105s、credentials18.715s已通过，尚无下载器终态，不能称全量通过，不重启测试也不并行启动另一轮打包测试。待当前终态后执行package.ps1正式冻结快照/三平台/ZIP验证。

最终门禁：package.ps1完成，前端185/185、legacy172/172、userscript3/3、完整Go/vet与三平台构建通过。独立ZIP/目录/源码630文件快照、13项ZIP/12项payload SHA及PE AMD64/ELF AMD64/ARM64架构校验通过，包无账号凭据/状态目录/临时夹具。Task1–5完成。按同版重新封包捕获本段和进度文档，最终哈希以releases同级.160-validation.md为准。未启动打包二进制，无真实百度/NAS验收。
