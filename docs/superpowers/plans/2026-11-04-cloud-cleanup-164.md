# .164 网盘清理工作台实施计划

> 使用superpowers:executing-plans原目录内联、逐项TDD，最后一次fresh-context整体review。用户站立授权覆盖常规实现和打包；不worktree/自动commit/push。

Goal：将重复文件与空目录清理迁入统一Vue工作台，清楚表达缓存候选和远端执行边界。
Architecture：CloudCleanupWorkspace负责导航/auth与分区；DuplicateScanStatus负责运行；DuplicatePreview负责分页分组/保留选择；EmptyDirectoryPreview负责候选；CleanupBatch负责固定清单与串行结果。精确DTO沿用现有identity_format=string，复用MutateCloud的RequireEmptyDirectory/ExpectedContentSHA256。
Tech：现有Vue3/TS/Vite、Go、SQLite，不加依赖。
Spec：docs/superpowers/specs/2026-11-04-cloud-cleanup-164.zh-CN.md。

## 全局约束

写操作前固定身份和内容证据；unknown不重发、不继续批次。在线冲突后刷新缓存/候选，以后端结果为准。int64字符串、UTC/本机读取/worker分别标识；认证/离页/迟到隔离；旧默认API保持兼容。

## Tasks

- [x] Task1：核实重复/空目录preview和mutation契约，精确DTO与旧兼容先RED→GREEN；独立路由/认证和只读表格骨架。
- [x] Task2：哈希扫描状态/预估/固定run控制与可收起单在途进度；失败/恢复/互斥与未知读回回归。
- [x] Task3：重复组分页、路径/大小/时间证据、保留副本选择与每组至少一份；刷新重置和迟到隔离回归。
- [x] Task4：空目录候选、明确缓存完整性、保护范围与有界分页；选中范围与线上RequireEmptyDirectory接线回归。
- [x] Task5：固定清单确认/串行执行/成功刷新/未知停批/operation链接；一个最终review，桌面/窄屏/键盘与全发布门禁后发布.164。

## Ledger

开始前检查主目录dirty改动，保留.152–.163累计内容，不自动提交。现场.163 ZIP SHA256 F5F7A0BA02BFA36D784B6D306893C8A876A728F76EF4C6D495C873B9ECDB3C0E，与交付记录一致。用户恢复active，撤销此前交付后暂停安排。
Pre-flight: Task1 DTO→Task3/4使用精确fs_id/size/provider_mtime；Task2运行状态不能从checkpoint推断worker；Task3/4固定候选→Task5要求hash/empty安全标志；既有decodeCloudMutation支持两标志和精确Expected字段，执行前线上内容与身份再核对。空目录后端目前返回全量本地候选，后续分页须避免伪称已降低数据库内存开销。


Task1完成：精确重复组/空目录preview接口先RED（大ID仍数字、空目录仍数字），再可选identity_format=string，旧默认兼容。重复组size/条目ID/mtime/总计/游标字符串，旧内容校验UTC保留；无账号字段。独立/cloud/cleanup路由404先RED，接Vue+保护路由后GREEN。只读表格需明确点击预览；每页10组、每组初显20份，超出明确标识，后续Task3补完整选择。Go intakeapi/webui全包与主包Management通过，前端265/265（该时4项清理UI）。
Task2完成：精确状态与估算接口白名单无account_fingerprint；worker由运行进程互斥读取而非checkpoint推断。启动expected_run_id复用request context，在reservation内、任何持久修改前核对；暂停扩展既有固定run方法duplicate分支，在inventory mutex内核对并cancel。旧接口无expected保留默认行为。陈旧启动/暂停和重启无worker先RED→GREEN，完整TestDuplicate定向28.560s通过；scan/refresh旧确认回归及intakeapi全包通过。
DuplicateScanStatus独立确认/预估/可收起/单在途5秒完成后轮询，未知不重发且强制写后GET，pagehide/BFCache/401隔离。当前清理UI7/7通过，包括大计数/纳秒UTC、旧结果保留、单次确认、unknown读回与迟到响应。构建/TS通过；剩余最终整版门禁/可见UI/一次整体review在Task5。Task3–5未实现，未发布.164，未访问真实账号/目录/授权或启动真实服务。下一步Task3：本页保留副本/有界展开/多余选择、刷新重置；随后Task4空目录与Task5固定批次。
Ruling: 只对新可选字符串DTO投影精确字段，legacy JSON保持；新增worker只在新投影读取，不持久化进SQLite。没有更改哈希扫描间隔/退避或数据库schema。

最终本轮全前端268/268通过，diff检查通过。无运行中测试进程；只保留本版可清理的本地测试日志，未创建外部Go缓存目录。


Task3完成：保留radio与待删checkbox互斥，当前预览本地选择、可见项批量选择、每20份逐次展开、刷新/翻页/失败清空选择；批次拒绝保留ID或路径落入任何待删项。回归先RED缺控件，再GREEN。
Task4完成：新增SQLite立即为空叶目录的count/limit页，coverage/run/generation和行同一只读事务；排除监控根、后代、祖先与根/本地不完整目录。expected代次冲突409拒绝混页，UI清空选择并要求回首批。旧无分页接口继续返回空子树祖先，未删除旧能力。Store分页/queue-safe边界/代次先RED→GREEN，API精确页先RED→GREEN，UI大代次/冲突先RED→GREEN。
Ruling: 新工作台只列立即为空叶目录，避免把仍含空子目录的祖先伪称可立即删除；删除子目录并刷新后再确认父目录，不自动扩展已确认范围。SQL真正LIMIT取行，未先读取全量；全局覆盖/总数计算仍有本地SQL成本，不宣称常数时间。
Task5实现完成，待最终发布门禁/ZIP后勾选：固定清单深拷贝、单次消费/逐项串行、停止后续/最小化、按operation ID回执、失败/unknown停批、成功刷新并等待完成后发下一项。unknown锁本页重发，所有页面请求具生命周期隔离。新增expected_retained兼容可选字段，目标和保留项线上hash+metadata核对；prepared事件记录cleanup_verification/本机checks_completed_at_utc，不回填旧证据。retained缺失/内容变更/同一ID/哈希后身份变更均拒绝删除，matching只删除一项且2次内容读取；prepared证据检查通过。API retained大ID精确解析通过。
整体fresh review一次（/root/review_164）源码及补充规格核对无Important/Critical；主任务额外复现批次与hash控制迟到401影响BFCache新页、离页unknown计数错误，已RED→GREEN修复。最终清理UI16/16通过，TypeScript/构建/diff通过，定向Go检查通过。
桌面mock三项串行删除confirmed、候选逐项刷新、operation链接和Escape焦点返回；390px确认三列完整、scrollWidth=clientWidth=375。抽屉表格宽度与checkbox宽列现场修正并复核。截图releases同级164-desktop/confirm/mobile.png。mock7418停止、fixture164.cjs删除、tab1关闭、viewport reset；无真实账号/授权/服务操作，未启动发布二进制，无worktree/commit/push。
下一步最终全前端/legacy/userscript/完整Go/vet/三平台及ZIP/源码快照验证；.163冻结不改包，本批不拆小版。


最终实现与验收完成：清理UI16/16、全前端278/278、legacy172/172、userscript3/3；完整go test -p1 ./...（downloader267.701s/taskstore219.756s）、go vet及Windows AMD64/Linux AMD64/Linux ARM64构建通过。首包ZIP13文件/12载荷哈希/686源码文件与快照总hash、PE/ELF架构校验通过。更新交付记录后同版本重打包固定最终文档；最终SHA只在releases同级164-validation.md，未公布中间包作为最终产物。五项全部闭环，本版冻结后按active目标规划下一批。
