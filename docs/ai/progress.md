# 持续优化进度

## v0.8.30-preview.151（网盘管理交互完善，本地实现 5/5，发布验收中）

集中交付：浏览位置/前进后退、属性面板、页面操作对话框、可缩小后台进度、明确的扫描次数模式。设计与执行清单：`docs/superpowers/specs/2026-11-02-cloud-workspace-151.zh-CN.md`、`docs/superpowers/plans/2026-11-02-cloud-workspace-151.md`。五项全部通过后再创建新预览包；真实状态保持在 .150。 当前前端 17/17、legacy 172/172、新次数模式/恢复/SQLite 原子证据回归和 vet 已通过；独立审查发现并关闭一项跨运行次数选择问题，完整发布与可视验收进行中。

## v0.8.30-preview.150（下载工作台与全站界面重构，已交付）

真实任务重启诊断确认：AI量化交易第1期在旧进程 18:33 已进入 delete 后处理并记录 unknown，20:07 重启未造成此状态。本地 1,186 文件全部重新 SHA-256 核对匹配；当前监控目录为空，但没有旧删除回执，保持隔离。修正“正在等待目录内容”的错误文案，并为未来未知结果补固定原因/HTTP/纯数字异步 task ID 与响应观察证据，不回填历史、不重发操作。详见 incidents/2026-11-02-source-unknown-after-download.zh-CN.md。

同版本追加并发下载日志关联：batch_id 复用根任务 AttemptID，transfer_id 在单文件获取链接/HTTP/续传回退/校验发布间保持一致；增加 UTC、完整路径、阶段、低频进度及安全失败分类，默认并发保持 2。模拟并发隔离/字段关联和凭据不泄漏回归通过；最新发布来源及哈希以同级 validation 为准，详情见 `download-logging.zh-CN.md`。

已提交旧改动并确认干净基线 `48306e7`，随后在原目录实施最高优先级 UI 重构。Vue 3 根任务总览与详情文件表格、分状态排序/筛选、聚合统计、事件抽屉、新任务策略设置和全站侧栏/表格主题已实现。网盘文件浏览移至默认首屏，缓存/清理/设置/历史拆分为功能分区；静态文件离线嵌入 Go。

目录树在文件传输前安全创建，发现的文件先显示待下载，列表响应复用。针对目录预创建/无额外列表、待下载可见、表单真实结构、认证、ID 精度及 Vue 构建产物交互增加回归。实屏发现并修复原历史筛选 form 未闭合导致浏览表单丢失和脚本中断。

最新 .150 来源 e23eaeb，完整 Go 测试/vet、前端 7/7、WebUI 172/172、userscript 3/3、三平台构建及载荷校验通过。追加了持久化传输用时/平均速度、44px 网盘列表、独立大小列、面包屑/上一级/空目录结构和扫描节奏修正。真实状态现位于 .150，授权就绪，源任务仍保持后处理隔离；早期 .149 未重启的记录仅适用于最初验收时点。旧历史明细和统计不回填。当前源码、包哈希及 runtime 证据以 releases 同级 `.150-validation.md` 为准。
## v0.8.30-preview.149（自动监控列表请求退避与重启恢复，5/5）

1. 自动队列巡检只对可重试的目录列表错误退避；结构化读取百度 `Retry-After`，本地 30 秒指数退避最高 15 分钟，provider 建议最多等待 1 小时。
2. SQLite v40→v41 新增账号隔离 retry state，保存失败次数、固定错误码、等待毫秒、retry_at 和 updated UTC；旧失败没有被虚构回填。
3. 冷却期由 Downloader 在扫描历史创建和百度请求之前拦截，仍走明确 skip；重启后读取原截止时间，不会立即重发目录请求。
4. 百度 `ListDir` 成功后重置退避；不可重试失败不留下 cooldown。mock 503 / Retry-After、冷却中无请求及到期成功恢复均有回归。
5. health API 和任务页展示 UTC 重试截止、剩余秒数与等待说明；不暴露账号指纹或 provider 原始错误。

定向 SQLite、Downloader 和迁移回归已通过；全仓 Go 测试、vet、WebUI 172/172、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP 12/12 载荷 SHA-256、491 条源码快照、`git diff --check` 均通过。授权续期用例曾在一次长时间并行全仓运行中失败，单测连续 5 次及随后发布全仓门禁均通过，未改动该无关逻辑。

唯一 `.nas-downloader` 从 `.148` 移至 `.149`，43 个文件迁移后可读取且旧路径不存在；SQLite v40→v41、integrity `ok`、任务 0、retry state 0。真实授权状态可用，自动监控启动时只读目录列表成功一次；未手动启动全盘扫描、下载或远端写操作。包 SHA-256 `BFD5B4E7A96F18D5B8FF6D35143D46C0D981629141851B50D4FE5C8E443CD459`；记录见 `v0.8.30-preview.149-validation.md`。外部限流行为与 NAS/SMB 仍未验收。

## v0.8.30-preview.143（全盘扫描历史指定轮次对比，6/6 已交付）

- 新增两轮完成扫描跨页选择和清除控件；选择仅保存在当前管理页内存，最多两轮。
- sequence 自动排列前后轮，计算覆盖差值和九类原因差值；快照/原因缺失分别安全处理。
- 比较视图显示两轮 run ID、sequence、观察 UTC 与结果；显式下载 JSON 使用字段白名单，不包含账号指纹或网盘路径。
- WebUI 163/163、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 发布门禁通过。真实账号目录 `.142`→`.143` 原位移动后 schema v38 / integrity `ok`，health `ok`、OAuth ready 且账号绑定、队列/source_unknown 为 0、自动扫描 worker 未运行、`/cloud` HTTP 200。只读接口显示既有历史；没有请求云端目录、启动扫描或执行远端写操作。完整包哈希和验证边界见 releases 同级 `v0.8.30-preview.143-validation.md`。
- 规格/计划：`docs/superpowers/specs/2026-11-02-full-scan-selected-run-comparison.zh-CN.md`、`docs/superpowers/plans/2026-11-02-full-scan-selected-run-comparison.md`。
## v0.8.30-preview.142（全盘扫描覆盖历史变化对比，6/6 已交付）

1. Store 以同账号的紧邻上一条完成轮比较；失败、暂停扫描不改变完成轮顺序，sequence 决定顺序。
2. 比较覆盖状态和未完整目录数，差值为本轮减前序；双方观察 UTC 与 run ID/sequence 随比较返回。
3. 两轮原因快照都记录时计算九类原因差值；旧 NULL 显示不可比较，不跨过缺失快照、不用当前状态代替。
4. 扫描历史列表、单轮证据 JSON 导出使用同一 DTO；不输出内部账号指纹。
5. WebUI 显示状态变化、计数改善/增加、前后时间和无前序/缺失证据提示；本地历史读取不请求百度 API。
6. Store 包括相邻完成轮、失败间隔、账号隔离、前序缺快照、单轮导出脱敏测试；WebUI 162/162、userscript 3/3、全仓 `go test ./... -count=1`、`go vet ./...` 及 Windows AMD64/Linux AMD64/Linux ARM64 发布门禁通过。

实际运行切换后 SQLite schema v38 重开且 integrity `ok`；health `ok`、OAuth ready 且账号绑定，下载任务/source_unknown 均为 0；全盘扫描 worker 未运行、历史为空，管理页 HTTP 200。未请求百度目录、未启动全盘扫描或执行远端写操作。详细 SHA 和边界见 releases 同级 `v0.8.30-preview.142-validation.md`。规格/计划：`docs/superpowers/specs/2026-11-02-full-scan-coverage-history-deltas.zh-CN.md`、`docs/superpowers/plans/2026-11-02-full-scan-coverage-history-deltas.md`。
## v0.8.30-preview.141（全盘扫描覆盖历史筛选，5/5 已交付）

1. 增加历史覆盖状态筛选，旧覆盖快照缺失可显式选“历史未记录”。
2. 增加未完整目录数范围；NULL 不混作 0，范围支持闭区间和零边界。
3. 增加九类原因字段白名单筛选及“原因未记录”；可单独定位有 `.139` 覆盖快照但尚无 `.140` 原因快照的运行。
4. 与扫描运行状态、UTC 日期组合筛选，使用既有稳定序列游标分页。
5. 表单支持条件保留、分页续查、清除回第一页；无新数据库迁移或百度请求。

Store/API 回归、全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 162/162、userscript 3/3 及 Windows AMD64/Linux AMD64/Linux ARM64 打包均通过。ZIP SHA 与源码快照见 `v0.8.30-preview.141-validation.md`。该功能只读本地历史，无百度请求；真实账号体验待切换后的运行检查。规格/计划：`docs/superpowers/specs/2026-11-02-full-scan-coverage-history-filters.zh-CN.md`、`docs/superpowers/plans/2026-11-02-full-scan-coverage-history-filters.md`。

## v0.8.30-preview.140（全盘扫描覆盖原因快照历史，6/6 已交付）

1. 完成时持久化 9 类覆盖原因计数，保留原因可能重叠的语义。
2. 原因快照复用覆盖状态的观测时刻与 generation，在完成事务内与扫描事件一起提交。
3. SQLite v37→v38 为历史摘要加可空字段；已有记录不回填。
4. 列表和单轮导出使用白名单字段；UI 分开展示历史值与当前实时原因。
5. taskstore 历史、重启、回滚、迁移定向回归通过；WebUI 161/161 通过。
6. API 历史/导出白名单回归通过；全仓 Go 测试/vet、WebUI 161/161、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷 SHA-256 无差异。

唯一 `.nas-downloader` 从 `.139` 移至 `.140`，原路径已不存在；重启日志确认 schema v37→v38、`integrity_check=ok`。本机健康 `ok`、OAuth ready 且账号绑定、任务/下载/source_unknown 均为 0，`/cloud` HTTP 200；扫描历史为空，未发起全盘扫描或远端写操作。ZIP SHA、源码快照和限制见 `v0.8.30-preview.140-validation.md`。

详情见 `docs/superpowers/specs/2026-11-02-full-scan-coverage-reason-history.zh-CN.md` 与 `docs/superpowers/plans/2026-11-02-full-scan-coverage-reason-history.md`。

## v0.8.30-preview.139（全盘扫描覆盖快照历史，6/6 已交付）

1. SQLite schema v36→v37 增加 nullable 的历史覆盖状态、未完整目录数、generation 和本机观察 UTC；完成 run、快照、completed 事件原子提交。
2. 注入 completed 事件写入失败时，状态更新和快照同时回滚。
3. v36 迁移保留旧运行 NULL，明确“历史未记录”，不按当前缓存回填。
4. 扫描历史显示当时快照和本机观察时间，并说明当前覆盖见扫描状态栏、可能已变化。
5. 单轮 JSON 证据导出携带同一组持久化快照字段；跨 SQLite 重启读取不变。
6. 历史运行/事件 API 不序列化内部 account fingerprint。

完成快照不可变、跨重启、迁移保留旧记录、事务失败回滚等 taskstore/API 回归通过；WebUI 161/161、userscript 3/3、全仓 `go test ./... -count=1`、`go vet ./...` 通过。Windows AMD64/Linux AMD64/Linux ARM64 构建和 12 项载荷校验通过。唯一 `.nas-downloader` 从 `.138` 移至 `.139`，31 个状态文件逐项 SHA-256 一致且旧路径不存在；schema v36→v37、SQLite integrity `ok`、OAuth ready/账号绑定、任务 0、页面 HTTP 200。启动时自动监控只读目录核对成功；未做百度远端写操作或全盘扫描。SHA、源码快照和验收边界见 releases 同级 `v0.8.30-preview.139-validation.md`。规格/计划：`docs/superpowers/specs/2026-11-02-full-scan-coverage-history.zh-CN.md`、`docs/superpowers/plans/2026-11-02-full-scan-coverage-history.md`。

## v0.8.30-preview.138（缓存覆盖复核、导出与休眠恢复追赶，7/7 已交付）

- 展示缓存原因计数并支持快捷筛选；新增当前页与全筛选 JSON 导出。全量导出逐页绑定 generation/total，冲突或超过 10,000 条整份拒绝；导出字段白名单保留 UTC/run 证据，不含指纹和凭据。
- 页面恢复时发现扫描已完成且缓存 generation 超过已加载清单时，自动追赶刷新一次；扫描仍运行时不重复读清单。
- 新增 5 项 WebUI 回归；全部 WebUI 161/161、userscript 3/3、全仓 Go/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。generation 追赶测试先在移除处理逻辑时失败，再恢复实现后通过。真实账号健康/队列、本地清单及页面只读验收通过；未运行远端扫描或写操作。
- ZIP/SHA/source snapshot 与验收边界见 releases 同级 `v0.8.30-preview.138-validation.md`。

## v0.8.30-preview.137（全盘扫描完成后更新不完整目录清单，6/6 已交付）

- 正在扩展 `.136` 的清单新鲜度处理：页面观察到全盘扫描从运行态转为同一 `run_id` 完成后，已加载清单自动从 SQLite 更新。
- 范围包括排除历史 `complete`、暂停/失败、轮询重复和未打开清单；保留原因/路径筛选并重置到第一页，覆盖其他标签页启动及本页快速完成。
- 规格/计划已创建；4 项定向 WebUI 回归覆盖正常完成、历史完成、暂停/失败、快速完成和未加载清单。WebUI 156/156、userscript 3/3、全仓 Go/vet、三平台包和只读运行验收通过。未执行真实百度全盘扫描或远端写操作。
- 最终 ZIP/SHA/source snapshot 与验证边界见 releases 同级 `v0.8.30-preview.137-validation.md`。

## v0.8.30-preview.136（缓存变化后自动更新不完整目录清单，6/6 已交付）

- 已加载的清单会在本页局部刷新完成及远端操作成功返回后从 SQLite 重读；重复完成轮询不重复读取。请求失败不会触发该清单刷新。
- 重读保留当前原因/路径筛选，回第一页并丢弃旧 generation 与路径游标；没有额外百度请求。
- 新增两项 UI 回归，验证刷新完成去重、筛选保留、本地 API 边界以及成功/失败远端操作行为。定向回归通过，WebUI 152/152 通过。
- WebUI 152/152、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建与 ZIP/SHA/source snapshot 检查通过；本地页面与真实账号健康/队列/SQLite 清单只读验收通过。验证记录：releases 同级 `v0.8.30-preview.136-validation.md`。

## v0.8.30-preview.135（缓存覆盖不完整路径清单，5/5 已交付）

- 新增账号隔离的只读 `/api/cloud-inventory/incomplete-directories`，按本地覆盖判定返回路径、主原因、目录状态、目录缓存存在标记、最近观察 UTC 和刷新运行 ID；不导出 fingerprint。
- 支持按主原因及路径搜索，最多每页 100 条；按路径游标稳定分页并返回同一事务快照的总数及 generation。缓存 generation 改变返回 HTTP 409，页面自动重读第一页。
- 页面仅在用户读取清单时查询 SQLite；可在本地缓存打开路径或把路径填入现有刷新输入框。两项均不启动百度请求；局部刷新仍需显式点击原刷新按钮。
- Store 回归覆盖状态、孤项、路径筛选、游标、generation 和账号隔离；API 验证无百度 Provider 调用、参数拒绝和指纹隔离；UI 验证 HTML 转义、迟到页丢弃、generation 重读和显式刷新边界。
- WebUI 150/150、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建均通过；真实账号只读清单和根目录 GET 验收通过。SHA/source snapshot 与边界见 releases 同级 `v0.8.30-preview.135-validation.md`。

## v0.8.30-preview.134（缓存覆盖不完整原因分布，5/5 已交付）

- `/api/cloud-inventory/scan` 的 `inventory_coverage` 现在附带四种目录状态数与五种缓存结构诊断数；扫描状态和原因分布在同一 SQLite 只读快照计算，账号隔离。
- 结构诊断分别指出缺少目录状态行、状态行无对应缓存目录、父目录未完整、缓存路径关系异常和根目录未完整；类别可能重叠，UI 明示不可相加为去重总数。
- 管理页按需轮询本地状态并展示原因明细；不会发起百度列表请求或自动启动扫描。SQLite 状态失败继续标为不可读并保留上次成功读取边界。
- Store 定向回归及状态/UI失败路径检查通过；WebUI 148/148、userscript 3/3。全仓 Go 测试/vet、三平台发布构建和 ZIP/SHA/source snapshot 见 releases 同级验证记录。
- 真实账号只读切换后 health `ok`、OAuth ready、队列空闲；原因状态 API 与百度根目录 GET 只读成功。没有触发扫描、远端写入或下载；NAS/SMB、ARM64 原生及人工浏览器体验未验收。

## v0.8.30-preview.133（全盘扫描状态与当前缓存覆盖状态分开展示，5/5 已交付）

- 扫描检查点与当前缓存覆盖在同一 SQLite 只读事务返回；generation、未完整目录计数、扫描状态更新时间与运行状态不会跨快照拼接。
- 缓存覆盖算法与搜索结果共享；运行完成但受影响目录 stale 时，扫描运行仍显示 complete，当前覆盖显示 incomplete 与计数。无扫描记录时显示 unknown。
- `/api/cloud-inventory/scan` 保持原顶层字段，新增嵌套 `inventory_coverage`；覆盖读取错误沿用 API 错误响应，不附带陈旧覆盖数据。
- 网盘管理页并列显示运行状态和缓存覆盖，另显示缓存 generation、扫描状态更新时间；状态读取失败时明确标注当前不可读和上次成功读取摘要。
- Store 回归覆盖 complete→stale、unknown/账号隔离及 generation；Downloader/API/UI 回归覆盖扫描中、嵌套 API 字段、错误失败关闭、状态分开展示和不触发百度列表。Store 定向先因接口未实现编译失败，修复后通过。
- WebUI 148/148、userscript 3/3、全仓 `go test ./...` 与 `go vet ./...` 通过。Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP 哈希和源码快照见 releases 同级 `v0.8.30-preview.133-validation.md`。
- 真实账号只读切换验收：health `ok`、OAuth ready/账号绑定、任务/下载/source_unknown 为 0；本地缓存状态可读、实时根目录 GET 成功；未执行百度写操作。NAS/SMB、ARM64 原生与人工浏览器体验未验收。

## v0.8.30-preview.132（局部缓存刷新完成后的浏览上下文一致性，5/5 已交付）

- 刷新完成轮询不再无条件重读当前页面：实时百度视图不会因此增加 GET，无关本地缓存目录也不被刷新。
- 页面只将匹配当前 `run_id` 的完成状态与服务器 `root_path` 绑定；断点续刷/失败范围重试保持当前浏览路径，并使用保存范围决定完成影响。
- 受影响的本地缓存目录自动回到第一页读取新快照并重算目录大小；路径比较按目录分隔符匹配，`/target` 不会误命中 `/targeted`。
- 受影响的缓存搜索保留关键词、回第一页并重新读取；过期运行响应不能触发浏览/大小请求。
- 回归先复现旧行为：受影响缓存目录没有被重新读取；修复后定向刷新相关 WebUI 14/14，完整 WebUI 147/147。全仓 `go test ./... -count=1`、`go vet ./...`、userscript 3/3 和 Windows/Linux AMD64/Linux ARM64 构建均通过。
- 真实账号只读切换后健康 `ok`、OAuth ready/账号已绑定、任务/下载/source_unknown 为 0；缓存根目录读取 51 项，实时百度根目录首屏 GET 返回 10 项。未执行百度写操作；NAS/SMB 与 ARM64 原生运行未验收。ZIP/SHA 与源码快照见 releases 同级 `v0.8.30-preview.132-validation.md`。

## v0.8.30-preview.128（自动监控扫描历史与重启恢复，6/6 已交付）

- 实际自动监控扫描新增 SQLite 摘要和 append-only 事件，schema 33→34 原子迁移；历史按本地绑定账号 ID 的 SHA-256 隔离，token 刷新后仍指向相同账号命名空间，API/UI/export 不返回指纹或原账号 ID。
- 记录本机 started/finished/recorded UTC 和成功目录响应后的 `cloud_observed_at_utc`。失败或未执行不写虚假云端观察时间；错误只存固定分类码。
- 新进程开始同账号的实际扫描时，将旧 `running` 标为 `interrupted` 并原子追加事件；不推断成功或失败原因、不重发远端操作。
- maintenance 与扫描互斥 skip 返回明确 sentinel；`runScanner` 不修改最近扫描或成功时间，且回归确认 skip 不调用百度 `ListDir`。
- `/api/health` 从 SQLite 读取最近实际扫描和最近成功；当前是否正在执行仍来自进程内 worker 标记。历史在百度授权暂不可用时仍可按已绑定账号读取。
- 新增只读 `/monitor-history` 和 API，支持状态/UTC 日期筛选、稳定游标分页、事件按需分页、显式本轮 JSON 导出；页面转义云端路径/错误字段，进入页面不调用百度 API。
- 完整发布验证通过：全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 141/141、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建。ZIP 13 项、12 项载荷哈希和 443 项源码快照逐项核验通过；Windows 与 WSL Linux AMD64 `--version` 正确，staging 清零。随后按用户授权将唯一真实 `.nas-downloader` 从 `.100` 移至 `.128`，25 个文件 SHA-256 一致、旧目录已不存在；SQLite v24→v34、integrity `ok`，health `ok`、OAuth ready、任务/下载为 0；真实目录扫描写入 started/cloud_listed/completed 事件及独立本机/云端 UTC。本轮未调用网盘写操作。ARM64 未原生运行、NAS/SMB 未验收。包 SHA、最终源码快照及边界见 releases 同级 `v0.8.30-preview.128-validation.md`。

## v0.8.30-preview.129（自动监控健康状态与扫描历史一致性，5/5 已交付）

- 根因：`runScanner` 在开始/完成事件写入失败时会保留本进程失败结果，但 `/api/health` 只要 SQLite 还能返回旧成功记录，就不会显示该运行时失败，造成失败被上次成功掩盖。
- 修复：最近运行与最近成功摘要从同一 SQLite 只读事务读取；健康投影在非活动扫描期间优先呈现本进程最近一次实际扫描失败，正常持久历史仍提供最近成功时间。活动扫描期间暂时抑制旧的运行时错误。
- 第二项：历史读取失败明确投影 `scan_history_available=false` 与固定 `history_unavailable`；若保留本机最近成功时间则标记 `last_successful_scan_source=runtime`。WebUI 不再把该内存结果显示为“目录监控正常”。
- 第三项：runtime 回退成功时间在 API 序列化前显式转 UTC；UTC+8 回归先失败（返回 `+08:00`）后通过（返回 `Z`）。
- 第四项：`starting` 状态优先于旧 `last_successful_scan`；WebUI 回归先复现错误显示“目录监控正常”，修复后显示启动中和最近成功时间。
- 第五项：两种任务页的健康轮询复用单请求状态；慢响应期间第二次调用跳过，完成后可发起新请求。WebUI 回归先失败后通过。
- 验证：五项定向回归均通过；WebUI 141/141、userscript 3/3、全仓 `go test ./... -count=1`、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP/source snapshot 门禁均通过。最终包和 SHA-256 见 releases 同级 `v0.8.30-preview.129-validation.md`。
- 切换后真实账号只读启动验收通过：`.129` 健康 `ok`、OAuth ready 且账号已绑定、扫描历史可读；自动监控记录 `/NAS_Download_Queue_preview` 成功，任务数为 0。本轮没有远端写操作；NAS/SMB 未验收。完整 ZIP/SHA 和边界见 releases 同级 `v0.8.30-preview.129-validation.md`。
- 计划：`docs/superpowers/plans/2026-11-02-health-unpersisted-scan-failure.md`。

## v0.8.30-preview.130（网盘分页与目录缓存重核一致性，5/5 已交付）

- **已完成候选项 1/5：指定目录刷新遇到重复分页身份时有界重核。** 局部刷新在百度单页内重复 `fs_id` 或 SQLite staging 检出跨页重复时原会直接失败；现复用每目录最多两次的首页重核，且每次重读前仍耐久预留 API 请求额度。两种集成回归先复现失败，修复后从第一页续读并完整发布。
- **已完成候选项 2/5：实时网盘浏览遇到非首页重复身份时回锚。** HTTP/API 收到非零 offset 页的 `ErrDuplicatePageItem` 后只读回读第一页并设置既有 `boundary_changed` 信号；首页重复及网络等其它错误不重试。API 回归验证准确一次 fallback、无重复回锚及失败不重试。
- **已完成候选项 3/5：目录刷新重核事件记录准确原因。** SQLite v34→v35 为 append-only 刷新事件增加受限原因码；分别记录分页边界不匹配、百度单页重复 ID、本地暂存跨页重复 ID。历史页面展示中文原因；旧事件保留空值，不推测历史原因。对应集成测试先因字段缺失编译失败，迁移、事件读取和原因映射实现后定向回归通过。
- **候选项 4/5：全盘扫描历史也持久记录重核原因。** SQLite v35→v36 为 append-only 全盘扫描事件增加固定原因码，区分页边界身份变化和百度单页重复 ID；事件 API、导出和历史页面共同呈现，旧事件不回填猜测值。迁移及全盘扫描漂移集成回归通过。
- **候选项 5/5：实时网盘浏览提示具体回锚原因。** API 对重复页身份和预期边界不符分别返回固定原因码，页面翻译为中文说明；复用原本就会执行的首页回读，不增加请求次数。intakeapi 与 WebUI 回归通过。
- downloader 全包回归重跑、taskstore 全包、intakeapi 全包、三个相关包 `go vet` 以及分页回归重复运行通过。首次 downloader 全包运行返回失败但输出截断无法确认测试名；后续全包重跑通过，异常已记录在实现计划中。
- 发布门禁通过：WebUI 143/143、userscript 3/3、全仓 `go test ./...`、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP/source snapshot 检查。第一次全仓运行暴露页提交故障注入测试与状态轮询并发时的 DDL 竞争；将触发器改为扫描启动前安装并限定第二页 fs_id 后，故障用例重复 10 次通过，第二次发布脚本全仓测试通过。
- 真实账号只读切换验收通过：按用户授权将唯一 `.nas-downloader` 从 `.129` 同卷移至 `.130`，旧目录不存在、目标 25 个文件；SQLite v34→v36、integrity `ok`，health `ok`、OAuth ready/已绑定、扫描历史可读、`/NAS_Download_Queue_preview` 自动监控 success、任务与 source_unknown 均为 0。根目录首屏只读 GET 成功；未执行远端写操作。`.130` 继续运行在 `127.0.0.1:18089`。移动前文件被活动进程占用，未计算迁移前全目录哈希；NAS/SMB 与 ARM64 原生运行未验收。详细证据见 releases 同级 `v0.8.30-preview.130-validation.md`。

## v0.8.30-preview.131（本地缓存浏览分页代次一致性，5/5 已交付）

- **已完成候选项 1/5：列表页使用单一 SQLite 快照。** 页面内容、总数、目录大小与缓存代次在同一事务读取；没有 schema 迁移。
- **已完成候选项 2/5：旧代次拒绝返回陈旧页面。** 目录与搜索请求传入代次，当前缓存变化时得到固定 HTTP 409 且不附带条目；空缓存以 generation 0 开始。同代次分页继续正常。
- **已完成候选项 3/5：目录翻页冲突自动回首页。** 页面重读一次第一页并提示缓存更新，不重复请求陈旧 offset。
- **已完成候选项 4/5：搜索翻页冲突独立恢复。** 搜索词保留，查询代次与普通目录分页分开重置并重新读取。
- **已完成候选项 5/5：迟到响应不覆盖新上下文。** 目录切换后先发出的慢缓存响应被请求序号丢弃。
- 验收：定向 Store/API 回归通过；全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 145/145、userscript 3/3；发布脚本复跑全部测试并完成 Windows AMD64/Linux AMD64/Linux ARM64 构建。ZIP 13 项、12 项载荷哈希匹配，450 项源码快照；最终 ZIP/源码快照 SHA-256 见 releases 同级 `v0.8.30-preview.131-validation.md`。真实账号唯一 `.nas-downloader` 从 `.130` 移至 `.131` 后只读启动验收通过：OAuth ready/账号绑定、health `ok`、任务/下载/source_unknown 均为 0、扫描历史可读；最终启动后最近成功扫描 UTC `2026-11-01T20:41:00Z`。百度根目录实时 GET、本地缓存页/搜索和代次冲突均通过，未执行远端写操作。NAS/SMB 与 ARM64 原生运行未验收；详细边界见 releases 同级 `v0.8.30-preview.131-validation.md`。

## v0.8.30-preview.127（后台队列与监控可观测性，5/5）

- `source_operation_sending` 事件保存失败时，百度请求确定尚未发出；现恢复 job 快照、清除错误的源隔离并保留 fail-closed 队列错误。故障注入先复现 `source_unknown/sending` 错误投影，再通过；重启后按既有规则进入等待扫描确认的 pending。
- 成功扫描判定源缺失的事件保存失败时，现恢复原状态、错误、重试截止和更新时间。故障注入先复现误写成 failed，再通过。
- 扫描新任务按 1024 条分批保存；当前批次只有在 SQLite 成功后才进入任务视图，后续批次失败不再隐藏已经提交的任务。SQLite trigger 注入第二批失败，回归先复现当前任务列表为 0、修复后显示第一批 1024 条且不显示失败项。
- 三种故障路径按共同根因计为一条改进；三项定向 Go 回归及 `git diff --check` 已通过。另完成独立的队列可观测性改进：任务状态 API 与 WebSocket 增加 `queue_healthy`，SQLite 持久化故障时两种任务页均提示队列已停止处理并禁用暂停控制，详细错误仍通过 `/api/health` 查看；具体错误不进入任务状态负载。
- 另一独立监控缺口是健康接口已有最近成功扫描 UTC/标准扫描错误码，但任务页只呈现授权状态。两种任务页现通过既有 5 秒健康轮询显示扫描进行中/最近扫描结果、最近成功 UTC，HTTP 不可读时不会继续显示旧状态；错误码映射到固定中文，不渲染原始值，乱序返回的旧健康响应会被丢弃。
- 维护状态现通过 `queue_pause.maintenance` 进入 API/WebSocket 状态，避免路径迁移或远端操作阻止领取任务时仍显示“下载队列运行中”。扫描中新增 `scan_in_progress` 健康状态，任务页不再在重试期间继续显示上一次扫描失败。这两处是既有可观测性结果的准确性完善，不另计版本条目。
- 为让提示实际可达，HTTP server 与 WebSocket 现在跟随主程序信号上下文，而不是会在队列故障时被取消的 downloader 上下文；`TestWebSocketCanReportQueueStateAfterDownloaderStops` 确认下载器停止后 WebSocket 仍可发送 `queue_healthy:false`。
- 轮询版定时任务状态刷新现在单请求串行，定时刷新只在无请求时发起；手动刷新仍即时执行。HTTP 状态读取失败或 WebSocket 断开时显示“状态暂不可读”，清除已知队列控制快照并禁用按钮，等待服务端再次确认后恢复。
- 新增两项 WebUI 回归，先复现慢响应期间请求数增长及失败后旧状态/控制仍保留，再验证定时轮询有界和不可读状态 fail-closed。最终 Node 139/139、userscript 3/3、全仓 `go test ./... -count=1`、`go vet ./...`、三平台构建与 ZIP 门禁均通过；具体结果见 `.127` 验证记录。无真实百度账号操作，未验收 NAS/SMB。

## v0.8.30-preview.126（已交付：队列 SQLite 写失败状态投影一致性，5/5）

基于源码审查发现并修正五个独立状态转换的持久化失败路径：领取下载、进入授权等待、扫描身份更新、授权恢复、活动尝试收尾。五项 SQLite 失败回归均先在旧逻辑上失败，再验证内存快照和重新打开后的 SQLite 记录仍为最近成功状态。downloader 全包 228.634 秒、全仓 Go 测试/vet、WebUI 133/133、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建均通过；`.126` ZIP 与最终快照核验见 releases 同级验证记录。

## v0.8.30-preview.125（已交付：任务暂停/取消状态一致性与持久化回滚，5/5）

- **已完成事项 1/5：** 目录任务暂停后，只有该根路径下因 `context canceled` 产生的未完成子项改为 paused；直接/嵌套后代状态协调，已完成、真实失败和相似前缀目录保持原样。
- **已完成事项 2/5：** 取消已暂停目录时，子项不再留在 paused；活动目录取消收尾后，中断子项改为 cancelled，完成与真实失败仍保留。
- **已完成事项 3/5：** Cancel 的 SQLite 事件持久化失败时回滚内存状态、错误、重试截止和 pause 来源，不误报已取消；队列仍按现有 fail-closed 路径停调度。
- **已完成事项 4/5：** 手动 Retry 的 SQLite 写入失败时恢复任务状态、尝试/失败计数、错误、截止、容量摘要和更新时间，避免显示未实际入队的 pending。
- **已完成事项 5/5：** 取消 `retry_wait` 时在 append-only 事件中记录被清除的原重试截止 UTC，区分本机计划时间与云端观测时间。
- 发布门禁通过：全仓 Go 测试/vet、WebUI 133/133、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建。`.125` ZIP 13 项、12 项载荷哈希和 430 个源码快照均零差异；未包含私有运行数据。SHA-256 与完整校验记录见 releases 同级 `v0.8.30-preview.125-validation.md`。
- 验收限本机与 SQLite/mock，不连接真实百度账号，不代表 NAS/SMB 验收。

## v0.8.30-preview.124（已交付：单个下载任务暂停与继续，5/5）

- 推荐范围：单项状态跨重启保存；活动下载可安全中断并保留 partial；重试等待保留原 UTC 截止；管理员 API 与 Poll/WebSocket 控制及状态筛选；远端后处理隔离和全局暂停/维护门禁保持独立。
- 设计与计划：`docs/superpowers/specs/2026-11-02-per-task-download-pause.zh-CN.md`、`docs/superpowers/plans/2026-11-02-per-task-download-pause.md`。
- **已完成事项 1：** pending、retry_wait、downloading 可单独暂停；paused 和原状态写 SQLite，重启后恢复按钮可用，旧状态记录兼容。
- **已完成事项 2：** 活动传输的暂停意图先提交 SQLite，再 cancel 单任务；自然结束后保留 paused、不增加失败数，partial 经 Range 与 SHA-256 校验后续传。
- **已完成事项 3：** 暂停与恢复写 UTC 事件证据；retry_wait 的 deadline、attempt/failure 计数保留，全局暂停和 maintenance 不被单项恢复清除。
- **已完成事项 4：** 管理员 API、Poll/WebSocket DTO、任务状态筛选和显式控制已接线；source operation 已发送/未知时拒绝修改。
- **已完成事项 5：** 全仓 Go 测试/vet、WebUI 133/133、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP SHA-256、13 项内容、12 项 payload hash、428 个源码快照及运行数据排除见 releases 同级验证记录。
- 仅本机 SQLite、模拟接口和本地 HTTP 下载流；未连接真实百度账号，也未在 NAS/SMB 环境验收。

## v0.8.30-preview.123（已交付：下载队列暂停与恢复，5/5）

- 推荐范围：跨重启保留用户暂停状态；停止领取新任务但允许活动下载自然完成；暂停时持续监控并登记 pending；管理 API 保持与维护门禁独立；Poll/WebSocket 提供一致的显式暂停/恢复控制。
- 设计与计划：`docs/superpowers/specs/2026-11-02-download-queue-pause.zh-CN.md`、`docs/superpowers/plans/2026-11-02-download-queue-pause.md`。
- **已完成事项 1：** SQLite 设置保存用户暂停状态与 UTC；旧设置缺字段时保持未暂停，重启读回与失败写入原子性有回归覆盖。
- **已完成事项 2：** 暂停阻止新任务领取，活动任务自然结束；维护门禁独立，暂停期间仍能扫描并登记 pending。
- **已完成事项 3：** 管理 API 要求管理员认证，状态切换幂等；SQLite 持久化失败返回服务错误且不谎报成功。
- **已完成事项 4：** Poll 与 WebSocket 状态返回同一队列暂停信息，页面控制只在显式点击后发请求，失败保留已知状态。
- **已完成事项 5：** 发布门禁通过：全仓 Go 测试、`go vet ./...`、WebUI 132/132、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建。ZIP SHA-256、源码快照和运行数据排除见 releases 同级验证记录。
- 仅本机 SQLite、模拟接口及 UI 自动化；没有连接真实百度账号或 NAS/SMB。

## v0.8.30-preview.122（已交付：下载空间诊断与恢复提示，5/5）

- `.121` 已在 SQLite attempt event 保存本机空间证据，但任务列表仅显示通用错误；事件页还需要展开 JSON 才能核对字节、目录与本机检查时间。
- **已完成事项 1：** 任务持久快照、API DTO 和 SQLite 队列可选保存本地容量摘要；重启读回、老 JSON 兼容、手动 retry 与非空间失败清除均有 Go 回归。
- **已完成事项 2：** Poll/WebSocket 共用安全诊断卡，区分下载前容量不足、查询不可用与写入时磁盘满；显示本地路径、精确字节/易读容量与本机 UTC，未知值不显示成负数。
- **已完成事项 3：** 任务页渲染保留用户手动 Retry 按钮，单纯显示诊断不发起重试或百度请求。
- **已完成事项 4：** SQLite 事件时间线复用专用容量卡，仍可查看折叠 JSON；provider、云端与 HTTP 响应时间保持原字段语义。
- **已完成事项 5：** 发布门禁通过：全仓 Go 测试、`go vet ./...`、WebUI 131/131、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建。全仓首轮测试暴露缓存分页测试压力下 5 秒轮询限时不足；提升至 20 秒并增加解释后，全仓重跑通过。
- `.122` ZIP、SHA-256、源码快照和无私有运行数据检查见 releases 同级 `v0.8.30-preview.122-validation.md`。
- 仅本机文件系统、SQLite、模拟百度 API 和 UI 自动化；未连接真实百度账号，也未在 NAS/SMB 验收容量与配额语义。
- 设计与计划：`docs/superpowers/specs/2026-11-02-download-space-diagnostics.zh-CN.md`、`docs/superpowers/plans/2026-11-02-download-space-diagnostics.md`。

## v0.8.30-preview.121（已交付：下载磁盘空间安全与恢复，5/5）

- **1：Windows/Linux 下载前容量准入。** 目标目录不可查询时失败关闭；所需容量按已验证 partial 的剩余字节计算。
- **2：并发剩余空间 reservation。** 按下载 fs_id 唯一保留，随成功写入释放字节，退出时幂等释放。
- **3：partial 身份与容量门禁。** 未验证的全尺寸 partial 按全文件空间需求处理，容量不足时不截断原 partial，也不请求百度下载链接。
- **4：空间失败证据与分类。** SQLite attempt event 记录本机目标路径、required/available/reserved bytes 和容量检查 UTC；空间问题为普通失败，不安排百度 provider 重试。
- **5：写入中磁盘满及手动恢复。** ENOSPC/Windows 磁盘满不发布文件、保留已写 partial；释放空间后现有手动 Retry 可 Range 续传并完成 SHA-256 校验。
- 全仓 Go 测试/vet、WebUI 130/130、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷哈希匹配、418 项源码快照与工作区逐项一致，未包含私有运行数据。ZIP SHA-256 与验收细节见 releases 同级 `v0.8.30-preview.121-validation.md`。
- 仅本机文件系统、SQLite、模拟百度 API 与 UI 自动化；未连接真实百度账号，也未在 NAS/SMB 验收容量/配额语义。容量只在本进程 Downloader 内协调；独立状态目录若共享下载目标不互相预留。
- 设计：`docs/superpowers/specs/2026-11-01-download-disk-space-safety.zh-CN.md`，计划：`docs/superpowers/plans/2026-11-01-download-disk-space-safety.md`。
## v0.8.30-preview.120（已交付：重复文件扫描运行历史与证据，5 项）

- **1：每轮扫描摘要跨重启保存并迁移旧事件。** schema 32→33 按账号/run 回填可证明的开始时间、最新事件和 evidence；当前 run 由当前 SQLite checkpoint 校准，不从新状态推断旧进度。
- **2：扫描状态、关键事件与摘要投影同事务更新。** 更新失败不会留下部分状态；历史按不可变 start sequence 稳定分页，并可组合状态与 UTC 日期筛选。
- **3：筛选后的摘要可显式导出 JSON。** 上限 10,000 条、全有或全无，HTTP 使用显式 DTO，不输出内部 account fingerprint。
- **4：选中单轮后查看该 run 的事件。** 单轮 cursor 分页、稳定 sequence 顺序、过滤重置及迟到响应保护。
- **5：显式导出单轮完整事件证据。** 单个 SQLite 只读快照，上限 10,000 条并超限整体拒绝；敏感事件字段脱敏，不包含 SHA-256 中间状态。
- 最终门禁通过：`go test ./...`、`go vet ./...`、WebUI 129/129、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建。ZIP 13 项、12 项载荷 SHA-256 匹配、410 项源码快照、无私有运行数据；Windows 版本命令正确且无 staging 残留。ZIP 最终哈希和完整证据见 releases 同级 `v0.8.30-preview.120-validation.md`。
- 仅本机 SQLite/mock/API/UI；真实百度账号、远端写操作和 NAS/SMB 未验收。候选设计和计划分别见 `docs/superpowers/specs/2026-11-01-duplicate-scan-run-history.zh-CN.md`、`docs/superpowers/plans/2026-11-01-duplicate-scan-run-history.md`。

## v0.8.30-preview.115（已交付：本地路径迁移恢复可靠性，5/5）

- **事项 1：源目录隔离改名成功但 SQLite 检查点失败后恢复。** 按迁移 ID 和隔离目录状态继续，并重新核验目标。
- **事项 2：部分清理隔离源后安全续跑。** SQLite 追加阶段证据保存目标目录树 SHA-256；重启目标变化或旧记录缺凭据时保留残余数据并失败关闭。
- **事项 3：回退复制异常后保留恢复凭据。** 同步/关闭失败，或复制中断且残片无法删除时保留已核验 `.part`；重试仅在残片为其严格前缀时修复。
- **事项 4：拒绝与新显式启动配置冲突的恢复。** `.env`/启动参数锁定路径与持久迁移目标不一致时记录失败，不继续删除隔离源。
- **事项 5：相同路径提交可重试已失败迁移。** 用户勾选迁移后复用原失败迁移 ID；普通相同路径保存仍不创建迁移。
- **最终验证通过**：打包脚本 WebUI 121/121、userscript 3/3、串行全仓 Go 测试、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建。ZIP 13 项、12 项载荷哈希匹配；397 项源码快照零差异；Windows/Linux AMD64 版本命令与 ARM64 ELF 架构核验通过。最终哈希与完整证据见 releases 同级 `v0.8.30-preview.115-validation.md`。
- **验收边界**：本机文件系统、SQLite、mock 和 UI 自动化；未访问真实百度账号、未执行云端写操作或下载；NAS/SMB 未验收。

## v0.8.30-preview.116（已交付：本地路径迁移与运行配置恢复体验，5/5）

- **已完成候选事项 1/5：迁移完成检查点失败后可在当前进程重试。** 目标目录已完成 SHA-256 核验、隔离源目录已删除、路径设置已保存后，如果 SQLite 写入最终 `completed` 检查点失败，旧逻辑会退出 runner 并留下永久 `running/deleting_source`。现在尽力保存带明确错误原因的 `failed/deleting_source`；由于目标摘要证据已持久化且隔离源已确认删除，同路径表单重试会复用原迁移 ID并完成终态记录。SQLite trigger 故障注入先复现旧实现失败，再验证当前进程重试成功；定向 downloader 测试通过。
- **已完成候选事项 2/5：启动配置冲突拒绝迁移后解除队列维护门禁。** 启动时 SQLite 有活动迁移会先进入维护；若 `.env`/启动路径与记录冲突，恢复安全拒绝并持久化为 failed，但原逻辑没有 runner 收尾，队列一直无法调度。现在只有拒绝记录写入成功后才调用 `EndMaintenance`；记录失败仍保持维护隔离。回归按“建立活动记录→关闭→冲突配置重开”的真实顺序先复现 `maintenance=true`，修复后状态记录为 failed、隔离源保留且门禁解除。迁移相关定向测试通过。
- **已完成候选事项 3/5：启动配置与已保存下载路径拒绝指向状态目录的 junction。** 页面写入和迁移入口原本解析 junction/symlink 后校验，启动加载 `.env` 或 SQLite 路径却只做字面比较，可能把下载数据写进程序状态树。启动现在复用解析后的路径校验；显式配置与持久设置各有 RED/GREEN 回归，正常设置跨重启及迁移别名测试通过。
- **已完成候选事项 4/5：不迁移文件改下载路径前要求确认目录语义。** 本地路径改变但未选“迁移本地文件”时，页面现在明确说明旧文件留在原目录、新下载写入新目录；取消不发保存请求，确认后才更新设置。WebUI 回归覆盖取消、确认和路径读回；完整 WebUI 122/122 通过。
- **已完成候选事项 5/5：迁移期间锁定路径表单，终态后从服务端读回。** 迁移进行时禁用监控/下载路径、迁移勾选和保存按钮；完成或失败后刷新权威路径及 `.env` 可编辑状态。失败可能发生在设置已保存之后，重读后不会让迁移前的旧表单值被误用于反向迁移。WebUI 回归先复现 running 状态仍可编辑，再验证 failed 后路径读回及表单恢复。
- **待定行为：监控目录变更时如何处理未执行队列项。** 已发现扫描后旧目录的待处理项目可能变为 `source_missing`；此前已向用户列出“确认影响后继续 / 有未执行项时阻止变更 / 旧目录继续监控”三种处理方式，等待选择，未据此修改行为。
- **最终验证通过**：WebUI 123/123、userscript 3/3、串行全仓 Go 测试、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建；ZIP、载荷哈希、源码快照和私有运行数据排除经独立复核。最终 SHA-256 与验证细节见 releases 同级 `v0.8.30-preview.116-validation.md`。
- **验收边界**：本机文件系统、SQLite、mock 和 WebUI 自动化；未访问真实百度账号、未执行云端写操作或下载；NAS/SMB 未验收。

## v0.8.30-preview.114（已交付：远端操作重启恢复与本地一致性，6/6）

- **已完成事项 1/5：不确定的远端操作恢复时让对应目录缓存失效**。进程在百度变更请求中退出后，SQLite 原会将遗留 `prepared` 操作改为 `unknown`，但保留相关目录的 `complete` 覆盖，网页可能继续把旧缓存当成完整。现于同一 SQLite 事务追加恢复证据、将源/目标路径与父目录覆盖置为 `stale` 并递增账号缓存 generation；保留缓存条目及 SHA-256，不访问百度、不重发操作、不解除 unknown 隔离。测试先在旧实现复现，再验证账号隔离、缓存/哈希保留和 SQLite trigger 故障时整笔回滚。
- **全批次最终验证**：串行 `go test -p 1 -count=1 ./...`、`go vet ./...`、WebUI 121/121、userscript 3/3、`git diff --check` 通过；未访问真实百度账号或 NAS/SMB。
- **已完成事项 2/5：已确认但本地同步未完成的远端操作在重启后使缓存失效。** 为 `confirmed` 操作追加幂等的 `local_reconciliation_complete` 事件；本地缓存处理、队列身份处理和监控队列核对完成后才写标记。重启发现缺标记时，只在 SQLite 事务中将源/目标影响目录标为 `stale`，记录 UTC 路径证据并写独立的 `local_cache_recovery_complete` 标记；它只代表缓存已安全失效，监控任务队列仍由授权后的常规目录核对流程处理。保留网盘操作 `confirmed` 与缓存行/hash，不重发远端请求。既有版本的 `confirmed` 历史没有此标记，首次升级启动会各自失效一次，此后不重复。
- 已观察到旧 UI/存储回归误把“缓存失败”事件当作最后一条事件；调整断言按事件内容检索，追加的新完成事件不覆盖原错误证据。并行全仓第一次复核遇到既有 Retry-After HTTP-date 用例临界失败（剩余 15.999 秒，小于断言下限 16 秒）；该用例单独连续 5 次通过，串行 `go test -p 1 -count=1 ./...`、`go vet ./...`、WebUI 120/120、userscript 3/3 和 `git diff --check` 全部通过。未访问真实百度账号或 NAS/SMB。
- **已完成事项 3/5：unknown 操作的缓存失效失败会在重启后重试。** 旧逻辑忽略 `InvalidateCloudInventory` 错误，且启动恢复不检查已为 `unknown` 的操作。现当前请求失效成功后记录 `local_cache_recovery_complete`；失效或标记写入失败时返回明确 warning 并记录可写入的诊断，缺少标记则启动事务性重试、追加 UTC 目录证据并保持 `unknown`。SQLite trigger 故障回归验证恢复回滚、去除故障后收敛；Mutation mock 验证失败时无错误完成标记且重启后 stale。
- **已完成事项 4/5：unknown 状态持久化失败时仍立即隔离来源。** 模拟远端已返回但 SQLite 拒绝把 `prepared` 更新为 `unknown`：当前进程即刻按 fs_id 与源/目标路径阻止相关操作，尽力让缓存 stale，并在仍可写入时追加持久化失败证据；不改任务身份、不再次请求远端。去除故障并重启后，既有 prepared 恢复将操作转为 unknown 并保持缓存 stale。故障注入回归先失败后通过。
- **已完成事项 5/5：远端操作历史将恢复事件说明为中文且不混淆队列状态。** 针对缓存恢复、本地对账完成、unknown 恢复及 unknown 状态持久化失败事件显示语义说明和影响路径，保留 UTC 时间及原始 JSON，并验证路径 HTML 转义；界面明确缓存 stale 标记不代表队列已同步。WebUI 回归先失败后通过。
- **已完成事项 6/6：确认删除回执在进程重启后恢复未执行队列项。** 复现远端删除已确认、SQLite 写入本地队列事件失败、重启后的任务曾被误标为“源文件缺失”。现仅在成功认证的完整监控目录扫描中，以账号、fs_id、源路径、创建时间和准备阶段原始文件身份精确匹配 SQLite 的 confirmed 删除回执；只恢复零 attempts/failures 的 pending/auth_wait 任务，原子追加带 operation_id、响应观察时间和本地记录时间的删除事件，不重发远端操作。仅匹配监控目录内来源；目录外已移动任务不查询该回执。SQLite 触发器故障、跨重启恢复、凭据边界及 move-out/move-back 回归通过。
- **修订版发布门禁通过**：`scripts/package.ps1 -Version v0.8.30-preview.114 -AllowDirty -UpdatePreview` 完整执行通过；WebUI 121/121、userscript 3/3、全仓 Go 测试、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建均通过。压缩包 13 项、12 项载荷哈希匹配、395 项源码快照匹配，包内 Windows 版本为 `.114`。重新打包后 SHA-256 与详细验证见 releases 同级 `v0.8.30-preview.114-validation.md`。首次全仓测试发现的目录外移动边界已修正，并由完整重跑确认。
- **验收边界**：仅本机 SQLite/mock/UI 自动化与跨平台构建；未访问真实百度账号、未执行云端写入或下载，NAS/SMB 未验收。

## v0.8.30-preview.110（已交付：目录缓存覆盖、新鲜度与确认操作身份一致性，5/5）

- **已完成事项 1/5：覆盖检查补齐文件/目录父节点与根节点证据**。SQLite 回归覆盖文件/目录父节点缺失、根目录覆盖状态缺失，以及 `path`/`name`/`parent_path` 不一致但父目录状态均 complete 的语义错位；大小、搜索覆盖计数和全盘扫描完成门禁现已核对这些关系。
- **已完成事项 2/5：内容未变化的局部刷新同步目录大小观察时间**。同一事务重算受影响目录及祖先缓存的子树最早完整观测时间，同时保留聚合值、generation 和本地计算时间；嵌套目录最小观测时间回归通过。
- **已完成事项 3/5：局部刷新终止失败清理不可恢复的 staging 行**。failed 状态、队列清理和账号暂存清理同事务提交，保留此前已发布缓存；taskstore 回归通过。
- **已完成事项 4/5：识别未活跃或暂停的刷新检查点并提供原范围恢复入口**。状态 API 报告当前进程 worker 活跃性；重启遗留 running 或 paused 且有待处理目录时，页面可恢复持久化的原目录。downloader 与 WebUI 回归通过。
- **已完成事项 5/5：文件元数据变化时不再沿用旧 provider MD5**。确认文件改名/移动响应缺少 MD5 且 size/mtime 变化时，缓存旧 MD5 会错误指向新元数据。现仅在 size/mtime 未变时沿用缓存 MD5；先失败后通过的 SQLite 回归和全 taskstore 测试通过。
- **发布门禁通过**：全仓 `go test ./...`、`go vet ./...`、WebUI 105/105、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷哈希、371 项源码快照及运行数据排除核验通过。SHA-256 和验收边界见 releases 同级 `v0.8.30-preview.110-validation.md`。未验收真实百度账号或 NAS/SMB。

## v0.8.30-preview.111（已交付：目录缓存扫描恢复边界，5/5）

- **已完成事项 1/5：全盘扫描区分持久化运行状态与当前 worker 状态**。源码复核发现目录刷新已向网页提供 `worker_active`，全盘扫描只返回 SQLite 的 `running`，进程重启后会让已无 worker 的检查点看起来仍在运行。现在扫描状态 API 同样报告当前进程是否有全盘 worker，网页标出“本进程无扫描 worker”及“开始 / 继续扫描”恢复提示；活动 worker 与重启后的持久化检查点均有 Go 回归，页面状态有 WebUI 回归。聚焦 Go 与 WebUI 测试通过；完整版本门禁已在 `.111` 执行并通过。
- **已完成事项 2/5：局部刷新终止失败可从保存范围发起新一轮核对**。失败会清除不可恢复的队列与 staging，因此不能伪装成续跑；服务端可用旧 `root_path` 启动新 run，已发布缓存保持原样。网页现在只在失败记录有保存范围时显示“重新刷新失败范围”，点击后从头重查该范围并使用新 run ID；根目录重试保留整账号确认，取消时不发请求。WebUI 回归覆盖按钮缺失、同范围新 run 和根目录确认。
- **已完成事项 3/5：扫描 worker 退出前不提供重复启动或恢复入口**。全盘扫描与局部刷新互斥：任一 worker 活跃时禁用两种启动按钮；局部刷新在 paused/running 检查点仍有活跃 worker 时隐藏恢复入口，failed 检查点也等 worker 退出后才显示新一轮入口。避免状态先落库或另一类扫描占用 worker 时触发服务端必拒绝请求。WebUI 回归覆盖两类 worker 的入口互斥、全盘 active/idle、局部刷新 paused active/idle 和 failed active/idle。
- **已完成事项 4/5：重复 fs_id 页失败后全盘扫描可从当前目录首页恢复**。同页重复由 Baidu API 返回可识别错误，跨页重复由 taskstore 返回可识别错误；全盘扫描在非首页遇到任一重复时清空当前目录 offset 和边界凭据，checkpoint 同事务清除该目录旧运行的 `last_seen` 标记。两个 provider 模拟回归复现后都能安全恢复；跨页重复拒绝发布错误目录。
- **已完成事项 5/5：SQLite 页提交失败时保留可恢复的旧边界 checkpoint**。全盘与局部刷新原会先覆盖运行态边界，再提交 SQLite；末页提交失败时出现“旧非零 offset + 空边界”，failed 状态也因此无法保存，DB 留下无 worker 的 `running`。现在以候选状态副本提交，SQLite 成功后才替换运行态。全盘扫描通过 SQLite trigger 注入末页写失败，验证 failed/offset 100/旧边界跨重试保留，移除故障后继续到 complete。
- **发布门禁通过**：全仓 `go test ./...`、`go vet ./...`，WebUI 109/109、userscript 3/3，Windows AMD64/Linux AMD64/Linux ARM64 构建均通过。ZIP 13 项（12 项载荷校验全部匹配）、源码快照 371 项、私有运行数据 0 项；Windows 包内 `--version` 正确。最终哈希和完整证据见 releases 同级 `v0.8.30-preview.111-validation.md`。
- 真实百度账号和 NAS/SMB 未验收；本版只完成本机、SQLite、mock 与包验证。

## v0.8.30-preview.112（已交付：实时目录分页漂移核对与时间证据，5/5）

- 暂定主题：目录缓存分页漂移与恢复边界。
- **边界恢复专项回归（不计版本项）**：原扫描漂移测试仅覆盖页首插入导致偏移边界移动。本轮扩展全盘/局部扫描模拟删除已保存边界项、同 `fs_id` 元数据身份变化，并验证 SQLite 关闭/重开后仍能拒绝变化的边界且不发布目录完整。`go test ./internal/downloader -run 'TestCloudInventoryScan(ResumesPersistedPageBoundaryAfterReopen|RejectsChangedPersistedBoundaryAfterReopen)|TestInventoryScannersFailClosedWhenPageBoundaryMoves' -count=1` 通过；仅本机 mock/SQLite。
- **已完成候选事项 1/5：列表数据缩减后自动回到有效分页**。删除当前最后一页的网盘项目后，实时列表不带总数，旧页面会停在“第 N 项起、0 项”；缓存列表或搜索结果缩减时也可能保留无效偏移。现实时列表空页回到首页；缓存列表/搜索按新总数跳到最后有效页，仍为空或无法计算时回首页。三个 WebUI 回归覆盖实时 offset 100→0、缓存目录 offset 100→0、缓存搜索 offset 200→100；完整 `node --test scripts/webui.test.cjs` 112/112 通过。
- **已完成候选事项 2/5：实时翻页核对页首身份并在列表漂移时回锚**。百度 `list` 已返回 limit+1 前瞻项；现把该项的 fs_id、路径、名称、类型、大小、mtime、MD5 作为下一页预期边界。网盘管理列表和移动目标选择器在下一页首项不符或消失时回读首页并提示期间列表变化；正常翻页复用同一次 limit+1 响应，不增加请求。WebUI 覆盖两个界面的边界发送和漂移回锚，API 覆盖 offset 100→0 的二次读取，downloader 覆盖前瞻身份输出；完整 WebUI 114/114、downloader 全包 148.9 秒通过、intakeapi 全包通过。首次并行全包曾出现一个扫描提交故障回归超时后呈 complete 的瞬时失败，单项重跑和随后完整 downloader 包均通过。仅本机 mock/SQLite，未访问真实百度账号。
- **已完成候选事项 3/5：拒绝无效的前瞻列表身份**。原实现只校验当前页条目，没有校验 `limit+1` 的边界条目；路径与文件名不一致的坏边界仍被返回到浏览器，导致下一页边界请求无法使用。现复用同一条目身份校验并失败关闭。新回归先以无效路径复现旧行为，再验证返回 `ErrIntakeConflict`。
- **已完成候选事项 4/5：边界请求改传哈希指纹**。完整前瞻路径和文件名会随 `expected_boundary` 进入 URL，可能被反向代理访问日志记录，且扩大请求。现在服务端返回并接收 SHA-256 身份指纹，页面只提交 64 位十六进制值；服务端仍用 fs_id、路径、名称、类型、大小、mtime 和 MD5 重算核对。API/WebUI 回归确认漂移仍回锚且路径/文件名不出现在边界请求和前瞻响应中。
- **已完成候选事项 5/5：实时页展示本机读取完成 UTC 时间**。当前列表页与移动目标选择器展示该页列表响应读取完成时间，明确标为“本机读取完成 UTC”，不当作百度端时间或快照时间。WebUI 覆盖两个界面；downloader 回归解析时间并确认时区偏移为 0。
- **发布门禁通过**：全仓 Go 测试/vet、WebUI 116/116、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷哈希、371 项源码快照及运行数据排除核验通过。最终 ZIP SHA-256 `A7F7587A45AEEEE8D12F74DDA3C0C564EA02D86C32D209364D87996D41554C9F`。完整验证见 releases 同级 `v0.8.30-preview.112-validation.md`。
- **真实账号边界**：健康检查确认 `.100` 任务总数、下载中、待处理和 `source_unknown` 均为 0；真实账号 `/` 目录只读 GET 返回 HTTP 200、51 项。运行环境策略拒绝了停止 `.100` 并移动唯一 `.nas-downloader` 的命令，因此 `.112` 尚未完成真实账号启动验收，运行数据未变动。
- **本轮请求预算评估**：当前每目录按 100 项分页；若完成后再逐页完整读取一次用于核对，额外列表请求约等于首遍请求数，扫描 API 调用量最高约增加 100%，且第二遍仍无法排除其自身发生的并发变化。百度列表没有快照游标/版本证据，因此这只能提高漂移发现概率，不能证明原子快照。基于已确认的 API 频率顾虑，不将全量第二遍设为后台默认；后续若增加核对，必须明确范围、请求上限和“扫描期间仍可能变化”的状态语义。
- 大目录缓存浏览静态审查没有发现足以单列的新性能缺陷，暂不把重复聚合或查询合并拆成一项。
- 新批次必须积累至少五条彼此独立的用户结果并通过发布门禁后，才编号、打包；真实账号与 NAS/SMB 验收独立记录。

## v0.8.30-preview.113（已交付：全盘扫描额度与历史审计，5/5）

- 全量二次扫描预计增加约 100% 列表请求且仍不是远端原子快照，不作为后台默认。后续从 0/5 开始累计新的独立用户结果；本批 `.113` 已完成发布门禁并交付。
- **已完成候选事项 1/5：指定目录刷新可设置跨重启累计百度列表请求额度**。网页新刷新可设置总额度，恢复时显式输入追加额度；原不限额检查点也可从当前已用数起设有限额度；0 为不限。每次百度列表 GET 前先由 SQLite 耐久预留，错误/限流重试同样消耗额度；额度耗尽时状态为 `paused/request_budget_reached`，保留队列和检查点，未完成工作不会继续发请求。状态和关键事件记录额度快照；页面说明进程若在预留后、HTTP 前中断会消耗额度但不会超额。schema 27→28。设计与计划见 `docs/superpowers/specs/2026-10-31-scoped-refresh-request-budget.zh-CN.md` 和 `docs/superpowers/plans/2026-10-31-scoped-refresh-request-budget.md`。全仓 `go test ./...`、`go vet ./...`、WebUI 116/116、`git diff --check` 通过；预算耗尽只用本机 mock/SQLite 验证，没有在真实百度账号触发限流或耗尽额度。
- **已完成候选事项 2/5：全盘缓存扫描可设置跨重启累计列表请求额度**。全账号递归扫描也会在每个百度列表 GET 前由 SQLite 耐久预留；失败请求计入，额度耗尽暂停并保留队列、当前目录及页边界，恢复输入追加额度。页面展示已预留数/额度，schema 28→29；SQLite 关闭重开、mock 扫描限额暂停与续跑、API 输入校验、WebUI 状态/提交回归通过。全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 117/117、userscript 3/3 和 `git diff --check` 通过。仅本机 SQLite/mock；真实百度限流或额度耗尽未验收。设计与计划见 `docs/superpowers/specs/2026-10-31-full-scan-request-budget.zh-CN.md` 和 `docs/superpowers/plans/2026-10-31-full-scan-request-budget.md`。
- **已完成候选事项 3/5：保存全盘扫描运行历史与关键事件**。扫描页原先只保留当前检查点，新运行覆盖旧状态。现由 SQLite 保存可分页运行摘要和仅记录开始、恢复、暂停、限额耗尽、计划重试、失败、完成的追加事件；摘要随检查点更新，关键事件与状态在同一事务提交，不逐页制造事件。新页面只读查看 UTC 时间、路径、错误和请求额度，不发百度列表请求；schema 29→30，旧检查点不伪造历史，升级后续跑时记录可观察的恢复事件。taskstore/API/downloader/WebUI 回归、全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 118/118、userscript 3/3 和 `git diff --check` 通过。仅本机 SQLite/mock，未验收真实百度或 NAS/SMB。设计与计划见 `docs/superpowers/specs/2026-10-31-full-scan-history.zh-CN.md` 与 `docs/superpowers/plans/2026-10-31-full-scan-history.md`。
- **已完成候选事项 4/5：导出单轮全盘扫描摘要与事件证据**。从 SQLite 单一只读事务快照导出所选运行及按时间升序的事件；最多 10,000 条，超限整份拒绝，用户点击后下载 JSON。导出 DTO 不含内部账号指纹。taskstore/API/WebUI 定向回归覆盖一致性、上限、显式下载及指纹排除；相关计划见 `docs/superpowers/specs/2026-10-31-full-scan-history-export.zh-CN.md` 与 `docs/superpowers/plans/2026-10-31-full-scan-history-export.md`。
- **已完成候选事项 5/5：全盘扫描历史按状态和 UTC 日期筛选**。SQLite 服务端按运行状态及开始 UTC 日期筛选，边界采用起始日含、结束日含（查询上界为结束日次日），继续用不可变启动序号游标分页；修改/清除条件回到第一页。API 校验日期、状态和分页参数；WebUI 回归覆盖筛选保留与翻页。设计与计划见 `docs/superpowers/specs/2026-10-31-full-scan-history-filter.zh-CN.md` 与 `docs/superpowers/plans/2026-10-31-full-scan-history-filter.md`。
- **发布门禁通过**：全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 120/120、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项、12 项载荷 SHA-256 与包目录逐项比对匹配，源码快照 386 行，私有运行数据未进入包；包内 Windows `--version` 输出 `.113`。ZIP SHA-256 和详细证据见 releases 同级 `v0.8.30-preview.113-validation.md`。
- **验收边界**：仅本机代码、SQLite 和 mock 验证；本轮未访问真实百度 API、未做云端写操作/下载或 NAS/SMB 验收。
- 后续目录缓存事项继续按同一主题合并；纯研究或只补测试不计数。真实百度账号、NAS/SMB 与本机/mock 证据分别记录。

## v0.8.30-preview.109（已交付：目录缓存完整性证据，5/5）

- **已完成事项 1/5：终止失败不再让部分目录永久显示“扫描中”**。全盘扫描已提交至少一页后发生不可重试错误，SQLite 中目录覆盖状态原留为 `scanning`，即使扫描运行状态已为 `failed`。现改为在同一事务把本次扫描对应的部分目录状态置为 `failed`，保留旧/已提交文件项、不伪造观察时间、不标记完整；只更新仍属于该运行的 `scanning` 行。taskstore 回归修复前失败、修复后通过。
- **已完成事项 2/5：页面不再把遍历完成说成远端原子快照**。源码与公开 SDK 参数表明扫描使用偏移分页，未发现该接口列表参数提供快照游标；并发远端变化仍可能造成漏项。缓存浏览、全盘搜索及目录大小现在明确标注“遍历完成（非远端原子快照）”，无结果提示也说明分页期间变化可能遗漏。WebUI 两项定向回归通过；真实百度账号未验证。
- **已完成事项 3/5：目录大小遇到缺失子目录覆盖记录时失败关闭**。目录大小查询原本只汇总状态表里已存在的行，遗漏“缓存中有目录、但没有对应覆盖状态”的情况，可能错误返回完整。新增 SQLite 回归先复现错误结果，再将聚合查询补上双向缺失关系检查；大小仍可显示，但完整标记为 false。隐式根目录 `/` 不要求自身 `cloud_inventory` 条目；根目录大小、远端目录移动/删除/改名及分页扫描回归通过。
- **已完成事项 4/5：全盘缓存搜索将覆盖状态与目录条目双向核对**。全盘搜索原先只数覆盖状态表里的非完整行，目录状态记录缺失时会将搜索覆盖报告为完整，导致零命中看起来像确定不存在。新增 SQLite 回归复现后，改为按路径去重统计非完整状态、缺少状态的缓存目录及无缓存目录身份的状态行；隐式根目录例外保留。搜索完整/不完整及目录大小聚焦回归通过。
- **已完成事项 5/5：全盘扫描结束前验证目录覆盖图**。扫描原先仅以持久化队列为空作为全盘 `complete` 条件；孤立目录覆盖状态可能未进入待扫队列。新增 SQLite 回归注入无缓存身份的目录覆盖行，旧逻辑会让最后一页提交完整状态；现在全盘覆盖图不闭合时回滚最后一页和检查点，交由现有失败路径记录。重启游标与不完整搜索/大小回归通过。
- **发布门禁通过**：全仓 Go 测试/vet、WebUI 104/104、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷哈希匹配、371 项源码快照和私有数据排除检查通过。ZIP SHA-256 与验证边界见 releases 同级 `v0.8.30-preview.109-validation.md`。真实百度账号和 NAS/SMB 未验收。
- **后续方向更新**：这条偏移分页研究方向已拆分并累计为 `.112` 的五项交付；完整门禁和真实账号边界见 `.112` 条目。其后的新候选从 0/5 开始，限定目录核对方案尚未计入版本门槛。

## v0.8.30-preview.108（5 项：后台扫描一致性与状态恢复）

- **已完成事项 1/5：终止扫描失败清除全部重试计划**。源码复核发现全盘扫描和目录刷新在百度返回不可重试错误时清除了最近等待时长与 UTC 截止，却保留 SQLite 中的下一档退避基数；队列状态异常、checkpoint 提交失败等终止路径也有相同遗漏。失败后恢复全盘扫描会从旧基数继续。新增全盘/目录刷新回归，RED 均观察到遗留的 `1200ms` 退避基数，修复后验证 terminal 状态的实际等待、下一基数和截止时间均清空。仅本机 mock/SQLite 验证。
- **分页审查结论**：官方 SDK 当前公开的 `ListDirOptions` 仅说明 `Start/Limit` 偏移分页和排序字段，没有公开快照游标。源码按 `order=name&desc=0` 逐页读取；若扫描期间远端前段发生增删，偏移可能移动并漏项，重复 ID 检查不能侦测从未返回的项目。这是偏移语义推断，非真实账号观察。页边界重叠检测须持久化边界身份才可跨重启；尚未选定完整性协议，不宣称快照保证，也未引入默认全盘重扫。
- **已完成事项 2/5：持久化分页边界并检测可观察漂移**。在现有 `limit+1` 请求已获取的 lookahead 项中记录完整远端身份（fs_id、路径、名称、目录标记、大小、mtime、MD5），与页提交和 SQLite 检查点同事务持久化；重启续页前将返回首项与边界核对。身份不符时不发布不完整目录，重置当前目录偏移并从头重读。未增加百度请求；该检测只覆盖边界可见的漂移，不能证明遍历期间的原子快照。schema 26→27；旧偏移无边界凭据时安全重置到 0，并清除当前目录旧扫描标记。API、SQLite 重开/迁移、全盘与局部刷新漂移回归，以及完整 taskstore、Baidu API、downloader 测试包已通过。仅本机/mock 验证。
- **已完成事项 3/5：SQLite 提交后清单未更新时自动恢复**。进程若在 schema 迁移已提交、`manifest.json` 尚未更新时退出，重启先验证现存数据库是否已经满足当前 schema；验证通过则仅修复清单版本，避免旧 manifest 触发不适用的重复迁移。v24→当前 schema 的迁移恢复回归覆盖该路径。
- **已完成事项 4/5：防止三个云扫描状态轮询重叠**。全盘扫描、目录刷新和重复扫描的后台状态请求慢于轮询间隔时不再并发发起；避免迟到响应覆盖更新进度，也避免目录刷新完成回调重复浏览/统计。
- **已完成事项 5/5：状态读取暂时失败时保留最近成功进度**。重复扫描和本地目录迁移的轮询错误不再把已有摘要改成“尚无记录”或只显示错误；保留最近一次成功进度，并附上本次读取错误。WebUI 回归先复现再通过。
- **发布门禁**：`scripts/package.ps1 -Version v0.8.30-preview.108 -AllowDirty` 成功；完整 Go 测试/vet、WebUI 104/104、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 内容、载荷哈希、版本命令、源码快照和敏感运行数据排除已核验；最终哈希与详细证据见 releases 同级 `v0.8.30-preview.108-validation.md`。真实百度账号与 NAS/SMB 未验收。

## v0.8.30-preview.107（已交付，5 项：缓存扫描恢复与分页身份完整性）

- **问题与复现**：全盘扫描和局部刷新从 SQLite 恢复时，会把有未来 `retry_at_utc` 的检查点显示为 `listing`，虽然 worker 正在等待百度限流截止时间。扩展 `TestInventoryWorkersDoNotRequestBeforePersistedRetryDeadline` 后，RED 分别复现两种扫描阶段错误。
- **修复**：恢复时如持久化重试截止晚于当前 UTC，将阶段保留/设为 `backoff`；已过期或无截止仍显示 `listing`。重试截止仍在数据库中持久化，并由 worker 自行按时清除。
- **验证**：定向回归覆盖全盘与局部刷新；全仓非缓存测试/vet、WebUI 101/101、userscript 3/3、三平台构建、ZIP 载荷哈希、源码快照及私有数据检查均通过。完整证据见 releases 同级 `v0.8.30-preview.107-validation.md`。

- **问题与复现**：指数退避的下一档只在 worker 内存中递增。进程在等待重试、暂停后恢复或重试请求进行时重启，会从数据库里表示“上次实际等待”的 `retry_delay` 重新计算，导致下一档重复使用。任务状态分别覆盖全盘扫描、局部刷新与重复哈希扫描。
- **修复**：SQLite schema 25→26 为三类扫描状态分别保存 `retry_backoff_base_millis`，与实际等待时长和 UTC 截止分开；安排退避时原子保存下一基数，成功发布页/完成候选后清零。旧状态迁移从已有等待值推导一个有界下一基数；旧 schema 没有保存当时的本地基数，因此这是近似回填，若旧等待被 `Retry-After` 拉长，回填基数可能高于原指数序列下一档，最多按既有限制封顶 60 秒。
- **验证**：schema 列存在、V25→V26 回填及 SQLite 关闭/重开回读通过；全盘扫描、目录刷新和重复扫描恢复后的退避聚焦回归通过。完整版本门禁及包证据见 `.107-validation.md`。

- **问题与复现**：重复哈希扫描在曾安排重试后遇到终止错误时，将状态标记为 `failed`，但仍持久化先前的 `retry_delay`、下一退避基数和 UTC 重试时间。直接调用真实 Store 写入失败状态并重开读取，回归断言观察到三项旧重试证据仍在。
- **修复与验证**：重复扫描终止失败时清除旧延迟、基数和重试截止；新增 SQLite 持久化回归，先确认旧行为失败，再确认修复通过。

- **问题与复现**：重复扫描页把 `retry_delay_ms` 显示为“重试间隔”，同时单独显示“下次重试”；即使已失败且没有 `retry_at_utc`，页面仍会把历史实际等待时长放在当前计划语境下。
- **修复与验证**：状态页改为分别展示“最近实际等待时长”和“当前重试计划”；有计划时显示 UTC 截止与剩余等待，已到期时明确标记等待调度，无计划时明确显示无。WebUI 回归先确认旧标签不符，再验证新语义。

- **问题与复现**：全盘扫描仅检测本页之前已提交过的 `fs_id`，同一响应页重复 `fs_id` 会退化为逐行 upsert，后条覆盖前条，并可能把目录标记为 complete。定向 SQLite 回归确认旧行为接受并发布冲突身份。
- **修复与验证**：全盘扫描页在写入前拒绝页内重复 `fs_id`，该页与 checkpoint 一起回滚，完整目录状态不发布。SQLite RED/GREEN 回归及跨页重复回归通过。发布门禁已通过，包内验证详情见 `.107-validation.md`。

- **发布**：v0.8.30-preview.107 已生成独立目录与 ZIP；ZIP SHA-256：`bdcb91cfbb1d4a119f6d404feedba1a4fb83bf23eb96f14a48f37aba6f6cdc07`。没有迁移真实授权状态或连接真实账号/NAS。

## v0.8.30-preview.105（云目录刷新证据一致性，6 项）

- 刷新暂存逐条保存每个百度列表页的 UTC 观测时间，分页过程中关闭并重开 SQLite 后继续保留；缓存行与新增/变化快照采用所属页时间，目录发布事件保留最后一页的完整目录观测时间。
- schema 24 活动暂存条目没有逐页观测证据时，迁移后缓存观测值保持 0 哨兵，变化明细 API 明确返回 `observed_at_utc: null`；前端显示“未知”，不使用末页时间填补。
- 刷新暂存拒绝重复主键；同一 fs_id 在单页或跨页重复时事务回滚，刷新失败，旧完整缓存不变。
- 增加 schema 24→25 持久化迁移；SQLite 提交后 manifest 未更新时可重开验证并补写 manifest。
- **验证**：观测时间、重复 fs_id 和迁移恢复聚焦回归及 taskstore 全量通过；全仓 Go 测试、`go vet ./...`、Web UI 100/100、userscript 3/3 通过。Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷哈希匹配、358 项源码快照、私有运行数据 0 项，Windows 版本标识正确。
- **发布**：最终 ZIP SHA-256 和完整载荷校验见 releases 同级 `v0.8.30-preview.105-validation.md`。仅本机 SQLite 和 mock；没有访问真实百度账号，不把本机结果视作 NAS/SMB 验收。

## v0.8.30-preview.106（缓存扫描互斥与分页完整性，5/5）

- 全盘扫描在后页再次看到同一 fs_id 时拒绝该页事务，避免对已提交记录静默覆盖；旧普通单页 API 的 last-row-wins 测试继续通过。
- 全盘扫描、局部刷新和重复哈希扫描共同占用扫描准入；三条入口也检查 SQLite 中跨重启仍为 running/paused 的冲突扫描状态。
- 重复扫描处于 running、paused 或 failed（仍可恢复）时，CloudMutationScanConflict 在远端调用前拒绝远端 mutation，且 confirmed 缓存更新保持相同冲突语义。
- SHA-256 断点一并保存初始百度 filemetas MD5；跨重启 MD5 漂移触发现有身份对账；缓存或 provider 单侧缺失时丢弃前缀并从零重算，两侧均缺失时保持原续传行为。
- 云端改名、移动和删除在申请扫描准入时即与全盘扫描、局部刷新、重复哈希扫描互斥；在准入冲突时不会写远端操作日志或调用百度写 API。
- 回归先观察 RED：后页重复 fs_id、扫描期间 mutation、provider MD5 漂移均未被识别；修复后页重叠、旧 upsert 契约、扫描互斥、写请求前拒绝和两种续传边界聚焦测试通过。
- **发布验证**：全仓 `go test ./...`、`go vet ./...`、Web UI 100/100、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷 SHA-256、368 项源码快照和私有运行数据排除检查通过。最终 ZIP SHA-256 与完整记录见 releases 同级 `v0.8.30-preview.106-validation.md`。
- **验收边界**：本机 SQLite/mock 验证；没有访问真实百度账号或执行云端写操作。NAS/SMB、Docker 实机和目标浏览器仍未验收。

## v0.8.30-preview.104（本地路径迁移与缓存扫描可靠性，5 项）

- SQLite 阶段检查点在源目录已隔离后失败时，可按同一迁移回执恢复，重新核验目标后再清理隔离目录。
- 路径维护与全盘/局部目录缓存扫描准入同步核对，关闭扫描在维护准入间隙启动的竞态。
- 重启恢复重新验证解析后的源/目标/SQLite 状态目录关系；冲突作为带时间的 `failed` 迁移证据记录，管理网页仍可启动。
- 恢复与同记录重试都校验 tombstone 必须匹配源路径和迁移 ID；损坏回执不能把目标目录作为隔离源删除。
- 源目录在初始盘点后新增文件时，迁移总量随复制进度更新，完成值与实际复制文件/字节一致。
- **验证**：RED/GREEN SQLite、Windows junction、确定性并发和真实目录回归通过；打包脚本全仓 Go 测试、`go vet ./...`、Web UI 100/100、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项，12 项载荷 SHA-256 全匹配，356 项源码快照；Windows 二进制版本正确，未包含 `.nas-downloader`、管理员密钥、百度凭据、实际 `.env` 或下载目录。
- **发布**：ZIP 与源码快照的最终 SHA-256、包内清单和私有数据检查结果见 releases 同级 `v0.8.30-preview.104-validation.md`。
- **验收边界**：本机、SQLite、mock 与构建验证；未执行真实百度远端写操作或 NAS/SMB 验收。

## v0.8.30-preview.103（管理员会话失效后的跨页面恢复，5 项）

- 共享会话失效处理保留当前路径及查询参数，同一页面并发 401 只导航一次。
- 操作历史及目录刷新历史三类导出、手动入队、OAuth 设置与授权、任务列表/API、任务事件和任务操作分别在 401 时跳转登录；本次请求不重发。
- OAuth 状态接口 401 后停止 5 秒轮询；任务轮询版停止 2 秒状态与 5 秒健康轮询，WebSocket 版停止重连；云管理页清除扫描/刷新/重复扫描/迁移状态轮询。
- 聚焦 Web UI 回归 10/10；全仓 `go test -count=1 ./...`、`go vet ./...`、Web UI 100/100、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 发布构建全部通过。
- ZIP 13 项，12 项载荷 SHA256 全匹配，源码快照 359 行，二进制版本正确，无私有运行数据；发布细节、最终 ZIP 哈希与完整校验结果见 releases 同级 `v0.8.30-preview.103-validation.md`。本机自动化验证；没有执行百度网盘写操作或 NAS/SMB 验收。

### `.103` 之后的本机源码审查（2026-10-31）

- 复核 OAuth 页面 `/api/app-config` 读取和保存、其它受保护页面的 401 跳转：已调用共享会话失效处理，本轮未发现新的确定缺陷。
- 复核重复文件预览与删除链路：候选来自完整缓存且具备内容 SHA-256；远端删除前重新核对元数据、重新下载并验证 SHA-256、再次核对身份，随后持久化远端操作并执行删除。批量删除逐次刷新首组，结果数变化或错误时停止后续删除。
- 复核空目录候选：只从完整目录缓存生成，排除包含文件以及存在未完成子目录的目录树；候选按深度从深到浅排列，远端删除前仍重新读取并要求目录为空。
- 对照已发布验证记录，补齐 `.55` 局部刷新、`.77` 只读网络诊断、`.85` 重复扫描字节进度和 `.95` 入队分页四份实施计划中残留的未勾选完成项；`.77` 当前源码 API 与页面回归重新通过，历史包 SHA-256 匹配。
- 本轮源码/计划审查未发现新的确定产品缺陷；重新运行 `.77` 网络错误 API 定向测试和入队页诊断 WebUI 定向测试，均通过。历史 `.77` ZIP SHA-256 与验证记录一致；本轮只改计划/进度文档，未触发真实账号写操作或重打包。下一批仍为 0/5；外部 NAS/SMB 与浏览器验收不计为源码缺陷或版本条目。
- 继续本机只读审查任务失败与登录失效链路：百度 API 错误不含原始请求 URL；下载网络错误在进入任务错误记录前移除 URL；任务错误页面均转义云端/本地文本。入队页面的预检、提交、核对请求收到 401 时跳登录，提交中断仍提示先查记录且不会自动重发；目录/历史异步响应用代次号丢弃过期结果。管理页直接发起的历史导出请求也统一对 401 跳登录。本轮未发现确定缺陷或修改产品代码，候选仍为 0/5。
- 按本机代码审查补查迁移/下载文件发布路径：Linux 使用目录 `fsync` 并依赖挂载端实现，Windows 发布使用 `MoveFileEx` 的不覆盖/写穿标志；本机代码没有针对 SMB 服务端语义绕过错误的分支。Linux 内核 CIFS 客户端源码说明 SMB 目录操作为同步操作，但目标 NAS 型号、内核/挂载选项和服务端持久化仍无法由本机源码或本地盘测试证明，保留为外部验收项；未发现足以支持盲目改动的本地缺陷。
- 静态复核油猴衔接：只适配 `/disk/main` 和 `/disk/home` 的明确选择状态；旧 API 与 Vue 选择同时可读时要求结果一致；路径、目标地址和载荷大小逐项校验，未选择、冲突或未知页面均停止，不调用网盘/管理 API，只通过 fragment 传递路径。现有 Node 用例覆盖这些本地分支；真实百度 SPA 页面/油猴沙箱仍待用户环境实测，本轮未发现可复现的源码缺陷，候选仍为 0/5。
- 发现并修复本地迁移恢复缺口：源目录已改名为同记录的隔离目录后，若 SQLite 阶段检查点写入失败并记为 `failed/source_retired`，原重试入口仍因源路径缺失而拒绝恢复。新增真实 Store/SQLite 触发器回归，先复现重试失败，再允许同记录恢复；恢复会把隔离目录视为待核验源，重新比对目标完整内容后才清理。定向迁移测试、downloader 全包测试、`go test -count=1 ./...` 和 `go vet ./...` 通过。作为 `.104` 候选计 1 条独立 Bug 修复；未运行发布打包。
- 修复路径维护与全盘扫描/局部刷新准入间的竞态：扫描最初检查“非维护中”后，维护可能在其记录启动状态前取得准入。新增确定性并发回归，并让两种扫描在登记 `starting` 时与维护状态使用一致锁序、再次核对，避免维护期间仍启动扫描。用旧准入行为重跑回归时，扫描继续进入后续流程而非被维护拒绝；恢复修复后定向回归通过。downloader 全包、`go test -count=1 ./...`、`go vet ./...` 和相关 `git diff --check` 通过；作为 `.104` 候选计第 2 条独立 Bug 修复。
- 修复迁移重启恢复的路径别名校验遗漏：源路径父目录在停机期间被 junction 重定向到 SQLite 状态目录时，旧恢复入口会启动迁移 worker。恢复前现重新解析并核对源、目标与状态目录；发现冲突时写入带 UTC 时间的 `failed` 迁移证据并保持管理服务可启动，不启动文件操作。Windows junction 回归先观察到旧入口接受恢复，再验证新行为记录失败状态且维护锁未遗留；作为 `.104` 候选计第 3 条独立 Bug 修复。
- 阻止损坏的 SQLite 迁移回执把目标下载目录当作待删除隔离目录：恢复和同记录重试都要求 `tombstone_path` 等于源路径附加该迁移 ID 的隔离路径。此前构造 `phase=deleting_source` 且 tombstone 指向目标目录的记录会完成并删除目标；新增 SQLite 回归先复现该结果，再验证失败记录、重试拒绝且目标文件保留。作为 `.104` 候选计第 4 条独立数据安全 Bug 修复。
- 修复迁移进度总量与实际复制量脱节：扫描完成后新增的文件会参与复制但旧状态总量不更新。现在复制期间总量至少覆盖已复制文件/字节，复制成功后以实际复制量结算；真实目录回归在扫描与复制之间添加文件，验证总量、已处理量和目标内容一致。作为 `.104` 候选计第 5 条独立进度改进。
## v0.8.30-preview.102（已交付：管理登录与路径操作可观测性，5 项）

- 网盘监控目录输入保留首尾空格；登录限流准入时清扫 15 分钟外失败地址，避免容量填满后新地址永久被拒；管理页收到 401 跳转登录并保留原页面；全盘/局部扫描状态读取失败不再谎报“尚无记录”并保留最近成功摘要；本地迁移轮询防并发重叠。
- 五项均有 RED/GREEN 回归。全仓 `go test -count=1 ./...`、`go vet ./...`、Web UI 90/90、userscript 3/3 通过；发布脚本复跑全门禁和三平台构建通过。
- ZIP 共 13 个条目，12 个载荷 SHA256 全匹配，354 条源码快照记录，Windows 二进制 `--version` 正确，未发现私有运行数据。ZIP SHA-256：`108400a6e1e4f82ca5ae85f70704aae4a9d320b0df57b4b844d6b4cfe17fe328`；完整验证见 releases 同级 `v0.8.30-preview.102-validation.md`。
- 本机/模拟验证；未进行真实百度网盘写操作或 NAS/SMB 验收。

## v0.8.30-preview.101（已交付：入队预检与路径维护一致性，5 项）

- 修复五项独立问题：过期预检仅在列表响应派生 `expired` 且不再阻塞路径设置；预检/提交/核对/远端目录操作与路径维护通过读写准入互斥，并固定同一队列目录快照；旧入队历史按记录原目录跨重启加载和核对；并发路径修改在取得维护锁后读取最新迁移源目录。
- 回归证据：过期投影、认证 epoch/重启、旧目录历史恢复、未知结果不重发、维护期间准入和目录变更竞态测试通过。全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 85/85、userscript 3/3 通过。
- 发布证据：`scripts/package.ps1 -Version v0.8.30-preview.101 -AllowDirty` 通过，Windows AMD64/Linux AMD64/Linux ARM64 构建完成；ZIP SHA-256 为 `1706704e204750ea56c52eeced26fc322e81b7299a2305f491e49c940525d1f6`。ZIP 13 项，未发现 `.nas-downloader`、下载数据、实际 `.env`、凭据 sidecar 或 SQLite 私有状态。
- 边界：本机/模拟服务验证；未执行真实百度账号操作或 NAS/SMB 验收。详细范围与校验值见 releases 同级 `v0.8.30-preview.101-validation.md`。
## v0.8.30-preview.100（已交付：重复扫描时线上对象变化对账，5 项）

- **源码发现**：重复扫描在哈希前及哈希后读取百度当前元数据；若对象路径、名称、大小、mtime 或可用 MD5 与缓存不同，旧流程会把整轮任务标为失败。成功观察到的新身份没有完整同步到库存；无唯一对象回执也没有明确的安全暂停/刷新引导。
- **五项实现**：成功观察的当前身份事务性更新 SQLite 库存；清除旧 SHA 并将新旧父目录/大小代次标为失效；无唯一对象回执保留缓存/哈希并暂停；受影响目录刷新完整后重估剩余候选并从原 `next_fs_id` 重验；追加旧/新快照及百度观测 UTC/本机记录 UTC 事件，页面显示暂停原因与需刷新的路径。
- **定向证据**：覆盖下载前漂移、内容读取后漂移、无唯一对象回执、缓存身份并发变化、活动/祖先扫描队列冲突、事件写入失败回滚、目录失效/旧哈希不入预览、刷新后重估和 UI 路径/UTC 转义；taskstore 与 downloader 定向回归通过，新增 WebUI 定向回归通过。
- **发布验证**：`scripts/package.ps1 -Version v0.8.30-preview.100 -AllowDirty` 通过；全仓 Go 测试、vet、WebUI 84/84、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建均通过。最终 ZIP 与源码快照哈希见 releases 同级 `v0.8.30-preview.100-validation.md`。
- **真实账号只读验收**：按既有授权将唯一 `.nas-downloader` 从 `.99` 移至 `.100`；health `ok`、OAuth ready/账号绑定、任务/下载/重试/source_unknown 均为 0，SQLite 启动完整性诊断为 `ok`。`.100` 运行于 `127.0.0.1:18089`。
- **边界**：功能回归使用本机/mock；真实账号没有启动大规模哈希扫描、下载或网盘写操作。百度 Range 与 NAS/SMB 未验收。

## 已交付批次：本地路径迁移恢复可靠性（5/5）

`.114` 已交付。本批当前 5/5，最终全量验证与 `.115` 打包进行中；本批设计与实施记录见 [本地目录迁移恢复设计](../superpowers/specs/2026-10-31-local-migration-partial-retirement-recovery.zh-CN.md) 和 [实施计划](../superpowers/plans/2026-10-31-local-migration-partial-retirement-recovery.md)：

1. **已完成候选事项 1/5：源目录隔离改名成功但 SQLite 检查点失败后安全恢复。** 仅按同一迁移记录的隔离路径证据恢复；继续执行前重新核验目标并持久化恢复失败。
2. **已完成候选事项 2/5：部分清理隔离源后可安全恢复。** 进入 `deleting_source` 前将目标目录路径/类型/文件内容树 SHA-256 与阶段事件原子持久化；每次清理重试重新计算目标树并核验剩余隔离源为目标的匹配子集。目标改变、事件证据缺失或旧检查点无指纹时保留残余源并失败关闭；未变目标的部分删除可跨重启继续。故障注入、目标变化保护及旧记录无指纹拒删回归通过；SQLite schema 不变。
3. **已完成候选事项 3/5：回退复制失败后保留恢复凭据。** 不支持硬链接时，目标 `Sync`/`Close` 失败，或复制中断且目标因占用无法删除，旧实现会丢弃已核验 `.part`，留下不可恢复的目标残片。现在保留 `.part`；重试验证残片是其严格前缀后才恢复，非前缀仍失败关闭。同步、关闭、复制中断且清理失败注入回归通过；旧同步失败行为已先红后绿。
4. **已完成候选事项 4/5：重启后拒绝与新显式启动配置冲突的迁移。** 如果重启时 `.env`/启动参数已将本地下载目录或监控目录锁定到不同位置，旧恢复逻辑仍继续旧迁移并可能清理源目录。现在恢复前比较持久化迁移目标与显式配置；不匹配时追加失败证据并保留隔离源，匹配时允许续跑。旧行为红测观察到迁移被错误标记完成并删除隔离源；本地冲突拒绝和监控路径匹配/冲突校验通过。
5. **已完成候选事项 5/5：配置修正后可从同路径提交重试迁移。** 显式启动路径冲突造成迁移失败并保留隔离源后，用户修正配置时目标路径可能已是当前值，原 API 将相同路径保存直接当作无操作。现在用户勾选迁移并提交相同路径时，会复用匹配的失败迁移 ID 继续恢复；普通保存仍无操作。旧行为红测确认未启动迁移，修复后隔离源成功清理。

`.115` 已完成打包与门禁。发布清单、哈希和逐项验收见 releases 同级 [`v0.8.30-preview.115-validation.md`](../../nas_baidupan_auto_downloader-releases/v0.8.30-preview.115-validation.md)。实现验收使用本机文件系统与 SQLite/mock；NAS/SMB 未实测。本批不访问真实百度账号或执行云端写操作。

## v0.8.30-preview.117（监控目录切换与未执行队列对账，已交付 5/5）

`.116` 已交付。用户选择切换前展示受影响任务并确认；本批已达到 5/5 并交付为 `.117`。

1. **已完成候选事项 1/5：切换监控目录前预览并确认未执行任务。** 网页显示 pending/auth_wait/retry_wait 项并携带当前快照摘要；服务端在路径维护期间重新计算，若队列已变化则返回最新影响清单并要求重新确认。路径保存成功后清空旧监控目录的内存 eligible 集合与候选堆，保留 SQLite 任务历史，直到新目录成功扫描后再判断是否存在。
   - 回归：预览排序/摘要变化、缺少确认、旧摘要、当前摘要和 HTTP/WebUI 流程均有定向测试；新增同步路径切换及本地目录迁移完成后旧候选不得领取的回归。
   - 验证：downloader 定向用例通过；`TestTaskEventPageReadsBoundedEventsFromQueueStore` 单测复跑通过。完整门禁结果见本版结尾及验证记录。未使用真实百度账号或 NAS/SMB。

NAS/SMB 实机测试仍单独记录，不计作本机源码回归。

2. **已完成候选事项 2/5：大队列影响预览分页。** 监控路径影响预览新增 offset/limit/has_more；API 每页最多返回 50 项，所有页面共享按完整稳定排序快照计算的 digest 和总数，极大 offset 会安全返回空末页，非法分页请求返回 400。WebUI 提供上/下页，页请求序号防止迟到响应覆盖更新后的预览；确认摘要仍代表完整快照，旧快照冲突后服务端返回最新第一页。
   - 回归：downloader 测试覆盖三页顺序、共享摘要和最大整数 offset；HTTP 测试覆盖三页、非法 offset/limit 及极大 offset；WebUI 用例覆盖翻到第二页只显示对应项、总数范围和请求参数。
   - 验证：路径相关 downloader 和 main HTTP 测试通过；WebUI 7 项定向测试和 `git diff --check` 通过。

3. **已完成候选事项 3/5：源缺失判断注明成功扫描的监控路径。** `task_source_missing` 事件证据新增 `monitored_queue_dir`，与现有 `scan_epoch` 和 `CloudObservedAtUTC` 同存，明确该结论只相对于当次监控目录；不伪造远端操作发生/响应时间。回归验证 `/queue` 路径证据及云端观测时间，事件不包含远端操作时间。
   - 验证：`TestReconcilePersistsScanRecoveryAndMissingSourceEvidence` 先因缺少路径证据失败，补字段后通过；全仓 Go 测试随后通过。

4. **已完成候选事项 4/5：队列监控目录分页发生边界漂移时失败关闭。** `ListDir` 读取超过 999 项的目录时保留相邻页 overlap 并完整比较边界 `FileInfo`；offset 期间边界插入/删除/身份变化会返回 `ErrDirectoryPageBoundaryMoved`，调用方不拿不完整列表运行 `reconcileObserved`。内部全量读取使用独立分页入口，不继承交互分页的最大 offset。稳定三页列表、漂移失败和大于 1,000,000 的内部 offset 回归通过。
5. **已完成候选事项 5/5：路径提交后明确显示任务等待成功扫描对账。** 影响确认文案说明受影响项保存后暂停调度，只有新目录成功扫描后才会恢复资格或标记源缺失；失败扫描不会据此判缺失。WebUI 回归覆盖确认与保存后的状态提示。

发布门禁：修复大目录内部 offset 上限后，重新运行全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 126/126、userscript 3/3 和 `scripts/package.ps1` 全部通过；Windows AMD64/Linux AMD64/Linux ARM64 构建完成。Windows 二进制 `--version` 为 `v0.8.30-preview.117`；Linux ELF machine 分别为 62/183。最终 ZIP 载荷哈希、源码快照及私有数据检查见 releases 同级 `v0.8.30-preview.117-validation.md`。本机/SQLite/mock 验证，不进行真实百度写操作或 NAS/SMB 验收。

## v0.8.30-preview.99（已交付：大文件重复内容扫描断点恢复，5 项）

- **源码发现**：重复文件扫描状态只在整文件成功后累计字节；当前文件下载遇到中断、暂停或进程重启后会从零读取，单个大文件期间页面也看不到文件内进度。
- **五项实现**：页面展示活动文件路径和文件内字节；SQLite 持久化当前云端身份、偏移和 SHA-256 二进制状态；暂停/重启后从已持久化 Range 偏移续算；网络失败检查点与下载流 `Retry-After` 共用持久化退避；严格复核内容长度/Range/线上身份，并在单事务提交 SHA、已完成进度和检查点清理。SQLite schema 23→24。
- **本地证据**：schema 23→24 缺列迁移并验证既有扫描状态/已验证 SHA 保留；SHA 检查点跨 Store 关闭重开且不进入 JSON；候选完成事务成功及检查点冲突回滚；2×16 MiB 本地 HTTP Range 中断续算结果与整文件 SHA 一致；下载 429 秒数/HTTP 日期 Retry-After；WebUI 当前文件路径、文件字节和累计字节展示测试。全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 83/83、userscript 3/3、`git diff --check` 均通过；Windows AMD64/Linux AMD64/Linux ARM64 构建完成，ZIP 12 项 SHA 清单全部匹配。
- **真实账号只读验收**：按既有授权将唯一真实 `.nas-downloader` 从 `.98` 移动到 `.99`；schema 24、SQLite integrity ok、health ok、OAuth ready/账号绑定、任务/下载/source_unknown 均为 0、缓存 complete（983 页/8,050 项），根目录 GET 成功，监控目录 `/NAS_Download_Queue_preview`。管理员会话已登出，`.99` 服务运行于 `127.0.0.1:18089`。
- **边界**：没有启动大规模真实账号哈希扫描、下载或网盘写操作；百度节点 Range 行为未验证；没有可见 SMB/NAS 映射，本轮只做本机审查，NAS/SMB 未验收。详细记录见 releases 同级 `v0.8.30-preview.99-validation.md`。

## v0.8.30-preview.98（已交付：只读恢复与重复内容哈希完整性，5/5）

- **范围**：尊重重复扫描 provider Retry-After 并持久化截止；全盘/局部/重复扫描等待按墙钟每最多 30 秒重新核对；直播 MD5 变化优先写入本地缓存并事务清除旧 SHA-256、拒绝旧候选并发回写；OAuth UserID 检查共用 Downloader pacer；只读 API 错误安全展示 Retry-After。
- **实现与验证**：五项代码及聚焦回归通过；全仓 Go 测试/vet、WebUI 83/83、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP 13 项/12 项哈希及 346 项源码快照核验通过。ZIP SHA-256 与详情见 releases 同级 `v0.8.30-preview.98-validation.md`。
- **真实账号只读验收**：按用户既有授权将唯一 `.nas-downloader` 从 `.97` 移至 `.98`；SQLite schema 23、integrity ok，health ok、OAuth ready/账号绑定、任务/下载/source_unknown 均为 0、缓存 complete，网盘根目录读取成功、`/NAS_Download_Queue_preview` 为 0 项。`.98` 服务运行于 `127.0.0.1:18089`。
- **边界**：限流仅用本地模拟验证；未执行云端写入或下载；NAS/SMB 未验收。

## v0.8.30-preview.97（已交付：只读请求限流与扫描恢复可靠性）

- **六项改动**：将共享 pacer 覆盖到全部 metadata GET（文件列表、文件元数据、下载链接、账号信息；文件内容流不经此 API pacer）；解析并遵从 429/5xx 响应中的 Retry-After 秒数或 HTTP 日期，最大等待 1 小时；全盘扫描和指定目录刷新分别持久化退避毫秒与 UTC 截止时间并在重启后等待；暂停后 Start/Resume 保留触发退避的错误原因，成功页发布后清除已恢复的错误与重试计划；页面展示本机计划 UTC 与剩余等待。schema 22→23，旧数据库已有队列、缓存与运行记录保留。
- **回归**：新增 GET pacer覆盖、Retry-After 解析/上下限、HTTP 错误回执、两种扫描跨 SQLite 重开计划、到期前不发请求、暂停后恢复时仍保留 LastError、取消等待、成功后错误证据清理及网页状态显示测试。发现节流影响并行下载测试的观测窗口后，调整测试下载体时长到 600ms、四次链接等待窗到 2s，以真实覆盖 250ms API 节流下的并发传输上限；定向回归通过。
- **验证**：全仓 `go test -count=1 ./...` 已通过；发布脚本继续负责 vet、WebUI/userscript、三平台构建与 ZIP 校验。最终包哈希和来源快照见 releases 同级 `v0.8.30-preview.97-validation.md`。
- **边界**：自动限流与重试故障路径只在本机/模拟服务验证，没有在真实账号人为触发 429。按既有授权完成真实账号 `.95`→`.97` 单目录移动、SQLite 22→23 完整性核验、health/根目录/监控目录/缓存状态只读检查；没有网盘写操作或下载。NAS/SMB、Linux/ARM64 实机仍未验收。
- **下一批路线**：待用户环境的真实运行/外部验收反馈后归并下一批，不预定 `.98`，达 5 项门槛再编号。

## v0.8.30-preview.96（已交付：本地路径迁移恢复可靠性）

- **五项改动**：迁移复制中断恢复（完整目标清理同迁移残留 `.part`；仅在 `.part` 哈希匹配源且目标为严格字节前缀时恢复，其他冲突保留，相关状态合并计一项）；Shutdown 等待迁移 runner 退出后再关闭 SQLite；删除隔离源目录前重新核验目标；隔离/清理阶段失败后按 SQLite 阶段证据安全重试；验证阶段关机取消后保留原活动阶段并在启动后原位续跑。
- **回归**：新增复制恢复/冲突保留、删除前复核/重试、设置恢复及 `verifying`/`source_retired`/`deleting_source` 取消续跑回归，均先观察失败后通过。兄弟文件并发测试启动检测窗由 300ms 调整为 2s，连续 20 次通过；该稳定性修正不计入五项。
- **验证**：全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 82/82、userscript 3/3；Windows AMD64、Linux AMD64、Linux ARM64 构建通过。Windows 二进制 `--version` 正确。
- **产物**：ZIP 13 项、12 项载荷 SHA-256 全部匹配；源码快照 342 项一致；发布目录无 `.nas-downloader`、账号凭据、实际 `.env`、下载数据或 staging 残留。详细哈希见 releases 同级 `v0.8.30-preview.96-validation.md`。
- **边界**：没有对真实百度账号执行远端写操作；NAS/SMB 迁移、占用和重启实机验收及 Linux/ARM64 实机运行仍待验证。
- **下一批路线**：拟审查“百度网盘只读请求限流与恢复可靠性”，当前 0/5；从可复现的用户结果和调用链确认独立事项，不预定 `.97`，目录缓存相关能力继续合并评估。
## v0.8.30-preview.95（网盘管理可靠性合并批次）

- **版本目标**：把入队记录、路径输入和网盘目录浏览中的独立可靠性问题合并为一个可验收的 preview，达到 5 项门槛后再编号。
- **事项 1：入队记录稳定分页**。服务端使用 `(created DESC, id DESC)` 稳定排序和有界 canonical base64url keyset cursor，保留旧 offset API；浏览器支持本次访问中的上一页游标栈。分页请求失败不提交游标，允许同页重试并正确返回。
- **事项 2：网盘路径输入保真**。手动入队逐行解析忽略空行但不裁剪路径；外部选择 handoff 保留精确路径；入队页和网盘管理页的目录输入也不 trim 有效路径，避免尾部空格被静默裁剪后误选另一对象。后端原有文件名约束仍拒绝本地无法安全表示的尾随空格/句点名称，因此不放宽下载目标校验。
- **事项 3：入队页目录读取失败时清理旧列表**。新目录请求开始或失败时清空旧项目和翻页状态，并保留请求路径供修正/重试。
- **事项 4：主网盘管理页忽略过期目录响应**。快速连续浏览时只应用当前请求结果；加载期间禁用翻页；目录大小结果按浏览代次和大小请求序号淘汰过期响应。
- **事项 5：本地迁移失败恢复指引**。显示原始迁移错误，并说明文件占用时关闭正在使用目录的程序，保持源/目标路径、重新勾选迁移后保存以重试；不自动重试。
- **验证**：新回归先复现后通过；全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 82/82、userscript 3/3、gofmt 和 `git diff --check` 通过。发布脚本再执行回归并构建 Windows AMD64、Linux AMD64、Linux ARM64；版本、ZIP 及载荷清单核验见 releases 同级 `v0.8.30-preview.95-validation.md`。
- **发布边界**：此批次只做本地/mock/UI 自动化与跨平台构建，没有启动真实账号、移动或删除远端项目，也未在 NAS/SMB 上做迁移验收。
- **真实账号只读启动验收（2026-10-30，`.95` 发布后）**：按用户此前授权将唯一真实 `.nas-downloader` 从 `.94` 移至 `.95`，未复制；17 个文件移动前后相对路径、长度和 SHA-256 一致，`.94` 源目录已消失。`.95` health `ok`、OAuth `ready`/账号绑定，任务、下载和 source_unknown 均为 0；网页登录后根目录只读分页成功，`/NAS_Download_Queue_preview` 为 0 项，全盘缓存 `complete`。对 `/AI量化交易第1期` 的真实账号 API 预检返回 1 个 `ready` 项目及目标 `/NAS_Download_Queue_preview/AI量化交易第1期`；预检前后任务数均为 0，没有确认入队或移动。验证用管理员会话已登出；`.95` 服务当前仍运行于 `127.0.0.1:18089`，供用户查看。未执行其他远端写操作、下载、重复扫描或 NAS/SMB 验收。

## v0.8.30-preview.94（已交付：目录刷新明细工作台）

- **版本目标**：在 `.93` 的刷新明细上提供五项独立能力：变化类型筛选、路径/名称/fs_id 搜索、未筛选完整分类统计、单目录筛选 JSON 导出、复制旧/新路径与 fs_id。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-refresh-detail-workbench.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-refresh-detail-workbench.md)。
- **实现边界**：Store 在账号/run/event 范围内执行服务端筛选和稳定分页；搜索最多 128 Unicode 码点；统计不受筛选影响；同一只读 SQLite 快照最多导出 5,000 条，超限整份拒绝；剪贴板失败明确反馈。不新增 schema，不发起远端请求。
- **回归验证**：全仓 `go test -count=1 ./...` 和 `go vet ./...` 通过；WebUI 73/73、userscript 3/3、`git diff --check` 通过。打包脚本再次运行上述回归并通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建成功；ZIP 13 项、12/12 payload 哈希通过、私有数据 0 项；Windows 二进制报告版本 `.94`。最终 ZIP SHA-256 与 source snapshot 摘要见 releases 同级验证记录。
- **真实账号冒烟（打包后）**：按用户既有授权将唯一含百度凭据的 `.nas-downloader` 从 `.77` 移动到 `.94`，未复制；启动时 SQLite 成功升级，`integrity_check=ok`、下载任务 0。账号可用；完整缓存读回 8,050 项/976 个完整目录。空监控目录刷新 1 页/0 项完成，并保存本机/百度 UTC 证据。随后在根目录创建唯一临时空测试目录及一个子目录，刷新完成；两次创建、两次删除的远端回执均为 `confirmed`。产品在确认 mkdir 后立即同步缓存，因此刷新没有新增 delta；筛选与空 JSON 导出返回 0 条且不含账号指纹。清理后测试路径无缓存残留，本地队列仍为 0；数据库共 23 条 confirmed 操作。停止后仅 `.94` 持有凭据数据，`.77` 无副本。
- **手动添加页面复测（2026-10-30）**：真实登录浏览器打开 `.94` `/intake`，账号显示可用、根目录列表读取 51 项；选中 `/AI量化交易第1期` 后点击“预检这些项目”，页面返回 1 个“待确认”项目和正确监控目标 `/NAS_Download_Queue_preview/AI量化交易第1期`。未勾选提交；下载任务保持 0，未移动或下载。当前 `.94` 页面链路未复现旧版 `.24` 的请求失败。服务已停止。
- **验收边界**：真实账号覆盖账户启动、缓存读取、空/一项目录刷新和受控空目录 CRUD；非空逐项变化筛选/复制场景由 SQLite/mock 与 WebUI 自动化覆盖，因为应用会在确认 mkdir 后即时同步缓存。NAS/SMB 与外部浏览器未验收。

## v0.8.30-preview.92（已交付：目录刷新变更摘要）

- **版本目标**：让用户从单轮刷新历史看出每个变化目录的直系子项新增、移除和元数据变化数量，同时保留本机事件时间与百度列表观测时间的区别。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-refresh-delta-summary.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-refresh-delta-summary.md)。
- **约束**：只有刷新前目录缓存为 `complete` 才计算三类数量；第一次观测或不完整基线保持未知。计数随目录发布、库存和队列状态在同一 SQLite 事务提交；不记录逐文件历史副本，不访问百度网盘。
- **实现**：Store 已按 `fs_id` 分类， schema 20→21 原子迁移与 manifest 恢复已实现；事件 API/JSON 证据导出复用新增可空计数；刷新历史页面区分直系子项数量与无完整基线。
- **回归验证**：目录/迁移/API 定向用例、全量 taskstore 与全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 68/68、userscript 3/3、`git diff --check` 均通过；测试覆盖完整/缺失基线、增删/元数据变化、零变化、迁移恢复及事件插入失败时缓存与 checkpoint 原子回滚。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 载荷哈希匹配、私有运行数据路径 0 项；最终 SHA-256 与源码快照摘要记录在 releases 同级验证文件中。
- **验收边界**：本机 SQLite/mock 与页面测试；未访问真实百度账号，也未验证 NAS/SMB 或外部浏览器。

## v0.8.30-preview.93（已交付：目录刷新变更明细）

- **版本目标**：在 `.92` 目录级增删/变化数量基础上，持久记录每个完整发布目录中的具体新增、删除、同身份元数据变化和同路径身份替换对象，提供前后缓存快照与本机/云端 UTC 证据。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-refresh-change-details.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-refresh-change-details.md)。
- **范围**：schema 21→22，新 append-only 明细表；只对完整旧目录基线分类，明细与目录缓存、checkpoint、父事件同事务写入；网页按需分页浏览。类型筛选用于聚焦目录发布事件；完整运行证据导出保持原边界，不隐式加载明细。
- **验收**：身份变化仅在唯一同路径匹配时配对；缓存 SHA-256 仅在身份/大小/mtime 匹配时展示；事件或任一明细插入失败全部回滚；覆盖迁移恢复、账号隔离、UI 转义/迟到响应、全仓回归和三平台测试包。
- **实现与回归**：Store 明细分类/分页、21→22 原子迁移与旧 manifest 恢复、认证 API 和按需 WebUI 已完成。定向回归及全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 70/70、userscript 3/3、`git diff --check` 通过；覆盖唯一/歧义身份匹配、完整/未知基线、账号隔离、快照与 SHA 证据及失败事务回滚。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 载荷哈希匹配、源码快照和私有运行数据路径检查通过；SHA-256 `6F3180F5E02E8553102877B02BD448E917E7DDEAAF6FC85CF74C61948552ACD6`，详见 releases 同级 `v0.8.30-preview.93-validation.md`。
- **验收边界**：本机 SQLite/mock 与页面自动化；未访问真实百度账号或 NAS/SMB。未做外部浏览器和目标 NAS 验收。

## v0.8.30-preview.91（已交付：导出筛选后的刷新运行摘要）

- **版本目标**：将当前状态/UTC 日期筛选命中的目录缓存刷新运行摘要导出为单份有界 JSON，便于离线盘点。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-cache-refresh-filtered-summary-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-cache-refresh-filtered-summary-export.md)。
- **约束**：复用 `.90` 筛选；同一 SQLite 只读事务，最多 5,000 条，超限拒绝整份；仅导出摘要，不含事件/账号指纹，不访问网盘。
- **实现**：新增账号绑定的只读摘要导出 API；支持状态/日期筛选，拒绝未知/重复参数，单 SQLite 快照最多 5,000 条，超限整份拒绝；WebUI 显式下载并忽略筛选变化后的迟到响应。

## v0.8.30-preview.90（已交付：刷新运行历史状态与 UTC 日期筛选）

- **版本目标**：在本地刷新运行历史中按当前状态和启动 UTC 日期范围查找运行；继续使用启动事件序号稳定分页。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-cache-refresh-history-filters.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-cache-refresh-history-filters.md)。
- **范围**：不发起百度请求，不改扫描状态，不新增 SQLite schema；筛选只作用于运行列表，证据导出和事件历史继续按选定 run ID 查询。
- **实现**：Store 在 SQLite 侧按最新运行状态与启动 UTC 日期范围筛选；API 校验状态、规范日期、重复/未知参数和日期顺序；网页分页持续携带筛选，调整/清除筛选回到第一页。
- **回归验证**：Store 状态/日期组合、UTC 日边界、不可变启动序号分页和账号隔离；API 状态/日期/重复/未知参数验证；WebUI 改筛选重置分页、旧页保留筛选、清除恢复全量；全仓 `go test ./...`、`go vet ./...`、WebUI 66/66、userscript 3/3、`git diff --check` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 载荷校验、SOURCE-SNAPSHOT 与私有运行数据路径检查见 releases 同级 `v0.8.30-preview.90-validation.md`。
- **验收边界**：仅本机 SQLite/mock 与页面自动化，不访问真实账号或 NAS/SMB。

## v0.8.30-preview.89（已交付：刷新运行证据导出）

- **版本目标**：为选定的目录缓存刷新运行导出完整、有界、可离线保存的 JSON 证据；摘要与事件来自同一 SQLite 只读快照。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-cache-refresh-evidence-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-cache-refresh-evidence-export.md)。
- **限制**：最多 100 条事件，超限拒绝整份；账号指纹不导出，敏感 evidence 脱敏；不访问网盘。
- **回归验证**：Store 同快照读取/账号隔离/100 条边界/缺失运行、Downloader 认证账号绑定、API GET/ID/隐私/超限与未授权路由、WebUI 点击下载和过期响应保护均通过；全仓 `go test ./...`、`go vet ./...`、WebUI 65/65、userscript 3/3、`git diff --check` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；最终 ZIP 13 项、12 项载荷校验、SOURCE-SNAPSHOT 与私有运行数据检查见 releases 同级 `v0.8.30-preview.89-validation.md`。
- **验收边界**：本地 SQLite/mock；没有访问真实百度账号或 NAS/SMB。

## v0.8.30-preview.88（已交付：目录缓存刷新运行历史）

- **版本目标**：让用户从网页发现 SQLite 中历次目录缓存刷新运行，并查看选定运行的关键事件；运行列表使用不可变启动序号稳定分页。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-cache-refresh-run-history.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-cache-refresh-run-history.md)。
- **实现**：schema 19→20 增加运行列表索引；Store 按认证账号查询每轮启动信息及最新事件摘要；新增受保护只读 API；网盘管理页按需加载运行记录并复用事件页，两个分页游标独立。
- **回归验证**：Store/API/downloader 定向用例、迁移 19→20 与 manifest 恢复、账号隔离、跨重开稳定游标、WebUI 63/63、userscript 3/3、全仓 `go test ./...`、`go vet ./...`、`git diff --check` 通过。全量运行首次发现现有 HTTP/WebSocket 集成测试仅有 1 秒关闭期限，在全仓负载下超时；扩大该测试清理期限到 10 秒后，定向与全仓测试通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 载荷校验、私有运行数据路径 0 项。最终 ZIP、源码快照与哈希核验见 releases 同级 `v0.8.30-preview.88-validation.md`。
- **验收边界**：本地 SQLite/mock；不访问真实百度账号或 NAS/SMB。

## v0.8.30-preview.87（已交付：目录缓存刷新可审计历史）

- **版本目标**：刷新跨进程暂停、恢复或失败后，网页仍可查到每次运行关键事件与逐目录完整发布顺序，区分本机事件 UTC 和百度目录列表读取完成 UTC。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-directory-cache-refresh-history.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-directory-cache-refresh-history.md)。
- **实现**：SQLite schema 18→19 新增账号/run 隔离、仅追加的事件表；状态变化和逐目录发布与事件同事务；认证只读 API 按序号游标分页；管理页用户点击后读取本机时间线，转义路径并忽略过期响应。
- **回归验证**：迁移与落后 manifest 恢复、启动/暂停/恢复/目录发布事件写入失败回滚、`running` 检查点跨重开后显式恢复留痕、事件跨重开分页、账号/run 隔离、开始/暂停/恢复/失败/完成和目录观测证据、API 参数与隔离通过；全仓 `go test ./...`、`go vet ./...`、WebUI 63/63、userscript 3/3、`git diff --check` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 载荷校验、私有运行数据路径 0 项。最终 ZIP、来源快照和其余核验见 releases 同级 `v0.8.30-preview.87-validation.md`。
- **验收边界**：本地 SQLite、模拟网盘和页面自动化；未访问真实账号，也未验证 NAS/SMB 或外部浏览器环境。

## v0.8.30-preview.86（已交付：目录缓存增量核对与在线冲突修正）

- **用户方向**：利用本地目录清单和已保存文件 hash 计算重复内容；刷新某目录/子目录时，仅发布完整且与旧缓存核对后的变化，旧快照保持可用；远端操作与缓存冲突时以线上确认结果为准，单独更新缓存并留时间证据。
- **源码现状**：重复组来自 `cloud_inventory` 与 `cloud_content_hashes`；局部刷新目前逐页改正式 inventory，启动时标记范围 stale；已确认远端操作若本地身份匹配失败则只失效并提示，不做独立补偿更新。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-incremental-cloud-cache-reconciliation.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-incremental-cloud-cache-reconciliation.md)。
- **实现**：schema 17→18 新增账号/run/目录隔离的持久刷新暂存表；分页只写暂存，目录完整后以一个事务发布。刷新中保留正式目录、size 和重复组快照；完全不变时复用文件 hash 且 generation 不变；变化时清理已消失文件/子树 hash，仅失效身份或内容字段变化的文件 hash。已确认远端操作与本地身份冲突时，仅凭同账号 confirmed 操作记录和精确对象/源目标路径将线上核验结果修正到缓存，旧/新身份和 UTC 时间写入追加事件；待刷新父目录标为 stale。unknown 结果沿用隔离策略。
- **回归验证**：覆盖 v17→v18 与落后 manifest 恢复、分页暂存跨重开、刷新期间完整列表与 duplicate groups 可读、同目录不变不增加 generation/不丢 hash、MD5 变化只失效对应 hash、远端确认删除/移动身份漂移时修复缓存及事件证据；WebUI 61/61、userscript 3/3、全仓 `go test ./...`、`go vet ./...`、`git diff --check` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 载荷校验、私有运行数据 0 项。最终 ZIP 和源码快照哈希见 releases 同级 `v0.8.30-preview.86-validation.md`。
- **验收边界**：完全本地/mock；没有对真实账号刷新、扫描、移动或删除。NAS/SMB 和目标浏览器仍需用户环境验收。

## v0.8.30-preview.85（已交付：重复扫描文件/字节级持久进度）

- **问题证据**：扫描状态只持久显示文件数；启动前的读取量估算不反映运行期间已完整校验/剩余数据量，续跑也未按当前游标之后的候选字节核准总量。
- **计划范围**：SQLite 持久化 total/processed bytes；仅完整校验并保存 SHA-256 后计入；恢复时以累计完成量加当前游标之后的候选重新核准总量；页面展示文件和字节进度。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-duplicate-scan-byte-progress.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-duplicate-scan-byte-progress.md)。
- **实现**：SQLite schema 16→17 原子迁移增加 total/processed bytes；已校验并持久化 SHA-256 的文件才累计字节，恢复时根据游标之后的当前候选重新核准总文件与总字节；扫描状态和详情显示两种进度。迁移只从身份仍匹配的已验证内容哈希回填可证明的历史字节，其余保持 0。
- **回归验证**：覆盖迁移回填与重开恢复、重复迁移列存在时的幂等性、扫描续跑重算总量、逐项失败重试不重复计数、零字节展示；WebUI 61/61、userscript 3/3、`go test ./...`、`go vet ./...`、`git diff --check` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项，载荷清单 12/12，未包含私有运行数据。最终 ZIP 哈希、源码快照及记录见 releases 同级 `v0.8.30-preview.85-validation.md`。
- **验收边界**：未对真实百度账号执行大扫描或远端删除；NAS/SMB 和目标浏览器仍需用户环境验收。

## v0.8.30-preview.84（已交付：本地缓存搜索结果导航）

- **版本目标**：用户从全盘缓存搜索定位条目后，可继续在本地缓存浏览其父目录，不因查找而触发百度实时读取。
- **范围**：搜索结果分别提供缓存内和实时定位；缓存入口清空搜索词并保持缓存来源，从父目录第 0 页读取，目录结果另可直接打开自身；实时入口维持原有行为。覆盖状态和云端观测时间继续按目标目录的缓存元数据展示。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-cache-search-navigation.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-cache-search-navigation.md)。
- **回归验证**：WebUI 覆盖缓存/实时两条路径、缓存来源隔离、旧缓存警告和只读动作；完整 WebUI 60/60、userscript 3/3、全仓 `go test ./...` 与 `go vet ./...` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 打包通过；最终 ZIP 哈希、源码快照与清单核验见 releases 同级 `v0.8.30-preview.84-validation.md`。
- **验收边界**：本地 UI 与打包验证，不访问真实百度账号、不执行远端写入；NAS/SMB 和目标浏览器仍待环境验收。

## v0.8.30-preview.83（已交付：远端操作历史稳定游标分页）

- **版本目标**：历史列表翻页期间，即使顶部新增操作，也不让 offset 位移造成旧记录重复或跳过。
- **范围**：SQLite 使用 `(created_at_utc, operation_id)` 降序游标分页；API 默认使用游标、保留显式 offset 兼容；网页为已访问页面保存游标以支持上一页/下一页。总数每页实时统计，不承诺跨页快照；状态筛选下记录状态变化仍按实时结果反映。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-history-stable-pagination.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-history-stable-pagination.md)。
- **实现**：Store/API/downloader/UI 使用 `(created_at_utc, operation_id)` 游标续页；旧 offset 调用保持兼容。页码前后使用当前会话的游标栈，筛选/刷新重置；总数标记为实时匹配总数。
- **回归验证**：Store 覆盖同时间戳 ID 排序、头插不移动旧游标、过滤、尾页和非法游标；API 覆盖下一游标、部分/重复/无效游标、与 offset 冲突及旧 offset；UI 覆盖下一页、上一页、筛选重置、顶部插入和实时总数。定向 Go、全仓 Go 测试、vet、WebUI 60/60、userscript 3/3、`git diff --check` 通过。
- **测试稳定性修复**：首次全仓测试暴露原退避恢复回归只留 400ms future deadline，在 SQLite 重开/整包负载下可能于启动前过期。将测试窗口延长到 5 秒并把完成时限增至 20 秒；该测试连续 3 次及后续全仓测试均通过，不改产品退避策略。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 三平台构建与 ZIP 载荷/来源快照/私有数据检查通过；最终 SHA-256 与验收记录见 releases 同级 `v0.8.30-preview.83-validation.md`。
- **验收边界**：只读本地历史，不访问百度 API、不改变网盘/队列状态；未做真实浏览器与 NAS/SMB 环境验收。

## v0.8.30-preview.81（已交付：重复文件预览服务端分页）

- **版本目标**：修复 `.80` 遗留的完整重复组列表一次性传输和常驻浏览器内存问题；普通浏览每次最多加载 25 组，并继续支持跨页保留项选择和全量批量删除。
- **实现范围**：SQLite 同一只读事务返回有界页、总组数和总待删除项数；API 拒绝越界/重复分页参数；批量删除逐组从第一页重读，避免组消失后 offset 跳页；每个文件仍经既有身份和 SHA-256 复核，unknown/失败立即停止。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-cloud-duplicate-preview-server-pagination.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-cloud-duplicate-preview-server-pagination.md)。
- **回归验证**：Store/API 覆盖稳定分页、总组/待删除数、边界、账户隔离；UI 覆盖分页、跨页 keeper、过期 keeper 丢弃、删除后从首页续读、unknown/剩余计数变化停止。WebUI 56/56、userscript 3/3、`go test ./...`、`go vet ./...`、`git diff --check` 通过。
- **发布验证**：Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、载荷校验 12/12、无私有运行数据。最终包哈希和详细记录见 `nas_baidupan_auto_downloader-releases/v0.8.30-preview.81-validation.md`。
- **验收边界**：没有对真实账号执行删除；真实浏览器和 NAS/SMB 验收未覆盖。

## v0.8.30-preview.82（已交付：重复扫描事件历史）

- **版本目标**：让用户查看 SQLite 已持久化的重复扫描状态转换时间线，便于长时间扫描、退避、暂停、失败和重启后回顾进度。
- **范围**：账号隔离的只读 API；最新事件分页及加载更早记录；页面显式按需读取、转义证据、显示 UTC 时间。不增加扫描频率、逐文件事件或远端操作。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-duplicate-scan-event-history.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-duplicate-scan-event-history.md)。
- **实现**：复用 append-only SQLite 扫描事件；按账号读取最新事件、通过 `before_sequence` 加载更早项；管理认证 GET 对证据脱敏。网页折叠区按点击加载，以 UTC 时间顺序显示；不增加自动请求、扫描或百度网盘操作。
- **回归验证**：覆盖 newest-first 分页、账号隔离、游标、参数边界、证据脱敏、按需读取、历史前置追加、空态/重试和迟到响应；WebUI 59/59、userscript 3/3、全仓 Go 测试/vet、`git diff --check` 通过。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建与 ZIP 清单/来源快照/私有数据检查通过；最终哈希和详情见 `nas_baidupan_auto_downloader-releases/v0.8.30-preview.82-validation.md`。
- **验收边界**：未新增数据库迁移；没有对真实账号执行写操作，NAS/SMB 目标环境验收仍待用户环境。

## v0.8.30-preview.80（已交付：重复文件预览分页）

- **版本目标**：将重复文件预览限制为每页渲染 25 组，降低大批量结果的页面 DOM 数量，便于逐页核对。
- **实现**：完整预览数组仍保留在页面内存中；上一页/下一页不发 API；`keep/delete` 选择可跨页保留；批量删除数和操作范围仍是全部预览组；刷新预览回第一页并恢复默认保留项。
- **回归验证**：覆盖 0、25、26、51 组、边界页范围、跨页保留选择、全量删除计数、绝对组索引和翻页无网络请求；WebUI 54/54、userscript 3/3、全仓 `go test ./...`、`go vet ./...` 通过。
- **性能边界**：本功能仅限制渲染的 DOM 数量；API 仍一次返回全部预览组，页面内存仍保留完整预览，未实施服务端分页。
- **发布验证**：Windows AMD64、Linux AMD64、Linux ARM64 构建及 ZIP 源码快照、载荷哈希、包版本、私有运行数据排除通过；最终哈希和完整记录见 releases 目录外部验证文件。
- **验收边界**：未访问真实百度账号或执行网盘删除；只证明页面 DOM 渲染上限，不证明全量响应或内存有界。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-cloud-duplicate-preview-pagination.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-cloud-duplicate-preview-pagination.md)。

## v0.8.30-preview.79（已交付：重复文件预览选择保留副本）

- **版本目标**：用户可在删除前指定每个重复组中希望保留的副本，避免只能接受路径排序默认值。
- **实现**：新增页面内临时选择，只重排当前已验证组中的 `keep/delete`；刷新预览恢复服务端默认。选择不触发 API；单项/批量删除继续单独确认并逐项复核身份和 SHA-256。
- **回归验证**：定向测试覆盖无请求切换、切换后删除清单、刷新重置、路径转义和批量删除 SHA-256 载荷；WebUI 53/53、userscript 3/3、全仓 `go test ./...`、`go vet ./...`、三平台构建通过。
- **发布验证**：ZIP 载荷、源码快照、包内版本和私有数据排除由发布脚本验证；最终 ZIP SHA-256 及载荷计数见 releases 目录外部验证文件。
- **验收边界**：未访问真实百度账号、未执行真实文件删除、未做真实浏览器验收。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-cloud-duplicate-keeper-selection.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-cloud-duplicate-keeper-selection.md)。

## v0.8.30-preview.78（已交付：网盘移动目标目录选择器）

- **版本目标**：将网盘移动操作从手动输入绝对路径改为可浏览的目录选择器，降低目标路径误输概率。
- **实现**：选择器默认从当前实时目录开始，使用现有目录列表 GET 分页浏览、进入子目录和返回上级；只有用户明确点击“移动到此目录”才调用现有远端移动接口。列表请求失败时不能确认，过期响应被忽略，取消/关闭不发写请求；服务端身份复核、队列暂停、未知结果隔离和缓存更新策略保持原样。没有新增 API。
- **回归验证**：目录读取、路径导航、HTML 转义、分页、请求失败、过期响应和取消无写请求由 WebUI 脚本覆盖；WebUI 51/51、userscript 3/3、全仓 `go test ./...`、`go vet ./...`、`git diff --check` 通过。
- **验收边界**：本版本通过模拟 API 的脚本测试，未在真实浏览器做交互验收，也未部署到 `.77` 真实账号运行目录；没有执行百度网盘写操作。
- **发布验证**：Windows AMD64 版本输出正确，Linux AMD64/ARM64 的 Go 构建信息架构匹配；ZIP 含 13 个条目，12/12 载荷 SHA256 匹配，无 `.nas-downloader`、管理员/百度密钥、实际 `.env` 或下载数据。最终 ZIP SHA-256 记录在 releases 目录的外部验证文件中。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-cloud-move-directory-picker.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-cloud-move-directory-picker.md)。

## v0.8.30-preview.63（已交付：本地目录迁移路径别名隔离）

- **版本目标**：防止通过 Windows junction/符号链接父目录绕过迁移源、目标和程序 SQLite 状态目录的路径重叠检查。
- **实现**：增加现存父目录物理解析后再追加缺失路径分量的校验；迁移前、目标创建后分别检查源/目标/状态目录边界；普通网页路径设置也校验物理路径不与状态目录重叠。
- **回归验证**：Windows NTFS junction 测试分别覆盖目标别名进入迁移源和 SQLite 状态目录；验证迁移请求被拒绝且不会在受保护路径下创建目标，也验证非迁移路径设置拒绝状态目录别名。`go test ./internal/downloader -run 'Test(LocalDirectoryMigration|ManagedPaths|ValidateLocalMigrationPathsRejectsResolvedSourceAndStateOverlap)' -count=1` 通过。
- **发布门禁**：`scripts/package.ps1 -Version v0.8.30-preview.63 -AllowDirty -UpdatePreview` 通过；WebUI 32/32、userscript 3/3、全仓 `go test ./...`、`go vet ./...` 通过，Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项，SHA256SUMS 12/12 匹配，源码快照 274 文件，无 `.nas-downloader`、`admin-key.json`、`baidu-app.json`、`.env` 或下载目录。最终 ZIP SHA-256 `3AB6674595D60B98F40C42A9248717E932402B47EB122C7C6E84665319CDC4BC`。
- **真实运行状态更新（2026-10-30）**：更新前 `.62` health `ok`、OAuth `ready=true`、任务 0；按用户此前授权停止 `.62`，将唯一 `.nas-downloader` 移到 `.63`（未复制）。`.63` health `ok`、OAuth `ready=true`、任务 0；`.62` 原路径不存在该目录。此为本地服务只读启动验证，不代表 NAS/SMB 验收。未执行网盘写操作、下载、重复扫描或清理。
- **已知验收边界**：当前主机没有 SMB 映射；NTFS junction 回归不能替代 NAS/SMB 的占用重试和中断重启恢复测试。后续 `.64` 先累积目标 NAS/SMB 实测反馈，达到版本门槛后发布。

## v0.8.30-preview.64（已交付：只读错误诊断与历史错误本地化）

- **只读 API 错误分类改进**：目录列表、缓存列表/大小、空目录候选、重复扫描状态/候选、全盘及局部缓存扫描状态等只读 GET 失败时统一显示可安全重试的提示；无效分页返回 400。POST 网盘变更仍保留未知结果警告。API 回归覆盖 8 个 GET、无效分页和写入失败边界。
- **远端操作历史错误本地化**：操作历史列表、摘要导出、事件详情及单条证据导出在 SQLite/证据解析失败时原先返回英文错误，页面原样显示。改为中文可重试提示，仍返回 HTTP 503，不把底层错误字符串（可能带凭据）写入响应。四个 API 的回归先因英文错误失败，修改后通过。
- **发布验证**：打包脚本通过 WebUI 32/32、userscript 3/3、全仓 `go test ./...`、`go vet ./...` 和 Windows AMD64/Linux AMD64/Linux ARM64 构建；ZIP 13 项、SHA256SUMS 12/12，未包含 `.nas-downloader`、`admin-key.json`、`baidu-app.json`、`.env` 或下载文件。包内 Windows `-version` 输出正确。最终 ZIP SHA-256 以发布目录验证记录为准。
- **手动入队预检复测（2026-10-30）**：使用真实账号，对网盘根目录和 `/NAS_Download_Queue_preview` 执行受保护列表 GET，均返回 HTTP 200；监控目录当前 0 项。对用户此前提及的 `/AI量化交易第1期` 调用页面同用的 `POST /api/intakes/preview`，返回 `state=preview`、1 个项目、目标 `/NAS_Download_Queue_preview`。预检记录两分钟后过期，任务队列始终为 0；未确认提交、移动或下载。此次 API 复测没有复现此前“请求失败”，但不是登录后的真实浏览器点击验收。
- **剩余验收**：目标 NAS/SMB 目录迁移、占用重试、中断重启恢复和真实浏览器操作仍需用户目标环境验证；需记录文件系统/协议及迁移阶段 UTC 证据。未用本地盘或 API 预检代替这些验收。

## v0.8.30-preview.65（已交付：远端操作历史按类型筛选）

- **范围**：历史页新增新建目录、移动、重命名、删除筛选；列表分页与摘要 JSON 导出共用筛选条件。仅查当前 SQLite，不访问百度 API；不新增写操作。
- **实现与回归**：Store 按 `operation_kind` 参数化过滤并拒绝非法值；API 校验四种类型；页面选择类型后重新读取，清空筛选会移除 `kind` 参数。Store/API/UI 回归均已通过，WebUI 32/32。TDD 曾捕获 SQL 列名应为 `operation_kind`，修正后通过。
- **发布验证**：这是一个完整用户可见功能。全仓 Go 测试、vet、WebUI 32/32、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 和 ZIP 检查均通过；详细校验值见 releases 目录 `v0.8.30-preview.65-validation.md`。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-kind-filter.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-kind-filter.md)。

## v0.8.30-preview.66（已交付：本地目录迁移证据导出）

- **范围**：从 SQLite 同一只读快照导出最近一次本地目录迁移摘要和有界追加事件，包含源/目标路径、阶段、文件/字节进度及 UTC 时间；不读写迁移目录或网盘。
- **目的**：将现有页面只显示“当前进度”的信息用于故障记录，便于用户整理 NAS/SMB 迁移过程证据。
- **实现与回归**：Store 同一只读事务读取摘要与升序事件；最多 100 条，超限拒绝整份。管理员 GET 返回 JSON 附件；无记录 404，内部错误不透出，事件敏感字段递归掩码。页面仅在点击后下载。Store/API/WebUI 回归通过，含 SQLite 关闭重开后读取、无记录、101 条超限、事件 UTC、访问令牌遮蔽及无百度/写操作调用；WebUI 33/33。
- **发布验证**：全仓 Go 测试、vet、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP 校验通过；详细信息与 SHA-256 见 releases 目录 `v0.8.30-preview.66-validation.md`。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-migration-evidence-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-migration-evidence-export.md)。

## v0.8.30-preview.67（已交付：历史迁移记录浏览）

- **版本目标**：在网页分页浏览本地 SQLite 中所有历史目录迁移摘要，选择任一条记录导出其完整证据，补足 `.66` 只导出最近一次迁移的限制。
- **实现**：新增稳定排序（开始时间倒序、迁移 ID 倒序）的 SQLite 有界只读分页；摘要与事件导出按指定 ID 在同一 SQLite 快照中读取。API 限制每页最多 50 项，拒绝无效分页；未知 ID 404，超出 100 条事件则拒绝整份导出。页面支持手动刷新、分页、逐条导出，并忽略过期列表响应；所有路径、错误和证据内容按现有策略转义/脱敏，不触碰目录或百度 API。
- **回归**：Store 覆盖同时间戳稳定排序、分页、无记录、重启读回、列表后新增更晚迁移仍导出所选旧记录、未知 ID 和非法页数；API 覆盖页元数据、分页错误、精确 ID、404、脱敏与 no-store；WebUI 覆盖分页、旧响应忽略、路径转义和逐条导出。
- **边界**：未访问百度 API，不读写本地迁移目录；目标 NAS/SMB 迁移与重启恢复仍待实际环境验收。
- **发布验证**：`scripts/package.ps1 -Version v0.8.30-preview.67 -AllowDirty` 全部通过；全仓 Go 测试、vet、WebUI 34/34、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项、清单 12/12 匹配、私有运行数据 0 项；包内 Windows 版本核对通过。ZIP SHA-256 `BF68FDC63DBD819CBD9207742C7339CFB74D074A154D6E5DB0457D8A105DBEDD`；详细记录见 releases 目录 `v0.8.30-preview.67-validation.md`。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-migration-history.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-migration-history.md)。

## v0.8.30-preview.68（已交付：本地迁移历史检索）

- **候选目标**：按迁移状态及源/目标路径关键词检索 SQLite 历史，帮助从多次迁移中定位记录；沿用稳定分页和按 ID 导出。
- **实现进度**：Store 已支持只读同快照过滤，合法状态为 running/failed/completed；路径关键字按字面匹配并转义 `%`、`_`、反斜线，UTF-8 长度最多 256 字节。API 已校验筛选参数；页面提交筛选后回第一页，翻页/刷新保持筛选、清除后重置，迟到响应不覆盖当前结果。
- **发布验证**：Store 全测试、API 定向测试、WebUI 35/35、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP 检查通过；清单 12/12 匹配，私有运行数据 0 项。SHA-256 `E8C918755547BB9EA5B895DC18A4E5D11AA650078051E8C5A5A800D5DA175090`，详细记录见 releases 目录 `v0.8.30-preview.68-validation.md`。
- **边界**：SQLite-only；不访问百度 API 或迁移目录，不改变历史，不添加恢复/重试入口。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-migration-history-search.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-migration-history-search.md)。

## v0.8.30-preview.69（已交付：筛选迁移摘要导出）

- **候选目标**：对当前状态/路径筛选结果一次导出迁移摘要 JSON，不含逐项事件；保留按 ID 导出完整事件的功能。
- **实现进度**：Store 复用 `.68` 筛选条件，在单个只读 SQLite 快照导出稳定排序结果；最多 5,000 条，第 5,001 条拒绝整份。API 复用参数校验并返回 no-store JSON 附件；页面新增仅点击后触发、使用当前筛选控件的导出按钮。
- **发布验证**：全 Store 测试、全 API 包、WebUI 36/36、userscript 3/3、全仓 Go/vet、三平台构建和 ZIP 检查通过；清单 12/12 匹配，私有运行数据 0 项。SHA-256 `27470DF7A12E7DD74854A33BE571FE73A1C1F91DECCE5CE4F4E647B9623A5BD1`，详细记录见 releases 目录 `v0.8.30-preview.69-validation.md`。
- **边界**：不包含追加事件，不访问百度 API 或本地迁移目录，不修改或恢复历史。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-migration-history-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-migration-history-export.md)。

## v0.8.30-preview.70（已交付：本地迁移事件时间线）

- **版本目标与实现**：在历史记录行内按需查看该条迁移的 UTC 阶段事件，按 sequence 游标读取更早事件；API 严格限定迁移 ID，证据递归脱敏，页面转义内容并忽略切换记录后的迟到响应。
- **回归验证**：Store 覆盖稳定序列分页、记录 ID 隔离、空事件、未知 ID 和边界；API 覆盖 UTC、脱敏、no-store、输入错误和通用失败；WebUI 验证按需加载、游标、HTML 转义和迟到响应。定向 Store/API 测试通过。
- **发布验证**：`scripts/package.ps1 -Version v0.8.30-preview.70 -AllowDirty` 通过；WebUI 37/37、userscript 3/3、全仓 Go 测试、`go vet ./...` 与 Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项，SHA256SUMS 12/12，私有运行数据 0 项；包内 Windows 版本正确。ZIP SHA-256 `60281EAB0B23CAE8A02429D0273C3F08ED93C65989923035282FF31EF8F247C9`；详细记录见 releases 目录 `v0.8.30-preview.70-validation.md`。
- **边界**：仅生成独立包，没有启动服务、部署到用户运行目录或访问真实百度/NAS；只读 SQLite，不提供修改/恢复，不访问本地迁移目录。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-migration-event-timeline.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-migration-event-timeline.md)。

## v0.8.30-preview.71（已交付：按 UTC 日期筛选迁移历史）

- **候选目标**：历史记录支持按开始时间 UTC 日期范围筛选，并沿用当前筛选进行稳定分页和摘要导出，便于在记录较多时限定排查窗口。
- **范围**：起始日和结束日均为 UTC 日历日期；起始边界包含当日 00:00:00Z，结束边界包含整日（内部使用次日 00:00:00Z 排除边界）。日期严格校验，不改变既有状态/路径筛选语义。
- **边界**：本地 SQLite 只读；不触碰迁移目录、网盘或管理员/百度凭据；无日期筛选时保持现有行为。
- **发布验证**：`scripts/package.ps1 -Version v0.8.30-preview.71 -AllowDirty` 通过；WebUI 38/38、userscript 3/3、全仓 Go 测试、`go vet ./...` 及三平台构建通过。ZIP 13 项、SHA256SUMS 12/12、私有数据 0 项；Windows 二进制版本正确。ZIP SHA-256 `6F974418439DD2A8646D128F6CB1FB31A372ACAFC7D431315E705861943E94C8`；详细记录见 releases 目录 `v0.8.30-preview.71-validation.md`。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-local-migration-history-utc-date-filter.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-local-migration-history-utc-date-filter.md)。

## v0.8.30-preview.72（已交付：独立只读远端操作历史页）

- **版本目标与实现**：将原嵌入网盘管理的折叠历史区迁移到 `/operations` 独立页面；`/cloud` 增加只读历史链接，独立页可返回网盘管理。
- **复用能力**：沿用 SQLite 列表分页、状态/类型/关键字/时间筛选、摘要 JSON 导出、单笔事件查看及脱敏证据导出。没有改历史 API、数据库或操作执行语义。只读历史页不含文件浏览、mkdir/move/rename/delete、重发或放行入口；未知结果保持隔离。
- **回归与发布验证**：登录保护的 `/operations` HTTP 集成测试通过；WebUI 40/40、userscript 3/3、全仓 Go 测试、`go vet ./...` 和三平台构建通过。ZIP 13 项、校验 12/12、无私有运行数据，包内版本正确；SHA-256 见 releases 目录验证记录。
- **验收边界**：只生成独立包，没有启动服务或部署到用户运行目录；没有调用真实百度账号或 NAS/SMB。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-history-standalone.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-history-standalone.md)。

## v0.8.30-preview.73（已实现：远端操作事件按状态筛选）

- **实现**：SQLite 单笔操作事件查询支持 `prepared`、`confirmed`、`unknown`、`failed` 状态过滤；筛选条件与 operation ID、sequence 游标共同限定，空结果不影响操作是否存在的判定。API 拒绝重复或非法状态参数，保持事件证据递归脱敏和只读行为。
- **页面**：独立历史页的每笔事件时间线增加状态下拉、应用和清除按钮。切换筛选从最新事件开始，旧页清除，后续分页复用当前状态；每个操作单独缓存和计数请求，迟到响应会被丢弃。
- **验证**：定向 Store/API/downloader 与 WebUI 回归通过；完整测试、跨平台发布包门禁记录见 releases 目录 `v0.8.30-preview.73-validation.md`。
- **边界**：未访问百度 API、未执行远端写操作、未启动服务或部署到真实运行目录；NAS/SMB 和真实浏览器环境仍需外部验收。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-event-status-filter.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-event-status-filter.md)。

## v0.8.30-preview.74（已交付：事件时间线 UTC 日期筛选与筛选证据导出）

- **候选目标**：在 `.73` 的单笔事件状态筛选上增加 UTC 起止日期，并允许下载匹配的事件证据，便于长时间线定位和留存。
- **实现**：Store 对单笔事件页及证据导出共用 operation ID、状态、UTC 日期条件；结束日按包含整日处理，内部转为次日 UTC 零点排除边界。证据导出在单个 SQLite 只读快照中返回操作摘要和升序事件，筛选后超过 100 条则整份拒绝。原有全事件导出保持不变。
- **页面**：时间线可按状态与 UTC 日期筛选，分页沿用全部条件；支持只在明确点击后导出当前匹配证据。无效范围不发请求；事件读取失败时保留筛选值和重试控件；旧状态仍按 unknown 隔离。
- **发布验证**：全仓 Go 测试和 vet、WebUI 43/43、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、SHA256SUMS 12/12、私有运行数据 0 项，包内版本和源码文档快照通过核对。SHA-256 与边界见 releases 目录 `v0.8.30-preview.74-validation.md`。
- **验收边界**：只生成独立预览包，没有访问百度 API、操作真实账号/NAS、启动服务或部署到运行目录；Linux 与 ARM64 尚未做目标硬件实跑，真实浏览器人工验收未完成。
- **设计与计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-event-utc-range-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-event-utc-range-export.md)。

## v0.8.30-preview.75（已交付：下载任务事件证据导出）

- **版本目标**：用户可从下载任务时间线主动下载指定任务的完整本地事件，方便长期后台下载故障排查。
- **实现**：新增 SQLite 只读事务快照导出，按序返回单任务事件；最多 100 条，第 101 条出现时整份拒绝。管理员保护 GET 附件接口递归遮蔽嵌套敏感字段；任务时间线增加下载按钮和登录失效/超限/通用错误提示。不导出当前任务投影，不触发扫描、重试、取消、百度 API 或远端变更。
- **手动添加浏览器复测（2026-10-30）**：当前运行 `.63` 的真实登录浏览器显示账号可用，根目录及 `/NAS_Download_Queue_preview` 可读取；页面输入 `/AI量化交易第1期` 后，预检成功，1 个项目的目标为 `/NAS_Download_Queue_preview/AI量化交易第1期`，状态“待确认”。未勾选确认或提交；随后受保护 `/api/status` 返回任务总数 0，health 为 `ok`。因此本次未复现用户报告的请求失败，未执行云端写操作或下载。
- **验证和发布**：Store 覆盖空事件、100/101 条上限、升序和缺失任务；API 覆盖会话保护、成功附件、递归遮蔽、404/400/405、超限 413/no-store 整份拒绝；WebUI 45/45 覆盖主动点击、并发重复拦截、成功下载和 401/413 提示。全仓 Go 测试、`go vet ./...`、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项、SHA256SUMS 12/12，私有数据 0；SHA-256 和源码快照见 releases 目录 `v0.8.30-preview.75-validation.md`。
- **验收边界**：生成独立预览包，未替换正在运行的 `.63`、未启动新版本或更新运行目录。此次真实浏览器验收仅覆盖登录、列表及只读入队预检；未确认远端移动或下载。NAS/SMB 长队列、迁移占用重试与中断恢复仍需目标环境实测。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-task-event-evidence-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-task-event-evidence-export.md)。

## v0.8.30-preview.76（已交付：下载任务状态筛选与搜索）

- **实现**：Downloader 按允许的任务状态和云端路径/文件名字面子串过滤；搜索去除首尾空格并忽略大小写，限制 128 个 Unicode 码点。筛选后仍服务器端排序/分页，只保留 `offset+limit` 个候选；响应同时返回筛选命中数、全队列数量和未筛选状态统计。HTTP 与 WebSocket 共享校验规则，拒绝重复/非法筛选。WebSocket 与轮询页在筛选时返回第一页，翻页和自动刷新保留条件，迟到的旧响应不会覆盖新条件；超长输入不发送请求。输入框长度按 UTF-16 单元设置为 256，以容纳最多 128 个补充平面码点，脚本按码点执行实际限制。
- **TDD 与验证**：Downloader、HTTP、WebSocket 和双页面交互均有回归；页面测试确认筛选条件、分页、自动刷新、清除、全队列计数和旧响应处理。`go test ./... -count=1`、`go vet ./...`、WebUI 47/47、userscript 3/3 通过。一次打包尝试中的 downloader 全量测试出现间歇失败；单独复跑和最终发布脚本全仓测试通过，未跳过测试。
- **发布包**：三平台二进制（Windows AMD64、Linux AMD64、Linux ARM64），ZIP 13 项；12/12 校验和通过，304 项源码快照逐项匹配打包时工作区；私有运行文件 0 项。包内 Windows 版本为 `v0.8.30-preview.76`，Linux ELF machine 为 AMD64 62、ARM64 183。归档 SHA-256：`743C64CC3B78CB6F6CD294026A09C7F2DB634E161D47316DDA6B78E13A728B25`。详细记录见 release 目录 `v0.8.30-preview.76-validation.md`。
- **验收边界**：已生成独立预览包，没有覆盖或启动 `.63`，没有访问百度接口或更改真实账号状态；网页交互由 Node DOM 模拟验证，真实浏览器和真实长队列验收仍待用户使用 `.76`。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-task-list-filter.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-task-list-filter.md)。

## v0.8.30-preview.77（已交付：只读网盘请求网络错误诊断）

- **目标**：改善手动添加预检与实时目录读取的 DNS、超时、连接失败提示，帮助定位 NAS DNS/网络/代理问题。
- **安全范围**：沿用只读错误通道和固定中文文案；HTTP/百度错误码保持原行为，未知的写操作仍使用隔离/不可重发语义；不展示 URL/凭据，不自动重试或提交移动。
- **实现**：只读 `readFailure` 安全分类 DNS 解析失败、网络超时、dial 失败和其他网络中断，提示检查 NAS DNS/网络/代理并明确只读查询可安全重试。保留 HTTP/百度错误码、认证和 context 原分类；所有网络文案固定，不含底层字符串、URL 或凭据。写操作的 `failure` 路径和 unknown 隔离保持不变。
- **TDD 与验证**：带敏感 URL 的 DNS/超时/dial API 回归先因通用提示失败，分类后通过；预检页面模拟服务端安全错误，确认文案可见且不自动重试、不确认、不提交。`go test ./...`、`go vet ./...`、WebUI 48/48、userscript 3/3 通过；`git diff --check` 通过。
- **发布包**：Windows AMD64、Linux AMD64、Linux ARM64 构建通过；ZIP 13 项、12/12 checksum、306 项源码快照逐项匹配、私有运行数据 0 项；Windows 版本检查与 ELF machine 62/183 通过。SHA-256：`A5B22C137CB164D6195C329E782B982FF672E79D5CF5A5BC5316B7BFB524C641`；完整记录见 releases 目录 `v0.8.30-preview.77-validation.md`。
- **初始验收边界**：发布时只生成独立测试包，没有启动 `.63`、没有访问真实百度账号。DNS/网络分类通过合成错误测试，尚未在用户 NAS 的真实网络故障下验证。
- **真实账号只读启动验收（2026-10-30）**：按用户此前授权确认 `.63` health `ok`、OAuth ready/账号绑定、队列 0、活动下载 0、source unknown 0、监控目录 0 项、全盘缓存扫描 complete 后，停止 `.63` 并将唯一 `.nas-downloader` 移动到 `.77`（未复制）。`.77` 在 `127.0.0.1:18089` 返回 health `ok`、OAuth ready/账号绑定、任务 0、下载 0、source unknown 0；百度根目录只读分页返回 3 项，监控目录仍为 0 项；`/cloud` 与 `/operations` 均 HTTP 200；`.63` 原状态目录不存在。releases 下 account.json 与 baidu-app.json 各仅 `.77` 一份；旧 smoke-data 目录仅有测试清单/任务数据，不含账号或百度应用凭据。未执行网盘写入、下载、重复扫描或清理。
- **手动添加失败复测（2026-10-30）**：在 `.77` 对真实账号执行页面同用的 `POST /api/intakes/preview`，输入 `/AI量化交易第1期`，HTTP 200，返回 1 个 `ready` 目录，目标 `/NAS_Download_Queue_preview/AI量化交易第1期`；复核任务仍为 0，未提交移动。`go test ./internal/intakeapi -count=1`、WebUI 48/48 通过；源码确认预检失败走只读错误分类，页面不会自动重试。浏览器自动化无法取得当前页面 URL，故真实浏览器点击链路仍未验证；此次 API 复测未复现原错误。
- **下一批候选评估（2026-10-30）**：从 `.77` 当前真实账号 SQLite 缓存只读统计，现有大小预筛为 1,811 个文件、128,826,360,902 bytes；再加有效 MD5 分组会剩 1,784 个、125,136,335,020 bytes，仅排除 27 个文件/3,690,025,882 bytes（约 2.9%）。由于收益较小，且 MD5 预筛会在提供方元数据异常时漏掉重复候选，本轮不以此风险换取有限带宽收益。暂不为该优化单独编号；下一版继续等待 NAS/SMB 实测或达到发布门槛的反馈/改进批次。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-readonly-network-error-diagnostics.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-readonly-network-error-diagnostics.md)。

下一版不立即编号：后续候选先按同主题归并，累计至少 5 条彼此独立的功能/改进/Bug 后再确定版本号；先收集用户反馈和 NAS/SMB 迁移/恢复验收证据，避免为目录缓存相邻小功能继续切分 preview。已交付版本不回溯合并，规则详见 `release-versioning.zh-CN.md`。

## v0.8.30-preview.59（已交付：远端操作历史只读视图）

- **版本目标**：在网盘管理页查看已持久化的远端操作及追加事件，支持状态筛选、分页和 UTC 时间线；未知结果维持隔离。
- **实现**：新增 SQLite 有界分页查询、downloader 只读 façade、受管理认证保护的 GET 列表/事件接口，以及默认折叠、按需加载的网页历史区。接口不返回 account fingerprint，对 evidence 中 token/secret/password/authorization/appkey/signkey 字段递归掩码；不提供重发、放行、删除历史等入口；不改 schema、不访问百度 API。
- **回归与发布验证**：新增 Store/API/UI 回归；全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 25/25、userscript 3/3 通过。`scripts/package.ps1 -Version v0.8.30-preview.59 -AllowDirty` 通过，并生成 Windows AMD64/Linux AMD64/Linux ARM64 包；载荷清单、版本和 SHA-256 见 releases 目录 `v0.8.30-preview.59-validation.md`。
- **真实账号只读冒烟（2026-10-30）**：切换前 `.58` health 为 `ok`、OAuth `ready`、任务总数 0；按用户此前授权停止旧进程并将唯一真实 `.nas-downloader` 移动到 `.59`，未复制。`.59` health 为 `ok`、OAuth `ready`、任务数 0；本地操作历史 GET 返回 19 条，单条事件 GET 返回 2 个事件，时间为 UTC。`.57`、`.58` 和 releases 根目录均无该真实状态目录。未执行网盘写入、下载、重复扫描或清理。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-history.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-history.md)。

## v0.8.30-preview.60（已交付：远端操作历史检索）

- **版本范围**：在 `.59` 只读历史页基础上按操作 ID、文件 ID、源/目标路径关键字和 UTC 时间范围查找 SQLite 历史；继续支持状态筛选、分页和事件详情，不访问百度 API。
- **安全边界**：参数化 SQLite 查询，`%`、`_`、反斜杠按字面字符匹配；有界关键字、分页和 RFC3339 时间输入；unknown 保持隔离，无远端变更入口。
- **实现**：Store 同时检索操作 ID、object ID、源/目标路径；状态和包含式时间边界可组合；调用仍限于当前账号 SQLite。API 验证 128 Unicode 字符上限和 RFC3339 纳秒范围并归一为 UTC。网页用本地日期时间控件，搜索后回到第一页，清空恢复全历史；请求序号防止旧搜索响应覆盖新条件。
- **TDD**：Store 新测试先因 filtered query 不存在而编译失败；API 新测试先因超长 q 返回 200 而失败；WebUI 新测试先因搜索控件未接线而失败。实现后 Store/API 定向测试、Downloader/API/Store 相关包全量测试通过。
- **发布验证**：`go test ./... -count=1`、`go vet ./...`、WebUI 27/27、userscript 3/3 通过；Windows AMD64/Linux AMD64/Linux ARM64 打包通过。ZIP 清单、268 个文件的 SHA-256 源码快照和包内版本检查见 releases 目录 `v0.8.30-preview.60-validation.md`。
- **真实账号只读冒烟（2026-10-30）**：`.59` health 正常、OAuth ready、任务 0、历史 19 条后，按授权将唯一 `.nas-downloader` 移动到 `.60`。`.60` health 正常、OAuth ready、任务 0；本地历史按操作 ID 命中 1 条、按 fs_id 命中 1 条、按路径关键字命中 4 条、精确 UTC 时间范围命中 1 条。`.59` 与 releases 根目录无真实状态副本。本轮全部是受保护 GET，没有远端写操作、下载或清理。

## v0.8.30-preview.61（已交付：远端操作历史摘要导出）

- **版本目标**：在只读历史页导出当前筛选命中的远端操作摘要，便于归档和离线核对。
- **实现**：SQLite Store 在单个只读事务内计数并读取稳定排序的摘要快照；最多导出 5,000 条，超限整批拒绝。复用 `.60` 的状态、关键字和 UTC 时间范围过滤；JSON 不包含账号指纹和事件证据。管理 API 为受保护 GET 附件，页面用同源 fetch 下载 Blob，不带列表分页参数；超限等 HTTP 错误显示在历史区。
- **验证**：Store 超限/取消/关闭、API 筛选/附件/只读与 WebUI 当前筛选、Blob 下载/HTTP 错误提示回归通过；全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 29/29、userscript 3/3 通过。三平台构建与 ZIP 校验见 `v0.8.30-preview.61-validation.md`。
- **真实账号只读验收（2026-10-30）**：确认 `.60` health 正常、OAuth ready、队列 0 后停止服务，将唯一 `.nas-downloader` 移至 `.61`，无副本。`.61` health `ok`、OAuth ready、任务 0；受保护导出 GET 返回 19 条摘要，按其中一个完整 operation ID 筛选返回 1 条。响应有 UTC 导出时间和 attachment 文件名，不含 evidence/account fingerprint；未执行百度远端写入、下载或清理。
- **发布**：`.61` ZIP 13 项，SHA256SUMS 12/12，源码快照匹配当前源码，私有运行文件 0；Windows 包内版本正确。最终 SHA-256 和详细证据见 releases 目录 `v0.8.30-preview.61-validation.md`。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-history-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-history-export.md)。

## v0.8.30-preview.62（已交付：单条远端操作脱敏证据导出）

- **版本目标**：除 `.61` 的摘要导出外，允许单独保存某笔操作及其完整追加事件，便于离线审计时间线。
- **范围与安全边界**：读取当前 SQLite 单个只读事务快照；每笔最多 100 条事件（事件 evidence 写入上限为 64 KiB，最坏 evidence 约 6.25 MiB），读取第 101 条即整份拒绝。API 复用现有递归敏感字段遮蔽；不导出 account fingerprint，不调用 provider，不提供 unknown 写操作入口。
- **实现**：Store 在同一只读事务读取摘要和完整事件；按 sequence 正序；100 条事件可完整导出，第 101 条导致整份拒绝。Downloader façade、受管理认证保护的 GET 附件 API 及递归敏感字段遮蔽已接通；网页每条历史记录提供独立导出入口并显示 HTTP 错误。
- **验证**：`go test ./... -count=1`、`go vet ./...`、WebUI 32/32、userscript 3/3 及三平台打包门禁通过；单笔 Store/API/WebUI 回归覆盖 100/101 条边界、未知状态只读、递归脱敏、附件头、错误提示、阻止重复请求和 provider 零调用。
- **真实账号只读验收（2026-10-30）**：确认 `.61` health 正常、OAuth ready、队列 0 后停止服务，将唯一 `.nas-downloader` 移至 `.62`，未复制。`.62` health `ok`、OAuth ready、任务 0；受保护 GET 按一条现有 operation ID 导出 2 个事件，序号升序、附件文件名安全，不含 account fingerprint；递归检查所有敏感字段值均为 `[REDACTED]`。未执行百度远端写入、下载或清理。
- **发布**：`.62` ZIP 13 项，SHA256SUMS 12/12，源码快照与当前源文件匹配，私有运行文件 0；Windows 包内版本正确。最终 SHA-256 和逐项证据见 releases 目录 `v0.8.30-preview.62-validation.md`。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-evidence-export.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-evidence-export.md)。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-remote-operation-history-search.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-remote-operation-history-search.md)。

## v0.8.30-preview.58（已交付：网盘改名/移动/删除同步待执行队列）
- **真实账号启动及只读冒烟（2026-10-30）**：先核实 `.57` health 为 `ok`、队列/下载/unknown 均为 0，再停止唯一旧进程并将 `.nas-downloader` 移动到 `.58`（不是复制）；`.57` 状态目录现不存在，`.58` 状态目录存在。`.58` 以原端口 8080 启动，health 返回版本 `.58`、OAuth `ready`，任务/下载/unknown 均为 0。网页登录/API 密钥 `adminadmin` 与版本 1 JSON 格式读回正确。网页路径设置读回 `/NAS_Download_Queue_preview` 和 `D:\projects\Git-Projects\downloader_auto_baidu`，两项仍可编辑；实时监控目录 0 项、网盘根目录分页 50 项、本地缓存根目录分页 50 项。此轮无网盘写入、下载或清理。ZIP 未改动，SHA-256 仍为 `B5D5B1CB510836CFFA07E1DCFFA2E3C1DADE4CD4FC818E477DD2F3D20C15255C`。
- **重复账号侧车核查与清理（2026-10-30）**：发现 releases 根目录遗留一份旧 .nas-downloader。仅比较 account.json、baidu-app.json、credentials.json 的 SHA-256，不读取密钥内容；三者与 .58 完全相同。旧 SQLite integrity_check=ok，schema 16，但 jobs、job_events、remote_operations、remote_operation_events、cloud_inventory、migration 记录均为 0。该副本未被进程使用；按用户‘保持只有一份’要求逐个删除旧副本的 6 个文件及空目录。复核 releases 下 account.json/baidu-app.json 各仅 .58 一份，.57 和 releases 根目录均无状态目录；.58 仍运行正常。旧验证目录中不含真实账号侧车，均保留。

- **版本目标**：补齐已批准的网盘管理要求：改名/移动后按 `fs_id` 更新待执行任务，确认删除后写入专用远端取消事件。保留下载模式、创建时间与尝试历史；活动、未知、已尝试或终态任务不得被重建并重置。
- **缺陷证据**：此前 `scheduler.reconcileObserved` 遇到相同 `fs_id` 但 `FileInfo` 改变时会新建 pending job，可能覆盖任务模式和历史；确认删除的待执行项则被记为通用“源项离开队列”，缺少远端操作 ID。
- **实现进度**：新增安全的同 fs_id 队列身份重基和持久化事件；确认的 rename/move 事件分别保存 `response_observed_at_utc` 与之后读取对象的 `cloud_observed_at_utc`，确认 delete 则保存专用事件及回执时间。移出监控目录的零尝试任务移回后恢复手动重试，不自动下载。旧 `SourceState=changed` 冲突在下一次成功队列扫描后恢复同一任务，保留尝试/失败计数。无 schema 迁移。
- **RED/GREEN**：6 个新回归先按预期失败，再通过；覆盖 rename/move 的策略/历史保持、队列移出/移回后手动重试、delete 关联远端操作与 UTC 回执、扫描按 fs_id 更新未开始任务、已尝试任务不被覆盖和 unknown 隔离。既有“身份变化后等待新列表”恢复测试通过。
- **当前验证**：RED/GREEN 新回归、`go test ./internal/downloader -count=1`（90.8 秒）、全仓 Go 测试、`go vet ./...`、WebUI 24/24、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。最终 ZIP 独立清单校验 12/12、私有数据 0，包内 Windows 可执行文件报告 `.58`。hash 和源码快照见 releases 目录 `v0.8.30-preview.58-validation.md`。未访问真实百度账号，未触碰 `.57` 运行目录。
- **设计/计划**：[中文设计](../superpowers/specs/2026-10-30-confirmed-cloud-queue-identity.zh-CN.md)、[实施计划](../superpowers/plans/2026-10-30-confirmed-cloud-queue-identity.md)。

## v0.8.30-preview.57（本地缓存全盘搜索，已打包）

- **版本目标**：允许在全盘 SQLite 缓存中按名称或路径查找文件/目录，并明确告知缓存覆盖程度；搜索不调用百度 API。用户从结果进入目录时，回到实时百度列表重新读取其父目录，避免用陈旧缓存对象执行远端操作。
- **实现**：新增受保护 `GET /api/cloud-inventory/search`，支持 1–128 Unicode 字符查询和分页，账号范围隔离，`%`/`_` 按字面字符处理；返回缓存扫描状态、未完成目录数和更新时间。匹配目录附带 SQLite 本地聚合大小。页面在本地缓存模式增加搜索入口，缓存不完整时提示无命中不代表网盘无匹配；结果本身只提供打开实时父目录。
- **同版本安全修复**：真实账号空目录只读预览显示自动监控队列目录 `/NAS_Download_Queue_preview` 可清理；删除后端虽会拒绝队列目录，但批量操作可能先删其他候选再中止。现预览过滤当前队列根目录及子树。新增 downloader 回归，先复现队列目录和嵌套子目录泄漏，再验证仅列出队列外空目录；未执行删除。
- **同版本目录元数据修复**：真实账号受控 CRUD 发现，远端目录 size/mtime 会随子项增删或移动变化；删除空子目录后再删除父目录时，实时对象身份仍有效，但缓存中的旧 mtime 导致增量更新拒绝并将缓存标为 stale。现在缓存增量复核对目录只比较 fs_id、路径、名称和目录类型；文件仍严格比较大小、mtime 和 MD5。新增回归先复现“目录 size/mtime 已变化但确认删除未反映到库存”，再修复并通过。
- **验证**：搜索 Store/API 和缓存模式 UI 测试覆盖分页、账号隔离、字面通配符、未知/不完整/扫描中覆盖、目录聚合大小、非法输入，以及搜索期间不调用实时网盘接口。`go test ./...`、`go vet ./...`、 `node scripts/webui.test.cjs`（24/24）、 `node scripts/userscript.test.cjs`（3/3）通过。
- **打包门禁**：修复后同版本更新 `.57`，全仓 Go 测试、vet、WebUI 24 项、userscript 3 项以及 Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 个文件，SHA256SUMS 12 项全部匹配，无 `.env`、授权侧车、管理员密钥、SQLite 或下载数据。当前 ZIP SHA-256 与完整校验记录见 releases 目录的 `v0.8.30-preview.57-validation.md`。
- **真实账号页面验收（2026-10-30）**：按此前授权将唯一 `.nas-downloader` 运行目录从 `.56` 移至 `.57` 后启动新版；网页登录页显示全盘缓存 complete（983 页/8,050 项）。本地缓存搜索 `CASE-text-to-sql` 返回 17 项，显示缓存完整和扫描时间；点击“实时查看所在目录”切至实时模式并定位 `/AI量化交易第1期/17-Agent实战与QMT实盘接口`，只读 `/api/cloud` 返回该目录 4 项。空目录预览显示 32 个候选，队列根目录与子树均不在候选中。health 返回 `.57`、认证正常、任务数 0。未移动或删除网盘文件，未启动重复文件扫描。
- **真实账号 CRUD 与修复复验（2026-10-30）**：在网盘根目录用唯一随机路径创建空目录、改名、创建空目标目录、移动、删除移动后的测试目录和空目标目录；首次一轮 6 笔远端操作均 confirmed、实时根目录无测试项，但暴露父目录缓存 mtime 失配，没有重发远端操作。修复后 `TestApplyConfirmedCloudMutationAcceptsChangedDirectoryMetadata` RED→GREEN；完成 983 页/8,050 项只读重扫后，用新版再次完成相同 6 步 CRUD，所有操作 confirmed 且无缓存警告。两项测试路径在实时列表和本地库存中均无残留；缓存 coverage complete、未完成目录 0，空目录候选仍 32 项且不含队列目录。SQLite `integrity_check=ok`，任务数 0。
- **运行状态**：`.57` 当前运行于 `127.0.0.1:8080`；唯一百度授权、SQLite 和队列数据位于 `.57/.nas-downloader`，`.56` 原运行目录已停止并移走该数据。管理员密钥按用户要求为 `adminadmin`，文件为版本 1 JSON；重启后 Bearer health 验证成功。SQLite 启动完整性检查为 `ok`。
- **当前边界**：约 120 GiB 的重复文件扫描及 32 个既有空目录的删除均未执行；需要用户在页面查看估算/候选并确认后再运行。NAS/SMB 路径迁移、占用重试和中断恢复仍须在目标 NAS 环境验收。

## v0.8.30-preview.56（本地缓存浏览，已打包）

- **版本目标**：提供完整的用户可见功能，在网盘管理页明确选择实时百度列表或本地 SQLite 缓存列表；缓存数据需展示覆盖状态和云端观测时间，且保持只读。
- **实现**：默认仍进入实时列表。缓存模式请求现有 `/api/cloud-inventory` 分页接口，不调用实时 `/api/cloud`，也不在缓存请求失败时回退；complete、stale/scanning、无缓存元数据分别呈现，complete 空目录不会与未缓存目录混淆。缓存模式隐藏远端新建/移动/改名/删除控件，并保留目录导航、分页和本地目录大小读取；切换数据源重置分页，返回实时模式恢复远端操作入口。
- **回归**：新增交互测试先以缺失选择器和缓存行为失败，再通过。首次审查发现清理卡片仍可远端删除；改为缓存模式隐藏整块空目录/重复文件清理区，切回实时模式恢复，新增断言并通过定向复测。
`node scripts/webui.test.cjs` 23/23，
`node scripts/userscript.test.cjs` 3/3；`go test ./...` 与 `go vet ./...` 通过。没有修改后端/API/schema，因此未新增重复的服务端测试。
- **打包门禁**：最终修正后再次运行 `scripts/package.ps1 -Version v0.8.30-preview.56 -AllowDirty -UpdatePreview`；全仓 Go 测试、vet、WebUI 23 项、userscript 3 项及 Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 个文件，SHA256SUMS 12 项均匹配；无 `.env`、授权侧车、管理员密钥、SQLite 或下载数据。ZIP 与最终 SHA-256 见独立 releases 目录。
- **审查**：独立只读复核先发现缓存模式下仍可操作空目录/重复内容清理；两块清理卡片现随缓存模式隐藏，切回实时模式恢复。复核修复和两项交互断言通过，未发现新的 Critical/Important。
- **真实账号只读验收（2026-10-30 03:18 Asia/Shanghai）**：在用户授权的唯一新版运行目录启动 v0.8.30-preview.56，HTTP 8080。health 返回版本 .56、OAuth ready；任务队列 0；监控目录 `/NAS_Download_Queue_preview` 实时列表为 0 项。完整 SQLite 缓存列表返回 51 项（39 个目录），39 个目录行均附带本地聚合大小与 `complete=true`；缓存状态 complete。HTTP 获取管理页确认来源选择器与“本地缓存”选项已随包提供。此验证未执行网盘写入、下载、SHA-256 重复扫描或目录删除；未做交互式真实浏览器检查。
- **手动入队目录预检复核（2026-10-30 03:27 Asia/Shanghai）**：使用当前运行服务和用户提供的 `/AI量化交易第1期` 路径，重新执行只读 `POST /api/intakes/preview`，返回 HTTP 200，来源目录身份有效、目标为 `/NAS_Download_Queue_preview/AI量化交易第1期`、状态 `ready`。随后登出 HTTP 会话。该接口只读核对，不移动网盘内容，也不创建下载任务；不能替代浏览器交互，但说明截图所示路径当前预检可通过。
- **运行目录**：按用户授权将原 v0.8.30-preview.53 的 `.nas-downloader` 移至 `.56`，保留 8,050 条库存及完整操作证据；新版服务当前运行在 8080，旧进程已停止。另发现 releases 根目录有一份同账号旧 sidecar（凭据文件与当前副本相同，数据库无任务/库存/操作历史）；自动策略拒绝递归删除，故该重复目录仍需用户手动处理。其他验证目录内不含该账号 `account.json` 的测试 sidecar 未触碰。
- **手动入队真实浏览器复测（2026-10-30）**：在 `.56` 管理页打开 `/intake`，可见账号可用、根目录 51 项；输入 `/AI量化交易第1期` 后点击“预检这些项目”，页面显示预检完成、目标 `/NAS_Download_Queue_preview/AI量化交易第1期`、源项目待确认。确认复选框未选，移动按钮 disabled。随后以同一浏览器会话只读检查 `/api/health`：版本 `.56`、认证 ready、`total_tasks=0`、`downloading=0`。用户截图中的请求失败本次未复现；未提交移动、未创建下载任务。
- **验收边界**：本轮真实账号检查通过 API 与 HTTP 页面获取，未交互操作真实浏览器；NAS/SMB 路径迁移和重启恢复仍需目标环境验收。约 120 GiB 重复扫描需页面确认，33 个既有空目录需复核后再决定删除；新 preview 等累积修改达到版本门槛后再开。
- **同版本补全**：核查缓存页后发现目录行仍提示“目录大小按需读取”。现扩展 `GET /api/cloud-inventory` 的目录条目，附带 `directory_size_bytes` / `directory_size_complete`，从本地 SQLite generation-scoped 大小缓存读取或计算；页面直接展示目录大小，stale 子树显示不完整。文件 `size` 与远端对象身份字段不变，不增加实时百度列表请求。每页目录聚合共用一个 SQLite 事务。Store 与 WebUI 回归均先失败后通过；
`node scripts/webui.test.cjs` 23/23，userscript 3/3，`go test ./...`、`go vet ./...` 和 Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 13 项、SHA256SUMS 12 项全部匹配；不含 `.env`、授权侧车、管理员密钥、SQLite 或下载文件。最终包由后续文档快照同步重打包。

## v0.8.30-preview.55（按目录刷新缓存，已交付）

- **版本目标**：提供一个完整用户可见功能，允许只刷新当前网盘目录及其子树，不必重扫整个账号；不得把局部覆盖标成全盘 complete。
- **实现**：SQLite schema 16 增加独立的局部刷新状态和持久化队列；每页库存、覆盖状态、队列及游标在单事务提交，末页从本次 run 已记录的目录项派生后代队列。页面提供当前目录刷新、进度详情、暂停控制及完成后列表/大小刷新；全盘扫描与局部刷新互斥，远端写操作与其范围冲突时在 provider 写入和操作日志前拒绝。
- **回归**：`go test ./internal/taskstore -count=1`、`go test ./internal/downloader -count=1`、`go test ./internal/intakeapi -count=1` 通过；覆盖迁移与重开、原子页/游标回滚、嵌套目录与兄弟目录隔离、全盘状态不变、暂停续跑/范围拒绝、可重试错误维持 backoff、不可重试错误结束且保持缓存 stale、远端操作前拒绝。
`node scripts/webui.test.cjs` 为 22/22，
`node scripts/userscript.test.cjs` 为 3/3。复核者初报子目录未递归，检查到末页事务会从本次 run 查询并排入已扫描页中的子目录后撤回；没有保留 Critical/Important finding。
- **打包门禁**：`scripts/package.ps1 -Version v0.8.30-preview.55 -AllowDirty -UpdatePreview` 全仓测试、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。Windows `-version` 输出 `v0.8.30-preview.55`；ZIP 13 项、发布清单 12 项全部匹配、无 `.nas-downloader`/凭据/admin-key/下载文件。ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.30-preview.55.zip`；SHA-256 `ea8fa326a76360fce22cf0ba05f40a275b867cd045d3afedd167af54fa3347e8`。
- **真实账号只读验证（2026-10-30）**：隔离副本使用真实授权 sidecar，远端监控路径设为随机不存在路径，调用当前目录实时列表及局部刷新，只刷新 `/NAS_Download_Queue_preview`；成功完成 1 页/0 项，局部目录大小 complete、0 bytes、0 个文件/子目录；全盘扫描状态仍为空（独立副本中没有创建全盘状态）。未做远端写入或下载；测试进程已停止，验证副本（含临时授权副本）已删除。
- **限制**：真实账号目标目录当前为空，本次 E2E 未测到非空后代目录；嵌套目录行为由 downloader 集成测试覆盖。NAS/SMB 与真实浏览器仍需用户环境验收。`v0.8.30-preview.56` 候选为本地缓存浏览，交互范围待用户确认。

## v0.8.30-preview.54（已确认网盘变更即时刷新本地缓存）

- **版本目标**：在完整 SQLite 清单覆盖受影响路径且无扫描游标冲突时，百度端 confirmed 的目录新建、改名、移动和删除，事务性刷新本地条目、目录覆盖状态、大小代次与时间证据；只包含这一项完整用户可见功能，符合版本门槛。
- **已实现**：taskstore 以单事务更新 inventory、目录状态、hash 路径和 generation；检测缺少的后代目录覆盖、身份不匹配、目标碰撞和扫描队列冲突。Provider 确认后重新读取 rename/move 身份，mkdir 从父目录列表唯一确认新身份；无法证明增量时保留 stale 与重扫提示，unknown 仍隔离。缓存刷新失败追加 SQLite 事件，保留百度操作 confirmed 状态，并记录本地失败原因和事件时间。
- **本地验证**：缓存事务和 downloader 集成测试通过，覆盖文件/目录 rename、move、delete、mkdir、generation、事务回滚、MD5 保留、身份/碰撞/覆盖拒绝、confirmed 观察失败、unknown 隔离和扫描队列前置拒绝。确认操作后的本地缓存刷新失败会追加 `remote_operation_events` 诊断事件，重开 SQLite 后仍保留本地失败原因/时间，远端操作仍为 `confirmed`。WebUI 22 项、userscript 3 项、`go test ./...`、`go vet ./...`、Windows/Linux AMD64/Linux ARM64 构建均通过。
- **发布包**：`v0.8.30-preview.54.zip` 位于独立 releases 目录；ZIP 12 项载荷校验通过，排除了 `.env`、授权凭据、管理员密钥、SQLite 和 downloads。源码快照基准 `ef1596b6d2e47193ed3a1681ea1f567437ed98d9`，打包时含工作区变更；最终 ZIP 的 SHA-256 见 releases 目录旁的 `.sha256` 校验文件。
- **真实账号 CRUD**：独立测试副本完成自建空目录 mkdir、rename、move 和空目录删除；六项远端操作均 `confirmed`，测试目录已确认清除。该副本使用全新 SQLite，目录缓存未完整，因此响应按设计带有缓存刷新 warning；本地缓存即时更新路径由 mock 集成测试验证，未宣称真实账号已验证此路径。监控路径为不存在的随机目录，未触发下载；原 `.53` 授权与运行状态未改动。
- **真实账号全盘缓存核验（2026-10-30 01:44 Asia/Shanghai）**：post-CRUD 只读扫描 run `cb1eebdbb92cb374899e965aad064f02` 完成，983 页、8,050 项、待扫 0。SQLite `integrity_check=ok`；库存含 7,075 文件和 975 目录，976 条目录覆盖状态（根目录+975 个目录）全部 complete；根目录聚合为 2,407,342,072,250 bytes，`complete=true`。测试路径前缀在云端根目录无残留。扫描未下载文件、未发远端写操作。随后已确认精确测试进程停止、端口关闭并删除隔离测试副本。
- 本机没有 SMB 映射；NAS/SMB 路径占用、迁移和重启恢复仍需用户目标环境验收。完成文档后同版本重打包，使 `SOURCE-SNAPSHOT.txt` 纳入本轮收尾。

## v0.8.30-preview.53（只读网盘错误诊断与真实账号续扫）

- **真实 CRUD 验收推进（2026-10-30 00:09 Asia/Shanghai）**：在根目录创建两个独立空测试目录，`mkdir` 远端操作均 `confirmed`：`/__nasdl_preview53_crud_20261030_9f3c_a`（fs_id `803716709111721`，operation `f5e7d7e8c533fd4c7b9bbe79a746fe1a`）和 `_b`（fs_id `849144456574835`，operation `05c94c3d8298a4e6f9c1edfd0e2ece6c`）。第一次改名在远端发送前被身份核验拒绝；SQLite 未出现 rename journal。直接只读 `filemetas` 确认百度省略 `server_filename`；客户端现从 provider path 派生缺失名称，回归测试先失败、后通过。更新 `.53` 后在真实账号完成 rename（`f356cd6a3377d82b6a6a8bbc0d4d739a`）、move（`d51f7bacfe3830caa170e1e00324c18d`）、删除空子目录（`93b90eee5fb9265ae856a526f1f223df`）及父目录（`abc450dea96b7be4dab592bc1c2ebcb8`），均 `confirmed`；最终 GET 根目录确认无测试项残留。
- **真实缓存恢复（2026-10-30 00:32 Asia/Shanghai）**：CRUD 后启动的只读全盘重扫 `87d5123656f76315afdaa7571d0a8b51` 已完成：983 页、8,050 项、待扫 0。SQLite 有 976 条目录状态、全部 `complete`；根目录 `complete=true`，2,407,342,072,250 bytes、7,075 files、975 directories；`integrity_check=ok`，下载任务 0，云端测试前缀条目 0。实时页面只读复核：空目录候选 33 个；重复扫描预估 1,811 个候选、128,826,360,902 bytes（约 120 GiB），未启动。
- 本机检查没有 SMB 映射，只有本地 `C:\` 和 `D:\` 文件系统盘；因此 NAS/SMB 路径占用、迁移和重启恢复仍需在用户目标环境验收。
- **当前交付核验（2026-10-30）**：`v0.8.30-preview.53.zip` 清单列出的 12 项载荷全部匹配（12/12）。运行目录管理员密钥格式正确且值为 `adminadmin`；服务 `/api/health` 返回 HTTP 200、版本 `v0.8.30-preview.53`、OAuth `ready`、任务队列 0。真实根目录列表 HTTP 200，首 30 个目录的本地缓存大小均为 complete；全盘扫描 run `87d5123656f76315afdaa7571d0a8b51` 状态为 complete（983 页/8,050 项/待扫 0）。
- `.53` 的功能实现、回归和测试包交付已完成，包继续作为当前用户测试版本；NAS/SMB 迁移及重启恢复属于目标环境验收，尚不能由本机结果替代。约 120 GiB 的重复文件哈希扫描需要用户在页面确认后才会启动；33 个既有空目录也只完成预览，没有删除。

- **继续验收（2026-10-29 23:49 Asia/Shanghai）**：浏览器实测发现完整缓存下目录行仍显示百度 API 占位 `0 B`，而点击“大小”才得到正确本地聚合值。修复 `/api/cloud` 对目录填充 SQLite 本地聚合大小及 `size_complete`；缓存不完整时显示“缓存大小未完成”。新增 Go 回归先因缺少字段失败，再通过。更新 `.53` 后真实浏览器看到 `/111111111111` 29.53 GiB、`/AI量化交易第1期` 1.57 GiB、`/Media` 1.89 TiB、空目录 0 B，与已缓存目录聚合一致。登录成功、OAuth 账号可用、全盘缓存仍为 983 页/8,050 项、下载任务数 0；没有执行远端写操作。
- WebUI 22 项、userscript 3 项、`go test ./...`、`go vet ./...`、Windows/Linux AMD64/Linux ARM64 发布构建通过。修正 `ADMIN_PASSWORD` 用户文档最低长度到 10 字节；最终 ZIP 校验值以交付时实际发布文件为准。
- 同一真实浏览器会话使用完整缓存生成空目录只读预览：33 个候选，扫描运行 ID 与全盘完整扫描一致；没有执行删除。重复项扫描仍为 `idle`，没有下载 SHA-256 候选文件。
- 后续审查发现聚合目录大小不能用于远端操作身份复核；新增独立 `provider_size` 并改 UI 预期对象构造继续使用它。回归先因字段缺失失败再通过，确保显示大小与操作前复核大小分离。
- 修正用户文档中 `ADMIN_PASSWORD` 最低长度仍写为 16 字节的过时描述，统一到代码实际接受的 10 字节；`.53` 的私有 `admin-key.json` 仍为版本 1 JSON、密码 `adminadmin`。

- **最新复核（2026-10-29 23:35 Asia/Shanghai）**：修正重复扫描估算的缓存不完整提示，并修复分页全盘扫描漏排较早页面子目录的问题。跨页回归先失败后通过；WebUI 22 项、userscript 3 项、`go test ./...`、`go vet ./...`、Windows/Linux AMD64/Linux ARM64 构建及 `.53` ZIP 更新均通过。
- `.53` 的 `admin-key.json` 已按请求修正为版本 1 JSON 记录，值为 `adminadmin`；没有创建密钥副本。网页登录成功，OAuth `ready=true`、账号绑定有效；路径读回 `/NAS_Download_Queue_preview` 和 `D:\projects\Git-Projects\downloader_auto_baidu`。
- 更新版 `.53` 于 2026-10-29 23:11 重启，SQLite schema 15、`integrity_check=ok`、任务数 0。真实账号第二轮只读全盘扫描于 23:34 完成：983 页、8,050 项（7,075 文件、975 目录）、待扫 0。SQLite 中 976 条目录状态（根目录+975 个目录）全部为 complete；根目录大小 `complete=true`，2,407,342,072,250 bytes、7,075 files、975 directories。首次扫描比本轮少 100 项，证实本次跨分页目录入队修复补齐了遗漏。
- 扫描未完整时重复项估算 API 在真实服务上返回 HTTP 409 和清晰的缓存未完整提示；未启动 SHA-256 重复内容扫描，未移动/删除/下载网盘内容。
- 打包后已删除本轮使用的 Go 构建缓存目录 `C:\Users\marvin\AppData\Local\Temp\nas-downloader-preview53-gocache`，确认路径不存在；未触碰其它缓存目录。
- 用户要求管理员密码使用 `adminadmin`；已将 `admin-key.json` 写为 `{"version":1,"key":"adminadmin"}`，并把本地管理密码最小长度从 16 调整为 10 字节。加入 taskstore 文件格式与管理 Bearer 登录回归，RED→GREEN。全量打包验证通过，`.53` 已更新。
- 更新后的 `.53` 成功启动；SQLite schema 15、integrity `ok`、任务数 0，读回监控目录 `/NAS_Download_Queue_preview` 和本地下载路径 `D:\projects\Git-Projects\downloader_auto_baidu`。OAuth 状态为 `ready=true` 且账号绑定有效。
- 前次远端建目录操作使 975 条目录缓存状态为 stale。2026-10-29 22:34（Asia/Shanghai）已通过管理 API 启动只读全盘扫描，HTTP 202；扫描状态 running，随后继续跟踪完成状态。未启动 SHA-256 重复内容扫描，未移动/删除网盘文件。
- 登录表单实测 `adminadmin` 登录成功，读取路径配置/OAuth 状态并登出均成功。续跑重扫最新观测为 119 页、1,095 项、215 个待扫目录；为打包估算错误提示修复，扫描将安全暂停后继续。
- 修复重复扫描估算的前置条件错误原先被归入通用“网盘请求失败/不要盲目重发移动”文案；现在包装为缓存冲突并复用只读错误映射。新增回归先复现错误文案，再通过。
- 本次按同版本规则继续迭代，未递增到 `.54`。修复远端 mkdir 已持久化为 `confirmed`、但后续本地队列核对失败时 API 错报为“结果未知”的问题；现在明确回传已确认结果及后续同步警告。修复重复扫描首次查询无状态行时的 502，改为返回 `idle`。
- 重复扫描新增开始前预估接口，按完整缓存统计候选文件数和预计读取字节，网页显示估算并要求确认；实际网络重试可能增加流量。估算和启动入口都检查目录缓存完整性，阻止缓存失效但扫描状态仍为历史 `complete` 时运行。
- 回归证据：新增确认操作后续核对失败接口用例、首次重复扫描状态用例、候选数/字节估算用例，以及失效缓存阻止估算和扫描的用例。全量 `go test ./...`、`go vet ./...`、WebUI 22 项、userscript 3 项通过。
- 本次打包更新：`scripts/package.ps1 -Version v0.8.30-preview.53 -UpdatePreview -AllowDirty` 成功，源码检查、跨平台构建、发布 ZIP 及 SHA-256 完成。该 SHA-256 作为本轮交付记录提供；打包脚本保留未管理运行数据。
- **已解决的阶段性阻塞（2026-10-29）**：曾出现管理员侧车解析失败、凭据核验失败和全盘缓存 stale；后续按版本 1 JSON 修正管理员密钥、完成账号核验，并在真实账号上完成全盘重扫。该历史状态不代表当前服务状态。
- 先前扫描检查点为 981 页/7,949 项且缓存 stale；后续修复跨页目录漏排，并完成 983 页/8,050 项扫描。旧状态已由本节开头 2026-10-30 的实时检查取代。
- `/NAS_Download_Queue_preview` 已由本工具创建为空目录并设置为监控目录；该远端操作状态为 `confirmed`。此前进度中关于该目录不存在/`-9` 的描述已过期。未移动、删除或下载任何已有网盘文件。
- 修复只读目录列表、入队预检和重新核对共用“不要盲目重发移动”错误提示的问题。只读请求现在返回可安全重试的说明；百度 API 错误只展示 HTTP 状态与错误码，不回传任意网络错误文本。Move/Delete 结果未知时继续维持隔离，不自动重发。
- `TestReadOnlyCloudFailuresShowSafeRetryableDiagnostics` 覆盖 `errno=-9` 诊断、只读可重试提示和模拟 `access_token` 错误文本不泄漏；intakeapi 全套及 WebUI 22 项、userscript 3 项通过。
- 真实账号实测：根目录只读分页 50 项成功；HTTP 列目录与入队预检在目录错误时均显示 `HTTP=200`、百度错误码 `-9` 和可重试读取提示。扫描进度暂停时为 5,079 项/230 个待扫目录；迁移状态目录并重启 preview.53 后读回相同检查点，续扫 12 秒推进到 5,161 项/227 个目录。SQLite schema 15、integrity `ok`；下载任务数始终为 0。
- 原进度记录中的两个监控路径 `-9` 问题已解决：`/NAS_Download_Queue_preview` 不存在，创建空目录并配置后回执为 `confirmed`；未移动、删除或下载已有文件。
- 变更该目录后网盘缓存被整体失效，SQLite 只读统计为 975 条目录状态 `stale`；这是复扫前的保守状态，不等同于本轮扫描失败。完整缓存需在账号核验恢复后重建。
- 最初 `.53` ZIP SHA-256 为 `FDD5BDD549EF36278DEA9B28396F01C71C94A9C1DE3E2268BD6E513B36862D70`；本轮同版本更新 ZIP SHA-256 为 `1FD25BC15B2AEC089988F013F574E52340F847A1771D5F401EBBFE3D19426995`。当前程序已重启至更新后的 `.53`。
- 全盘列表刷新、重复项扫描/清理、真实 CRUD、NAS/SMB 尚未完整验收；此前扫描待扫目录为 0，但远端变更已使缓存失效。百度账号重新核验是继续真实云端测试的前置条件。

## v0.8.30-preview.52（后台扫描请求生命周期与首轮真实账号）

- 全盘缓存和重复文件后台扫描从 HTTP 请求 context 改为由 Downloader 生命周期管理，防止收到 `202 Accepted` 后浏览器请求结束即取消 worker；加入请求 context 取消后的全盘扫描回归。
- ZIP SHA-256：`03AB649C63A2E7584E5FC9177B5D804C9265A2C4A263CF6B5C6ED3F9C65C3BCA`。该版本首轮真实账号验证确认 OAuth 账号绑定、根目录分页及扫描持续运行；详细的跨进程续扫证据见 preview.53。

## v0.8.30-preview.51（全盘库存页批量写入）

- 对连续文件项改用最多 80 行的多值 SQLite upsert，单句最多 880 个绑定参数；目录项仍按原页序处理身份变更和旧路径子树裁剪。页内重复 `fs_id` 仍按原先顺序逐项写入，保留 last-write-wins。整个批次、目录状态、清理、扫描队列和 generation 共用原事务。
- 回归覆盖 81 项拆批、重复 ID 顺序、目录状态路径重叠，以及第二批触发器失败时库存、已有 generation、持久化/内存扫描进度和队列整体回滚。taskstore 和全项目测试、`go vet` 通过；独立复审未发现阻塞问题。
- Windows AMD64 成对合成 benchmark（7 轮，每轮 30 次）：批量写入中位数 4.86 ms、99,433 B、688 allocs；旧顺序路径中位数 9.87 ms、79,857 B、1,738 allocs。该样本显示中位时间约降 50.8%、分配数降 60.4%，但分配字节增 24.5% 且耗时波动；不外推到 NAS/SMB。
- WebUI 22 项、userscript 3 项、全模块 Go 测试、vet、Windows/Linux AMD64/Linux ARM64 发布门禁通过；独立预览包全部载荷哈希匹配，ZIP 解压校验和 Windows 版本命令通过。ZIP SHA-256：`55646A0730C4A8844E6C22EB66FBADBED354C53B5707D51216FC4F7A9100E922`。
- 按用户此前授权，更新 preview.51 文件到 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；清单载荷哈希和目标程序版本已复核。目标目录其他文件保留。
- 本地 synthetic benchmark 和 mock SQLite 不证明百度限流、真实网盘目录或 NAS/SMB 长扫描表现。

## v0.8.30-preview.50（目录移动后的缓存身份修复）

- 修复全盘清单刷新时目录 `fs_id` 移动后旧路径子树仍留在缓存、同路径新目录可能被旧目录清理逻辑误删的问题。目录路径身份变化会在同一 SQLite 页面事务中清理旧/目标子树并将目标目录标记为待扫描；旧目录按 `fs_id` 删除，避免误删本页新身份。
- 新增移动、同路径替换及 SQLite 触发器注入失败回滚回归。taskstore 套件、WebUI 22 项、userscript 3 项、Go 全模块测试及 `go vet ./...` 通过。
- 包内 SHA-256 清单所有载荷与 ZIP 解压文件匹配；Windows 程序报告 v0.8.30-preview.50。ZIP SHA-256：`9295672D0C3873EB15E1A87AC52987D9C69AA8809CCB76DB984D9A941F259B97`。
- 按用户此前授权，将 preview.50 同名文件更新至 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；清单载荷哈希全部匹配，目标 Windows 程序报告 preview.50。未删除目标目录中的其他文件。
- 本修复验证来自本地 SQLite 合成数据；百度真实目录移动、NAS/SMB 行为和长时间扫描仍需用户环境验收。

## v0.8.30-preview.49（持久化目录大小聚合缓存）

- 网盘管理中的目录大小此前每次刷新都会按目录路径扫描全部后代文件与目录。本版增加账号级库存代次和 `(账号,路径)` SQLite 尺寸结果缓存：首次/失效后从本地全盘清单重算；同代次请求直接读缓存，服务重启后仍能命中。
- 每个扫描页提交和显式远端缓存失效同事务推进代次；页面失败则连同版本推进一起回滚。当前使用账号级失效，任一库存变动会让此账号其余目录尺寸在下次查询时重算。返回云端观测时间与本地计算时间，网页分别显示。
- SQLite v14→v15 迁移创建代次和目录尺寸缓存表，并测试现有目录数据保留及数据库版本已提交、manifest 未发布时的恢复。
- Windows 本机 SQLite 合成 50,000 文件、三轮每轮 5 次：聚合并持久化 53.4–61.2 ms / 6,579–7,398 B / 169–171 allocs；缓存命中 0.172–0.196 ms / 2,824–3,924 B / 72–75 allocs。该对照不代表 NAS/SMB 性能。
- WebUI 22 项、userscript 3 项、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项发行载荷及 ZIP 解压后哈希、Windows 版本检查通过。ZIP SHA-256：`0DE5DB5314B1887258016E8295C529F3693D6BC565E44126A18B8B8007401D84`。
- 按用户此前授权，将 preview.49 文件更新到 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；目标 12 项载荷哈希匹配，目标程序报告 preview.49。未删除目录里的其他文件。
- 真实百度目录变更、NAS/SMB I/O 与长时间扫描尚未在用户环境验证。

## v0.8.30-preview.48（重复文件预览复用 SQL 排序）

- 已验证哈希预览结果本来按 `sha256,path,fs_id` 排序；移除 Go 对每组删除候选的二次排序，保留项仍是路径字典序最小项，删除列表顺序不变。
- 乱序插入回归确认保留/删除次序；3,000 组、每组 3 个文件的基准显示分配字节约减少 6.7%、分配次数约减少 4.2%。墙钟时间波动，未宣称稳定提速。
- WebUI 22 项、userscript 3 项、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项载荷 SHA-256 与 ZIP 解压文件哈希匹配，Windows 版本命令通过。ZIP SHA-256：`6F7D357C53DF0D47DCD66903D208868BAA6F13992F78E4CE03E60FA30F3F4E60`。
- 按用户此前授权，将 preview.48 文件更新到 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；目标目录 12 项载荷哈希与发行清单一致，目标程序报告 preview.48。未删除目录中的其他文件。
- 真实百度网盘、NAS/SMB、浏览器实机和长时间限流/重启场景仍待用户环境验收。

## v0.8.30-preview.47（扫描子目录队列插入复用语句）

- 全盘扫描最终目录页原先对每个子目录分别执行 `tx.Exec`；现在每页准备一次 INSERT 语句并在事务内复用。仍逐项读取 `RowsAffected`，保持重复路径忽略、pending_count 准确和页面失败整体回滚。
- 增加 101 子目录回归，覆盖队列顺序、重复路径不重复计数和失败事务回滚。SQLite 对照基准对比 100 个单条插入：重复准备约 34,698–34,822 B / 1,131 allocs，复用预编译语句约 33,378–33,406 B / 1,036 allocs；耗时均约 4.5–5.0 ms，不能据此宣称扫描墙钟时间提升。
- WebUI 22 项、userscript 3 项、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项包内 SHA-256 和 ZIP 解压后哈希匹配，Windows 版本命令通过。ZIP SHA-256：`AE1BA97EB3F143B875D038441418F908F08456C0CDC45915BD254251C1EA4BA5`。
- 按用户此前授权，更新发行文件到 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；目标程序报告 preview.47，二进制哈希与发布目录一致。
- 百度限流与 NAS/SMB 的长时间扫描耗时仍需真实环境测量。

## v0.8.30-preview.46（减少目录大小刷新重复查询）

- `CloudDirectorySize` 原本需要 5 条 SQL 分别读取目录状态、后代覆盖情况、文件大小/数量、子目录数量和观测时间。现在把目录状态、后代目录数、不完整目录数和最旧完整观测时间合并为一次索引范围聚合，文件统计单独保留，总查询降为 2 条。
- 新增 SQLite 查询计划回归，要求元数据聚合按 `(account_fingerprint,path)` 有界索引范围读取。现有测试继续覆盖目录完整性、账号隔离、重启读回、时间和相邻路径前缀。
- Windows 合成缓存包含 50,000 个子目录与 50,000 个文件。三轮每项 10 次的对照：旧查询约 71–77 ms、3,328–3,329 B、103 allocs；新查询约 52–53 ms、2,440–2,448 B、58 allocs。这是本机 SQLite 对照，不代表目标 NAS/SMB 性能。
- WebUI 22 项、userscript 3 项、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项 payload SHA-256、ZIP 解压后文件哈希和 Windows 版本检查通过。ZIP SHA-256：`EB07E398FA4353FFC0662C336FA07B56B5433320E0FB60A90C64450879AE016E`。
- 按用户此前授权，更新发行文件到 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；目标程序报告 preview.46，二进制哈希与发布目录一致。未覆盖目录中的其他文件。
- 目标 NAS/SMB 的目录大小刷新速度和实际百度数据一致性仍待用户实测；未进行真实百度删除/移动操作。

## v0.8.30-preview.45（网盘管理与路径设置整合验证）

- 汇总网页配置自动监控目录/本地下载目录及安全迁移、远端网盘 CRUD、全盘缓存与目录大小、重复内容和空目录清理的实施路线，见 [路线文档](cloud-management-roadmap.zh-CN.md)；详细设计与分项计划沿用现有 cloud-management-cache 文档。
- WebUI 22 项、userscript 3 项、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项包内 SHA-256 清单逐项匹配，Windows 二进制版本检查通过。ZIP SHA-256：`2A79E3B604E9C3BFAF39B94898BE6E0F6ABA7FB20F9819550C5FCBC57945AB8D`。
- 按用户此前授权，更新发行文件到 `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.24`；目标 Windows 二进制复核显示 preview.45，且与发布目录二进制 SHA-256 一致。没有复制或修改授权/队列数据。
- 百度真实账号、NAS/SMB、长时间限流、浏览器实机及远端清理尚未验收；本地 API/mock 和包校验不能替代这些证据。

## v0.8.30-preview.44（扫描待办计数常数时间读取）

- 修复 preview.43 的剩余 O(N) 状态轮询：`LoadCloudScanState` 现在经扫描状态主键直接读持久化 `pending_count`，不再对待办表 `COUNT(*)`。SQLite 查询计划回归确认只检索状态行，不触碰队列表。
- 每页清单提交在同一事务中按出队 1 项和实际插入的子目录行更新计数；计数与队头缺失/多余不一致时回滚，避免错误标记 complete。
- v13→v14 迁移回填旧队列计数；迁移提交后、manifest 发布前重启会验证状态计数与队列表相符，不一致时失败关闭。
- taskstore、downloader、intakeapi 全量测试、WebUI 22 项、userscript 3 项、全模块 Go 测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项包内哈希和 Windows 版本命令通过。ZIP SHA256：`13CEADB41E787FBC05F79A171E9C9A76D8803ADE50C1284187FD2CA84DA716FF`。
- 状态轮询的数据库读取现为固定行查询；目标 NAS 实测延迟、百度限流和长扫描耗时仍待验收。本预览未更新当前运行目录。

## D27：扫描待办计数常数时间读取（已交付 preview.44）

- preview.43 的返回体已固定大小，但数据库仍执行全队列 `COUNT(*)`；preview.44 将计数持久化到状态行并维护在事务中。
- v13→v14 迁移、查询计划、大队列、回滚及不一致失败关闭回归通过。
- 真实大队列 NAS 轮询延迟仍需用户环境测量。

## v0.8.30-preview.43（大目录队列状态回归）

- 在 preview.42 的持久化扫描队列实现上补充 10,000 项队列回归：SQLite 返回准确的 10,001 待扫计数；JSON 状态小于 1 KiB，且不包含待扫路径清单。
- 完整发布门禁（WebUI 22 项、userscript 3 项、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建）、12 项 payload SHA256 和 Windows 版本命令通过。ZIP SHA256：`AFAF341E96E45A07F38E2C6C5F450C60EAC755C8D21CE80F04E57A1BDB01B1E0`。
- 本版确认了大队列状态接口和持久化结构；真实百度请求频率、目标 NAS 写入性能和长扫描耗时仍待实测，未更新当前运行目录。

## v0.8.30-preview.42（全盘扫描持久化目录队列）

- 扫描目录待办从完整 `pending_json` 数组改为 SQLite FIFO 队列表；每页状态只保存当前目录、游标和计数，状态 API 不再返回云端路径数组，管理页按 `pending_count` 展示。
- 最终目录页、清单陈旧项清理、移除当前目录、加入子目录和更新页/条目计数都在一个 SQLite 事务内提交。中途页的清单写入与 offset 一起提交；失败时队列和进度不前移。
- v12→v13 原子回填旧 worklist 顺序；manifest 发布前重启会核对队列表与旧 JSON，错序或不一致失败关闭。暂停/续跑继续使用原目录和分页 offset；修正用户暂停恰逢 API 取消时被记为 failed 的状态。
- taskstore/downloader/intakeapi 定向与全量测试、WebUI 22 项、userscript 3 项、完整 Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建通过；12 项包内哈希及 Windows 版本命令通过。ZIP SHA256：`F490F1B27B215C5FAA8BDF0BAF65BFFD9F3300983E48800E95BF838CE21E3827`。
- 大队列状态返回由路径数组变为固定大小计数；目标 NAS 数据库写入和百度长扫描耗时仍需在真实环境测量。本预览未更新当前运行目录。

## v0.8.30-preview.41（重复大小计数缓存）

- SQLite `cloud_inventory_file_sizes` 按账号/大小维护文件计数；INSERT、DELETE 及文件类型/大小改变的触发器与目录清单同事务提交，目录本身不计为文件。
- v11→v12 原子迁移回填计数并安装触发器；提交窗口恢复会比对派生计数与源库存，完全一致才发布 manifest。
- 重复扫描在启动或恢复时计算一次候选总数，执行中的每个 100 项批次只按 fs_id 索引读取候选页，不再反复对全盘文件分组和计数。逐项身份复核、哈希验证与游标提交不变。
- 触发器、迁移恢复、分页和相关定向回归通过；完整发布门禁（Node、Go 全模块测试、vet、Windows/Linux AMD64/Linux ARM64 构建）通过，12 项包内 SHA256 与 Windows 包内版本命令通过。ZIP SHA256：`1E0AE5A4D94A0E75B563E9694463A5A2C690AE727A9A36B57C2A1052D961915E`。
- v0.8.30-preview.41 位于 releases 目录；本预览未更新当前运行目录。百度真实账号、NAS/SMB 大型缓存性能仍需用户环境验收。

## v0.8.30-preview.40（空文件夹预览使用路径范围）

- 空目录候选计算从递归物化全盘目录的祖先/后代组合，改为按现有 `(account_fingerprint,path)` B-tree 子树范围查找后代文件和未完成/未登记的后代目录，避免为整棵树创建递归闭包。
- 候选仍要求目录本身及全部后代缓存完整、子树没有文件，保留叶到根排序；远端操作仍逐个重新核验目录身份与实时空状态。
- 查询计划、完整/非空子树、缓存缺失/失效、相邻路径前缀边界回归通过；完整门禁（Node 25 项、Go 全模块测试、vet、三平台构建）、12 项 payload SHA256 和 Windows 包内版本命令通过。ZIP SHA256：`C389A0BCA466CA036E83EDBCDDA315C3E6121CCA4F8D5A93481B50B726C0B826`。
- 大型深层目录树在目标 NAS 上的预览时延仍待实测；没有更新当前运行目录。

## v0.8.30-preview.39（已验证重复组查询索引）

- 哈希扫描完成后展示重复组的 SQLite 查询原先需要临时 `GROUP BY` 排序。增加 `(account_fingerprint,sha256,size_bytes,fs_id)` 索引，并以哈希索引顺序连接当前文件身份和 complete 目录再分组。
- 新增 SQLite v10→v11 原子迁移与提交后 manifest 恢复；迁移前后保护哈希记录，当前身份与完整目录校验仍决定候选是否属于重复组。
- 原查询计划失败回归确认存在 `TEMP B-TREE FOR GROUP BY`；新计划验证、重复组内容和 v8/v9/v10 迁移定向测试通过。完整发布门禁（Node 25 项、Go 全模块测试、vet、三平台构建）、12 项包内 SHA256 与 Windows 版本命令通过。ZIP SHA256：`5E1EAE56647A5FDB94F4ABC64247AD3563EDD0331B951F3A39CCBF1BB1CADEED`。
- 大规模哈希集的目标 NAS 查询耗时仍需实测；未更新当前运行目录。

## v0.8.30-preview.38（重复候选查询复用大小分组）

- 将重复候选总数与 fs_id 分页合并为一条 SQLite 查询，MATERIALIZED 大小分组 CTE 同时供总数与候选页读取，避免每批重复 GROUP BY。
- EXPLAIN 回归确认一次分组物化、使用专用大小索引且没有临时分组 B-tree；现有完整目录约束、稳定分页、空页总数、批次重试与逐项进度语义保持不变。
- 完整门禁（Node 25 项、Go 全模块测试、vet、三平台构建）、12 项包内 SHA256 和 Windows 版本命令通过。ZIP SHA256：`CE8AB8E3E36AEAA9B89C9D9D2B46B69BA6D7E2ED200ECB8B1D8102E8662F959A`。
- 目标 NAS/SMB 上大缓存实测仍待验收；未更新当前运行目录。

## v0.8.30-preview.37（重复内容扫描分批获取候选）

- 重复内容扫描按 `fs_id` 每次读取最多 100 个候选，只在当前缓存批次耗尽后重跑全盘大小分组查询；减少每个文件重复统计全盘的数据库工作。
- 批次仅在内存中缓存。每个文件仍需在线身份复核、完整下载内容哈希验证，并在成功后单独耐久提交扫描游标；限流退避保留当前候选，重启从最后提交的 `fs_id` 重新取数。
- 235 个候选 100/100/35 边界、限流重试、持久化退避恢复和重复项远端删除复核相关测试通过；完整门禁（Node 25 项、Go 全模块测试、vet、三平台构建）、12 项包内 SHA256 和 Windows 版本命令通过。ZIP SHA256：`2E45F3DF4FA369EE9654A8B249BFCB17E43BE954D548F439C35315F728621856`。
- 目标 NAS/SMB 查询次数与真实百度长扫描耗时仍待验收；未更新当前运行目录。

## v0.8.30-preview.36（重复内容扫描大小分组索引）

- 全盘缓存增加 `(account_fingerprint,is_dir,size_bytes,fs_id)` 索引，重复内容候选分组不再需要为大小列建立临时排序树；扫描仍只处理完整目录，并按 fs_id 稳定分页。
- 新增 SQLite v9→v10 事务迁移、旧版迁移链以及提交后 manifest 更新前重启恢复；修正 schema 必需对象计数，避免新索引接入后存储启动误报 schema incomplete。
- EXPLAIN、候选分页、v9 数据保留、提交窗口恢复和 v6/v8 迁移定向回归通过；完整发布门禁（Node 25 项、Go 全模块测试、vet、三平台构建）、12 项包内 SHA256 和 Windows 包内版本命令均通过。ZIP SHA256：`6A1C254B0D9717EA0D8EA823FAB589E4E151ABA1F9EC7B9E1E098360A7070B4C`。
- 目标 NAS/SMB 上全盘分组耗时仍需实测；预览包位于发布目录，未更新当前运行目录。

## v0.8.30-preview.35（复用全盘缓存页 SQLite 写入语句）

- 每个云端目录页仍在一个事务中原子写入；文件记录和子目录状态改为事务内复用预编译语句，减少重复 statement 准备成本，不改变分页、提交点和断点恢复粒度。
- Windows AMD64 Go 1.27.1 `BenchmarkUpsertCloudInventoryPage100` 2 秒样本：修改前约 11.85 ms/op、75,261 B/op、1,621 allocs/op；修改后约 9.90 ms/op、74,238 B/op、1,531 allocs/op。一次本机前后测量约快 16.5%，SQLite 同步写与文件系统波动仍主导，不能外推目标 NAS。
- `go test ./internal/taskstore -count=1` 通过；完整发布门禁、三平台构建与包内清单校验见发布目录 `v0.8.30-preview.35`。

## v0.8.30-preview.34（网盘目录分页复合索引与 v9 迁移）

- 将缓存父目录索引升级为 `(account_fingerprint,parent_path,name,fs_id)`，与 `ListCloudInventory` 的过滤和稳定排序一致；SQLite 查询计划回归确认直接使用该索引且没有临时排序树。
- 新增 v8→v9 SQLite 事务迁移。若进程在数据库提交之后、manifest 发布之前退出，重启会校验 scope 与新旧索引状态后完成 manifest；不符合已提交状态时失败关闭。迁移集成用例确认目录缓存仍可读取。
- 真实大目录/NAS 查询时延需在目标设备测量。独立预览包：`v0.8.30-preview.34.zip`；全模块测试、vet、三平台构建与包内清单校验见发布目录 `v0.8.30-preview.34`。

## v0.8.30-preview.33（网盘目录大小使用 SQLite 路径范围索引）

- 目录大小统计、完整性/时间查询、缓存失效与失效子树清理改用路径前缀范围条件，复用既有 `(account_fingerprint,path)` 索引；避免在大账号缓存中因 `substr(path,...)` 条件扫描全部路径记录。
- 新增 SQLite `EXPLAIN QUERY PLAN` 回归确认文件大小聚合按路径索引范围查找，并覆盖 `/docs` 不匹配相邻 `/docs0`、失效 `/docs` 不会污染 `/docs0` 的边界。
- 无需新增数据库迁移。独立预览包：`v0.8.30-preview.33.zip`；全模块测试、vet、三平台构建与包内载荷校验见发布目录 `v0.8.30-preview.33`。

## v0.8.30-preview.32（串行化网盘缓存页提交与远端修改）

- 修复目录扫描与网页远端改名/移动/删除/新建之间的缓存竞态：单页数据提交 SQLite 前继续持有网盘读锁，远端写操作的独占锁只有在该页提交后才能执行，随后失效受影响目录缓存。旧扫描结果不再能覆盖较新的 stale 标记。
- 新增确定性交错回归：暂停扫描页提交并发起远端改名，证明改名在页提交前不能发送，操作完成后的目录缓存保持 stale。真实百度、NAS/SMB 并发时延仍待实测。
- 独立预览包：`v0.8.30-preview.32.zip`；全模块测试、vet、三平台构建与包内载荷校验见发布目录 `v0.8.30-preview.32`。

## v0.8.30-preview.21（认证恢复取消过期重试截止时间）

- 任务从 `pending`/`retry_wait` 进入 `auth_wait` 时，清除原退避截止时间；通过新凭证和成功云端扫描恢复为 `pending` 时，也清理旧版本可能留存的截止时间，避免已授权任务仍被旧退避计划延迟。
- 认证等待事件记录被取消的 UTC 重试计划时间；扫描恢复事件仅在发现旧截止时间时记录其清理值，保持本机事件记录与未来计划时间的语义区分。
- RED/GREEN 回归证明原行为会将任务延迟 30 秒复核（原定截止约一小时后），修复后成功扫描即可立即认领。完整门禁与独立包验证见 `v0.8.30-preview.21-validation.md`。

## v0.8.30-preview.20（记录源身份复核的实际云端元数据）

- 改名或删除前的 `filemetas` 复核若发现同一 `fs_id` 元数据已变化，`source_identity_changed` 事件现在并列保存任务预期元数据与本次响应观察到的实际元数据；若云端已无此 `fs_id`，明确记录 `missing=true`。
- 元数据复核结果写入 `response_observed_at_utc`，不再误记为目录扫描时间或事件发生时间。不会保存访问令牌、原始错误文本，也不改变身份复核和源项隔离行为。
- 回归覆盖同 ID 内容变化、ID 缺失及不执行源操作；本机模拟 provider 证明事件证据和时间类别，不代表真实百度接口语义或消除核对到操作之间的竞态。

## v0.8.30-preview.19（迁移数据库无覆盖写通发布）

- 新增 `durablefs.PublishNoReplace`，统一 Windows 最终下载文件和 v1 迁移 SQLite 数据库发布路径。Windows 使用不带 `REPLACE_EXISTING` 的 `MoveFileExW(MOVEFILE_WRITE_THROUGH)`；Linux 保持 `renameat2(RENAME_NOREPLACE)`；其他平台继续失败关闭。
- RED/GREEN 覆盖发布成功后源项移走、目标已存在时拒绝覆盖且保留两边证据，以及 Windows API 标志；v1 迁移仍通过现有等价验证和清单切换回归。
- 迁移发布后才原子替换 SQLite 版本清单；Windows 写通请求不替代目录创建/删除 fsync，也不证明 NAS/SMB 或设备断电耐久。

## v0.8.30-preview.18（Windows 原子状态替换请求写通）

- 新增 `durablefs.ReplaceFile`，并用于 taskstore 原子 JSON、下载部分文件元数据和批量原子记录发布。Windows 通过 `MoveFileExW(REPLACE_EXISTING|WRITE_THROUGH)` 更新；其他平台继续调用 `os.Rename`，Linux 既有目录同步行为不变。
- 通用回归覆盖原子替换既有文件；Windows 专项回归核验替换/写通两个标志并检查实际结果。全模块 Windows Go 测试和 `go vet` 通过。
- Windows 对原子文件替换和文件发布请求写通，但目录创建/删除仍没有目录 fsync；NAS/SMB 与物理掉电耐久仍需目标环境实测。

## v0.8.30-preview.17（Windows 文件发布请求写通完成）

- Windows 无覆盖原子发布由 `MoveFile` 改为 `MoveFileExW` 并传入 `MOVEFILE_WRITE_THROUGH`。未传 `MOVEFILE_REPLACE_EXISTING`，已有目标仍拒绝覆盖；修改只影响 Windows 发布操作，不改变 Linux `renameat2(RENAME_NOREPLACE)` 路径。
- 专项 Windows 回归拦截 API 调用，确认只传写通标志，并实际核对源文件移走、目标内容正确；现有下载回退目标冲突测试通过。
- 此选项请求 Windows 等待移动落盘完成；不等同于对所有文件系统、NAS 控制器或设备缓存的物理断电保证。目标 NAS/SMB 仍需实机验证。

## v0.8.30-preview.16（SQLite 每次重连重新应用耐久 PRAGMA）

- SQLite `busy_timeout`、`foreign_keys`、`synchronous` 原先只在数据库首次取用连接后设置一次；若 `database/sql` 因 I/O 错误丢弃该连接，后续物理连接会回到 SQLite 默认值。现在通过 `modernc.org/sqlite` 的每连接 `_pragma` DSN 重复应用这些连接级设置，并在每次新连接时将 journal mode 设回 DELETE。启动仍核验实际值，偏离即失败关闭。
- 回归先失败后通过：测试把连接池空闲连接上限设为 0，强制每次查询新建连接，确认 `synchronous=3`、`foreign_keys=1`、`busy_timeout=5000`；另先切换 WAL 再丢弃连接，确认新连接重新使用 DELETE。SQLite 文件位于带空格目录的场景也通过。

## v0.8.30-preview.15（区分登记意图与已拥有的复制临时文件）

- 复制回退先把随机临时 basename 作为“尚未创建/拥有”的意图写入 `partial.json`，再用独占创建；创建成功后先耐久标记为本程序拥有，才开始复制。重启时只清理已标记拥有且位于目标目录的普通文件；仅有意图标记时只清除登记，不碰磁盘上的同名文件。
- 若进程在独占创建成功后、拥有标记耐久写入前被强制终止，可能留下空临时文件；恢复会保留该未确认归属文件以避免误删。此边界优先保护用户文件，之后可人工清理该随机命名残留。

## v0.8.30-preview.14（恢复时清理已登记的复制发布临时文件）

- 复制回退在建立目标目录临时文件后，将其随机 basename 先写入该任务 `partial.json`；复制中断后，重试只清理登记的同目录普通文件、同步该目录，再清除登记字段，之后重新尝试发布。复制已完成但重命名/目录同步/清理流程中断时也能清除登记状态。不会扫描整个下载树。
- 新增回归覆盖中断副本清理、耐久清除文件名记录、拒绝路径穿越并保留外部文件，以及损坏的部分文件元数据不得被新下载覆盖。清理时身份不符、目标目录同步失败或记录名称非法均失败关闭。
- 完整发布门禁及独立包验证见 `v0.8.30-preview.14-validation.md`。

## v0.8.30-preview.13（硬链接失败时原子复制发布）

- 下载发布仍优先使用同卷硬链接。硬链接失败且目标不存在时，复制到目标目录的临时文件，复制中校验预期长度和 SHA256，Sync 后以 Linux `renameat2(RENAME_NOREPLACE)` 或 Windows `MoveFile` 原子发布；不会覆盖已存在目标。发布成功后仍同步目标目录，再删除 `.nas-downloader` 中的源部分文件。
- 进程若在副本发布后、源部分文件清理前退出，重启路径会核对云端身份记录和部分文件哈希，只清理已验证最终文件的等内容副本；不同内容或元数据不符的部分文件保留。
- 回退会增加最多约一个文件大小的瞬时空间与本地复制 I/O；NAS/SMB、FUSE、exFAT 等底层对 no-replace rename 和目录同步的支持仍待实机验证。详见 `v0.8.30-preview.13-validation.md`。

## v0.8.30-preview.12（补充鉴权等待原因事件）

- 任务由 `pending`/`retry_wait` 进入 `auth_wait` 时，追加独立事件并记录稳定原因：凭证未就绪、已过期、不可用或被提供方拒绝。对于提供方拒绝，记录错误类别、操作名、HTTP 状态/错误码及本机响应观察时间，不保存 Token、指纹或原始错误文本。
- RED/GREEN 回归覆盖本地凭证未就绪不虚构线上时间，以及模拟 401 响应记录安全提供方证据；无 Token 泄漏。
- 完整门禁与独立包验证记录见 `v0.8.30-preview.12-validation.md`。

## v0.8.30-preview.11（补充源项操作失败事件证据）

- 源项改名/删除的完成、未知和可重试失败事件继续携带原有文件身份摘要；现补充安全的 `error_class`，以及百度 API 错误可确认时的操作名、HTTP 状态和错误码。只在有 HTTP 状态证据时记录本机响应观察时间；不把它当作百度端实际发生时间。
- 不写原始错误文本、请求 URL 或凭证。下载事件复用同一通用错误分类器，分类规则与既有结果保持一致。
- 回归覆盖可重试删除失败的结构化事件字段、响应观察时间与不写原始错误文本；下载错误分类复用回归保持一致。完整门禁与独立包验证记录见 `v0.8.30-preview.11-validation.md`。

## v0.8.30-preview.10（修复时钟前调后重试候选重新入延迟堆）

- 延续 preview.9 的墙上时钟截止处理，并让到期任务提升和候选重新分类使用同一个时间快照。确定性模拟时钟前调时，新回归发现旧提升流程会在“按模拟时间取出、按实际时间重新入队”之间无限循环；现直接把该任务提升到 ready 堆，再由真实 worker 启动执行。
- 增加 `claimAt` 时间入口供调度器对指定墙上时间做决策验证，不影响生产入口；回归覆盖先等待上限、再将墙上时间推进到截止之后并立即认领。
- 完整测试与新独立预览包验证见 `v0.8.30-preview.10-validation.md`。未修改系统时间；未在 NAS/SMB、真实百度账号或目标浏览器验收。

## v0.8.30-preview.9（重试等待期间重新校准墙上时钟截止时间）

- 新重试截止用 UTC 创建，去掉只能在当前进程比较的单调时钟读数；长期等待的 worker 最多睡 30 秒，再读取持久化 `NextAttempt` 并按墙上时钟重新计算。系统时间向前调整后，不再固定等完初始相对时长；短等待仍直接睡到截止时间附近。重试次数与退避规则不变。
- TDD：长截止回归先观察到 1 分钟截止任务返回 1 分钟完整等待；UTC 持久化回归先观察到截止值使用 `Local` 时区；增加复核间隔和 UTC 创建后两项及近截止等待均通过。
- 独立预览包和完整验证结果记录在 `v0.8.30-preview.9-validation.md`。这项行为用本地可观察等待时长验证，没有通过真实系统时钟跳变或 NAS/SMB 实测。

## v0.8.30-preview.8（验证 SQLite 未提交事务的进程崩溃恢复）

- 新增真实子进程故障回归：子进程在任务投影与事件事务均已写入但尚未提交时暂停；父进程确认 SQLite 回滚日志存在后强制终止它，再重新打开队列核对既有投影和事件仍在、未提交的新投影和事件均未出现。
- 该验证覆盖本机 SQLite 进程异常终止及回滚日志恢复，不等同于 NAS/SMB 断电、设备缓存刷盘或锁语义验收。
- 独立预览包及测试记录将在 `v0.8.30-preview.8-validation.md` 记录；真实百度、目标 NAS/SMB、目标浏览器及 race detector 仍未验证。

## v0.8.30-preview.7（启动时清理崩溃遗留原子写临时文件）

- taskstore 成功取得队列单写锁后，启动时清理队列根目录、`jobs` 和 `intake` 中符合 `record-*.tmp` 命名的普通文件。此类文件是原子 JSON 写入的非权威临时文件，进程崩溃后可遗留；若删除暂时失败，下次启动会重试，不影响权威记录恢复。
- 先失败再修复的回归覆盖三处目录清理、相似但不匹配的普通文件/目录保留，以及另一个实例持锁时不得提前清理其临时文件。
- 完整 Go 测试已通过；独立包与 SQLite/HTTP 启动验证结果记录在 `v0.8.30-preview.7-validation.md`。真实百度、NAS/SMB 断电耐久、race detector 和目标浏览器仍未验证。

## v0.8.30-preview.6（事件中区分下载短流）

- `io.ErrUnexpectedEOF` 事件新增 `error_class=incomplete_stream`，与普通网络错误、网络超时、文件系统错误及提供方 API 错误区分；不写原始错误文本，不改既有重试策略。
- RED/GREEN 回归先观察到短流只记为 `other_error`，修复后验证类别准确，且事件没有写入 `unexpected EOF` 原文。
- 独立包及验证记录：`v0.8.30-preview.6-validation.md`。真实百度、NAS/SMB、实机浏览器和 race detector 仍待验证。

## v0.8.30-preview.5（重试时清理已发布文件的残留硬链接）

- 如果最终文件和完成凭据已经发布，但临时 `content.part` 硬链接删除暂时失败，后续重试在重新验证最终文件、目录同步成功后，仅删除经 `os.SameFile` 确认为同一文件的残留名称；cleanup 失败不影响已验证的最终文件，其他部分文件不删除。
- RED/GREEN 覆盖模拟第一次删除失败后重试清理，以及确保身份不同的部分文件不会被清理。
- 独立包、哈希和包内冒烟在 `v0.8.30-preview.5-validation.md` 记录；NAS 突然断电仍未验证。

## v0.8.30-preview.4（区分网络超时事件类别）

- 下载尝试事件现在把 `net.Error.Timeout()` 明确记为 `network_timeout`，普通网络错误仍为 `network_error`；不保存原始错误/URL，也不改变下载器现有重试判断。
- 回归先观察到超时被记为普通 `network_error` 而失败，修复后断言 `network_timeout` 且事件证据不含模拟 URL 和 Token 参数。错误证据仍只使用稳定分类。
- 独立预览包与哈希验证将在 `v0.8.30-preview.4-validation.md` 记录；真实网络、百度/NAS 和目标浏览器仍待环境验收。

## v0.8.30-preview.3（启动恢复按 SQLite 主键顺序读取）

- 通用且已验证 JSON 的 `LoadEach` 保持兼容顺序；下载器启动专用 `LoadEachRaw` 改按 SQLite 整数主键升序扫描，避免对 100k 行计算旧 JSON 文件名表达式并排序。回归使用 ID 2、10、1 先观察到旧顺序 `[1,10,2]`，再验证主键顺序 `[1,2,10]`。
- Windows AMD64 Go 1.27.1 合成 100,000 根任务，`OpenDownloader` 单轮 3 次中位数由改动前约 1.182 s 降至改动后约 0.600 s（约 49%）；前/后 B/op 均约 111 MB、allocs/op 均约 902k。运行样本受本机 I/O 波动影响，作为方向性结果，仍不能外推 NAS/SMB。
- taskstore/downloader 聚焦测试已通过；完整门禁、三平台包与载荷摘要见发布验证记录。目标 NAS/SMB 与长期运行仍待实际环境验证。

## v0.8.30-preview.2（任务事件不暴露原始下载错误）

- v0.8.30-preview.1 已被取代，不要用于测试：其下载结束事件曾把原始错误文本写入 SQLite 事件证据，可能包含本机路径或网络地址。
- 修正版事件只写 `error_class` 稳定类别（文件系统、网络、提供方 API、取消、超时或其他），提供方 operation/HTTP 状态/错误码继续使用独立结构字段；原始错误文本和路径不进入事件 JSON。任务当前状态的既有错误展示与重试行为不变。
- 回归先因事件泄漏敏感路径且缺少安全类别而失败；修复后验证类别可读且事件中不含原始路径/错误文本。完整 Go 测试、`go vet`、WebUI 16 项及 userscript 3 项通过。首次并行跑全量测试有一回调度包失败，独立重跑和再次全量串行重跑均通过。
- 独立预览包与载荷校验和见同目录 `v0.8.30-preview.2-validation.md`；race detector、真实百度/NAS/SMB、实机浏览器/油猴仍未验证。

## v0.8.29-preview.1（消除重启时重复 SQLite 完整性扫描）

- 旧 SQLite 格式启动先单独连接执行一次全库 `integrity_check`，再由正式连接重复检查一次。现拆分建库与已有库验证：既有权威库先核对 schema、版本和 scope，不做 schema 补建，再设置/核实 pragma 并执行一次完整性扫描；迁移 staging 库重开也用不创建 schema 的路径。
- TDD：新回归在现有库缺 `job_events` 时先因 existing-open 接口缺失编译失败；实现后验证启动失败且缺表仍缺失。已有权威库损坏失败关闭测试继续通过。
- Windows AMD64 Go 1.27.1、Intel Core Ultra 7 265KF、100k 合成历史 `Store.Load` 端到端：改动前 3 次中位数 613.1 ms，改动后 5 次中位数 475.9 ms，方向性改善约 22%；B/op 和 alloc/op 基本不变。此本机基准不代表 NAS/SMB。
- 发布门禁：Windows 全模块 Go 测试/vet、WebUI 16 项、userscript 3 项、三平台无 CGO 构建通过。ZIP 有 12 项、11 个 payload 哈希一致。包内 Windows 同一临时队列连续两次启动，health=`waiting_authorization`，诊断从 `created` 变为 `reopened`，完整性和 pragma 正确。
- 全量 race 未验证：Windows cgo 子进程退出码 2，WSL `E_ACCESSDENIED`。真实 NAS/SMB 耐久与性能、百度线上链路、实机浏览器仍待目标环境。
- 独立包：[v0.8.29-preview.1.zip](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.29-preview.1.zip)，SHA256：`639BB905DDD91A8EE4D621D41862A2900371D73E93D9A34F215C7D456A53EAF5`；验证记录：[v0.8.29-preview.1-validation.md](D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.29-preview.1-validation.md)。源码提交：`e2111c28e833c96bc11e43836ae5c8fee1520a45`。

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

## 2026-11-01：v0.8.30-preview.118 全盘与局部目录分页漂移自动重核

- 完成 SQLite schema 31：两类只追加事件支持 `boundary_restart`，迁移保留历史 sequence、索引和触发器，并建立部分索引供 run/目录次数查询。
- 全盘扫描和局部刷新在检测页边界漂移后，使用 SQLite 事务核对同账号/run/目录重核次数、复位 offset/boundary、维护对应缓存标记或 staging，并写入 UTC 事件。最多自动重核两次；额度预留仍先于每个百度 GET。
- 状态 API 从事件记录派生当前目录重核数；云管理页展示 N/2 和历史事件中文标签。失败注入验证状态与事件不会部分提交；旧缓存仅在完整重读后才删除或发布。
- 验收：`go test ./... -count=1`、`go vet ./...`、WebUI 126/126、userscript 3/3、三平台构建通过。首次发布门禁遇到一项重试期限测试的单次超时；Windows/AMD64/CGO=0 下连续复跑 12 次通过，完整发布脚本随后重跑通过。
- 预览 ZIP：`v0.8.30-preview.118.zip`；最终 SHA-256 与 ZIP 内容、源码快照检查详见 releases 同级 validation 记录。该版只验证本机 SQLite/mock/API/UI 和构建，未用真实百度账号或 NAS/SMB 验收。

## 2026-11-01：v0.8.30-preview.119 局部目录刷新历史与请求额度审计（已交付）

- 完成局部刷新运行摘要投影与 schema 32 迁移；从历史事件回填可证明的范围和当前摘要，保留旧事件序号、索引与只追加触发器。
- 请求额度预留和运行摘要在同一 SQLite 事务更新；状态/列表/证据导出使用持久化摘要。
- 新增 `retry_scheduled`、`request_budget_reached` 关键事件；记录重试 UTC 截止、额度追加量，并在事件和证据导出中带出错误摘要。
- 定向 taskstore 迁移/事件/证据导出、局部刷新重试回归通过；WebUI 126/126、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建均通过。发布 ZIP 13 项、12 项载荷哈希匹配、406 项源码快照，无私有运行数据；验证记录见 releases 同级 `v0.8.30-preview.119-validation.md`。
- 未访问真实百度账号、未执行远端写操作，NAS/SMB 未验收。

## 2026-11-02：v0.8.30-preview.144 下载任务事件筛选与证据导出

- 任务事件支持按固定已持久化事件类型和本机 `recorded_at_utc` UTC 日历范围筛选；历史登记、状态变化、旧队列导入、身份变化及网盘源操作结果均可选择。
- SQLite 只读事务在同一快照中读取匹配总数与稳定 sequence 页；筛选贯穿新旧分页和刷新。用户显式导出当前筛选，最多 100 条，超过上限不返回部分证据，并保留敏感字段遮蔽。
- Store/API 主包定向回归通过；WebUI 165/165、userscript 3/3、全仓 Go 测试和 `go vet ./...` 通过。发布脚本三平台构建、ZIP 校验和最终源码快照见 releases 同级 `v0.8.30-preview.144-validation.md`。
- 功能只操作本地 SQLite 读取，不调用百度 API、不改变任务状态。真实账号只读检查与运行目录切换结果见 validation；未进行任何百度远端写操作或 NAS/SMB 验收。

## 2026-11-02：v0.8.30-preview.145 任务事件关联 ID 筛选

- SQLite 任务事件查询新增参数化 `operation_id`、`attempt_id` 精确条件；与任务 ID、事件类型和本机记录 UTC 日期按 AND 组合，在同一只读事务中计算命中数、稳定 sequence 页或有界导出；不新增 schema。
- 管理页面支持手工输入两个 ID，也可从事件卡片一键聚焦；聚焦保留类型/日期并清除另一种 ID。刷新、新旧分页、命中数、显式导出继续沿用全部筛选条件，迟到响应保护不变。
- 校验 ID 为有效 UTF-8、1..128 字节、无首尾空白/控制字符；重复、未知或无效 query 拒绝为 400；参数化 SQL 精确匹配，0 命中返回空页；404/413/503 边界覆盖。
- 发布门禁：WebUI 166/166、userscript 3/3、全仓 `go test ./... -count=1`、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP/SHA/source snapshot 检查通过。包哈希、状态目录切换和验收边界见 releases 同级 `v0.8.30-preview.145-validation.md`。
- 未执行百度云端写操作、下载、大规模扫描或 NAS/SMB 验收。

## 2026-11-02：v0.8.30-preview.146 任务事件调查工作台

- 任务事件过滤支持本机记录 UTC 精确时间左闭右开范围；旧 UTC 日期范围兼容，两类时间条件互斥，分页和证据导出保持相同精确值。精度不得超过 SQLite 微秒分辨率。
- 事件页在同一只读 SQLite 事务返回当前筛选总数、分页和忽略事件类型条件后的固定事件类型计数；未知类型合并为“other”，不向调用方泄露未知原始类型。
- 增加任务 ID + sequence 锚点上下文接口，固定返回前 5、锚点、后 5 条；跨任务/不存在的序号 404。列表页与上下文 API 统一使用递归脱敏，事件列表响应设置 no-store。
- 任务事件页面支持 UTC 当日快捷筛选（保留类型和关联 ID，清除精确时间条件）、类型计数快捷筛选、上下文折叠、复制服务端脱敏单条 JSON；刷新/翻页会使旧上下文请求失效，复制权限错误明确提示。
- 验收：全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 169/169、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP/SHA/source snapshot 通过。唯一真实账号状态从 `.145` 移至 `.146`，源目录消失且目标状态哈希一致；真实 health/OAuth/本地页面通过，未手动发起全盘扫描、下载或远端写操作。哈希与验证边界见 releases 同级 `v0.8.30-preview.146-validation.md`。

## 2026-11-02：v0.8.30-preview.147 跨任务全局事件历史

- 新增管理员保护的全局事件页及只读 API，可跨任务按 task ID、事件类型、operation ID、attempt ID、本机记录 UTC 日期或精确 UTC 左闭右开范围组合筛选；总数、类型计数、分页从同一 SQLite 只读事务读取，未知类型归并为 `other`。
- 用全局 sequence 上界固定分页和导出快照，前端以字符串传递 int64；特别覆盖空历史 `as_of_sequence=0`，后续出现事件时仍不会被加入原快照。递归脱敏导出最多 1,000 条，超限整份拒绝。
- SQLite schema v38→v39 只新增事件查询索引。迁移后启动恢复正常，历史表不重写。
- 验收：全仓 Go 测试/vet、WebUI 172/172、userscript 3/3、三平台构建；ZIP 13 项、12 项 payload SHA 匹配、489 行来源快照。真实 `.nas-downloader` 从 `.146` 移动到 `.147`，43 个文件逐项哈希一致且源目录消失；本机登录、OAuth 状态、空队列、页面/API 和空快照导出通过。未执行百度列表请求、全盘扫描、下载或远端写操作，NAS/SMB 未验收。ZIP 哈希与详细证据见 releases 同级 `v0.8.30-preview.147-validation.md`。


## 2026-11-02：v0.8.30-preview.148 局部刷新分页漂移证据

- 完成五项合并交付：provider/staging 重复 `fs_id` 通过类型化错误恢复身份并在局部刷新中从第一页重核；原 `offset` 和可确定的重复 `fs_id` 与 append-only 事件同事务持久化；刷新历史新增固定原因筛选并保持分页；事件页显示结构化证据；整轮 JSON 导出同步包含新字段且不导出账号指纹。
- SQLite schema v39→v40 仅增加 nullable `restart_offset` 与 `restart_fs_id`。旧历史保持 NULL、不推测过去；迁移按列幂等、保留序号和 append-only 触发器，并检查 integrity。重核次数上限、列表请求预留、旧缓存直到完整发布的边界未改变。
- 验证：定向 provider/staging 类型化错误、SQLite 迁移正常/提交后清单恢复、过滤/导出和 append-only 回归通过；分页漂移 10 组合场景通过。全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 172/172、userscript 3/3 通过；Windows AMD64/Linux AMD64/Linux ARM64 打包通过。
- 真实状态目录只保留一份：停止 `.147` 进程后，将唯一 `.nas-downloader` 从 `.147` 移动到 `.148`；43 个文件逐项 SHA-256 一致，源目录消失。`.148` 从 `Start.ps1` 启动；管理员登录、health `ok`、OAuth ready、队列 0、队列健康、SQLite v40 `integrity_check=ok`、管理页 HTTP 200、原因筛选 API HTTP 200。
- 程序内置自动监控启动后按 30 秒周期对 `/NAS_Download_Queue_preview` 执行只读列表核对，SQLite 最近 5 轮均成功；未手动启动全盘扫描、下载或网盘写入。当前网页显示本地下载路径 `D:\projects\Git-Projects\downloader_auto_baidu`，保留原状态中的路径设置。NAS/SMB 未验收。
- ZIP 载荷 13 项、12 项 payload SHA 匹配、来源快照 492 行；ZIP SHA-256 见 releases 同级 `v0.8.30-preview.148-validation.md`。
