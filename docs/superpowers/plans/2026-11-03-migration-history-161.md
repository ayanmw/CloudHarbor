# .161 本地迁移历史实施计划

> 使用superpowers:executing-plans原目录内联实施；每步先行为测试再实现，一个最终fresh-context整体review。用户授权无需例行确认，不worktree/自动commit/push。

Goal：把本地迁移历史、详情、事件和导出做成清晰独立的只读工作台。
Architecture：MigrationWorkspace负责表格/筛选/URL，MigrationDetails负责所选ID详情/事件/导出；migration-route负责实际URL和DTO校验。复用共享侧栏及已有后端HTTP，不引入新依赖。
Tech Stack：Vue3、TypeScript、Vite、Go、现有SQLite。
Spec：docs/superpowers/specs/2026-11-03-migration-history-161.zh-CN.md。

## 全局约束

五项一起在同版交付；不重发迁移，不增加真实文件操作。当前磁盘、历史记录、本机读取UTC和事件UTC分开。精确int64身份与游标不经过Number。默认API兼容。pagehide/401/迟到隔离，运行中不重叠，筛选失败不复用旧分页。

## 任务

- [x] Task1：核对server.go历史/日期/分页DTO和旧cloud全部历史能力；新增ui-migrations.test.mjs先复现独立入口缺失，然后接MigrationWorkspace、/settings/migrations保护路由与路径页导航。表格及空/失败/只读测试；必要字符串计数DTO先Go回归。
- [x] Task2：新增migration-route.ts与筛选/URL测试，覆盖合法UTC范围、Unicode关键词、重复/非法查询及页码，已提交条件与草稿隔离，回退/迟到响应不串页。仅使用已有筛选能力。
- [x] Task3：新增MigrationDetails与固定ID/观察上下文测试，来源/目标/状态/错误/UTC分开展示；关闭/换记录/401/迟到阻断旧详情。
- [x] Task4：事件列表与精确sequence可选DTO先RED，再实现分页/原始证据显示，before游标合法且来自当前页；失败保留旧证据但不继续旧游标。
- [x] Task5：单记录/已提交筛选摘要导出，先验证ID/条件绑定、上限、原始JSON大数、失败/401/迟到，再接操作。完成一个fresh review、可见UI、全门禁/三平台/ZIP校验，才发布.161。

## Ledger

.160已通过最终校验并冻结，ZIP SHA256 377320BDE60B790688683472A9FF2893A85576C403316A86C950F49F70D6FD64。当前.161仅规划与后端能力调查，尚无产品代码改动或包。已查server.go列表默认20上限50、offset分页；事件精确sequence和摘要5000/完整100上限见taskstore常量。下一步Task1接口精确DTO与独立路由回归。

Task1部分：独立页面/空/失败两条前端回归先RED，Go路由404先RED；再接MigrationWorkspace、main.ts、受保护/settings/migrations与路径页导航。当前表格仅显示状态/实际阶段/原目标/开始更新UTC，不将旧数字计数当精确值。默认首20条明确标注，并非完整分页交付。匿名登录保护路由加入管理回归。期间批量测试编辑误扩展NewRequest参数已纠正，主管理4.126s通过。构建通过，前端最终结果见本轮终态；Task1保留未勾选，仍需精确DTO、坏/迟到/401等完整测试。没有创建.161包、启动服务或真实账号操作，.160 ZIP冻结不改。

Task1精确计数：Go HTTP回归先复现counter_format=string仍输出数字，再新增可选string四计数DTO，旧默认数字保持兼容，非法/重复格式400且backend调用数不增。覆盖9007199254740993和MaxInt64。Vue回归先复现缺少计数展示，再请求字符串并校验canonical非负int64，以原字符串单独列显示已处理/总文件数和字节数，不转Number。构建通过；intakeapi迁移定向1.517s，前端最终188/188；最终嵌入/vet/diff本轮通过。仅mock，无账号/目录请求。Task1仍待读取失败保留/迟到/401完整回归再勾选，随后Task2筛选URL与分页。.160 ZIP冻结，.161不打开发包。

Task1完成：失败保留旧证据、401停读、离页/BFCache迟到回归通过。Task2部分：筛选显式提交/刷新不夹带草稿回归先RED再实现；新增migration-route严格状态、日期范围、UTF8字节256、非代理残片、每页20的安全offset校验。URL恢复/重复非法拒绝、翻页保留已提交条件回归通过。前端194/194、构建/TypeScript、Go资源1.236s、diff通过。Task2未勾选：仍需popstate/失败翻页/坏元数据/越界回退等边界完整验证，详情事件导出未接。本版1/5完整，无真实账号或服务操作，.160包冻结。

Task2完成：越界恢复先RED再实现单次last-page重读并replaceState，持续变化报错不循环；缺next_offset先RED再对照总量/offset/条数校验，拒绝重复ID。popstate取消旧页、坏URL、失败翻页保留历史但禁用旧分页均通过。构建/TypeScript、全前端197/197后追加失败翻页的路径定向13/13、Go资源1.262s、diff通过。当前完整2/5，下一步Task3固定记录详情。未打包.161，不操作真实账号或目录。

Task3完成：详情入口缺失两条回归先RED，再实现MigrationDetails固定行副本/本机观察UTC，显示ID、来源目标、实际阶段、原始精确计数、开始更新结束UTC及错误。明确历史不代表磁盘重新核验；不为详情额外请求或执行写操作。复用vModal焦点/Escape，关闭/列表重读/离页使详情失效，不从BFCache恢复旧详情。无效结束时间/错误字段拒绝为可操作记录。构建/TypeScript、全前端202/202（迁移17/17）、Go资源1.220s、diff通过。当前完整3/5，下一步Task4按固定ID读取关键事件及精确游标。无服务/账号或真实目录操作，不打.161包。

Task4部分：事件string格式Go回归先RED（数字不能解到string）再新增可选identity_format=string，sequence/next精确字符串，脱敏后的evidence_json保留原文，旧默认不变；Go迁移定向1.502s通过。Vue读取入口先RED再接MigrationEvents，固定所选ID、显式读取、每页20、精确顺序/上下界/游标校验、UTC原值与折叠证据，较新/更早页栈；关闭详情卸载取消请求，401上报父页停读登录。前端构建及迁移18/18通过，全部前端与Go结果见本轮终态。Task4仍未勾选，尚需翻页/错误/迟到/401边界回归；Task5导出未实现。不发布开发包，无真实账号/目录/服务操作。.160冻结。

Task4完成：精确20条分页游标、较新页失败保留旧证据并禁用分页、关闭后迟到不入新详情、事件401关闭详情停读均通过。Task5部分：两条导出入口先RED再实现MigrationExport；所选ID导出及已提交筛选摘要，草稿不夹带，100事件/5000摘要上限说明，原始JSON字节不重序列化，响应ID/筛选核对，离页/条件变化取消且隔离，401上报。构建与迁移23/23通过。尚待成功下载/大整数/迟到/401/条件变化完整回归、最终review/UI/完整门禁，不打包.161。无真实账号/服务或目录操作，.160冻结。

Task5收尾推进：补原始int64 JSON下载/固定文件名、已提交条件改变后迟到下载隔离、summary401回归；成功摘要回归先RED暴露请求q与响应filter.query字段差异，再修复映射GREEN。构建与全前端212/212、Go资源1.391s/intakeapi2.276s通过。fresh reviewer review_161只读审查无可复现Important/Critical问题，独立迁移27/27通过。

可见mock：桌面列表/详情/按ID事件已检查，390px列表/详情截图保存releases同级.161-desktop/mobile/detail.png。发现筛选使用未定义event-filters导致原生控件堆叠，改复用event-filter-panel event-filter-row，重建后桌面与390px复核。表格窄屏横滚保留全部列。Escape尝试未见AX变化，不能声称键盘关闭已验证，下一轮需定位焦点再复核。tab16已关闭、viewport恢复、fixture161.cjs删除、自有88978进程停止、18091无监听。未请求真实账号/目录/凭据。Task5保持未勾选：最终键盘QA、全门禁/三平台/ZIP快照尚未完成，不打包当前开发页。

最终键盘QA：本机mock明确焦点打开落在关闭按钮，locator.press(Escape)后dialog数量0、AX焦点返回查看详情，验证通过。旧pressKey(null)无变化只是未定焦点的尝试，不当作失败证据。自有66057进程停止，fixture161.cjs与tab17清理。独立review无阻塞，桌面/390px与最终键盘均完成；release说明已加入五项。本版功能冻结，进入package完整门禁，Task5仍待三平台/ZIP及源码快照核验后勾选。无真实账号/目录/凭据操作。

最终交付：五项完成。package.ps1全部门禁通过，前端212/212、legacy172/172、userscript3/3、Go全仓/vet与三平台构建通过。独立ZIP13/12载荷SHA、642源码快照及PE/ELF架构验证通过。将最终文档同版封包，最终哈希以releases同级.161-validation.md为准。独立review无阻塞，桌面/390px/键盘均验收，mock/fixture/tab清理。无真实账号/NAS/目录或授权操作，未启动发布二进制。
