# v0.8.31 跨运行哈希扫描最近事件迁移

旧页能力盘点确认缺口，见docs/ai/v0.8.31-legacy-cloud-parity.zh-CN.md。复用现有GET /api/cloud-cleanup/duplicate-scan/events本地后端，在identity_format=string模式复用hashHistoryEvents的字符串序号/游标及脱敏evidence_json原文；run_id为空表示跨运行，不猜造固定运行关联。默认旧events/numeric/null响应原样保留。精确请求只允许format/limit/before_sequence，规范游标，不增加provider读取和SQLite表。

先接口红绿验证超过2^53序号、嵌套大整数原文、敏感字段遮罩、旧兼容、异常格式/重复参数/规范游标与零provider。然后在hash历史页接入按需最近事件独立组件，20条，逆序页边界与真实run_id/原始UTC展示，legacy ID允许显示但不得伪造有效固定run链接；失败保留历史并禁用旧分页，401/离页/BFCache/迟到响应隔离，不自动扫描。JSON原文以pre插值防HTML。专项/全回归/独立复核后浏览器和同版发布，保持当前安装未更新直至全项完成。

## 实施记录

- 先新增Go红测：确认旧端点忽略string格式、数字游标导致精度协议不符；实施精确分支复用已脱敏hashHistoryEvents后，新增2项及intakeapi全包通过。默认旧events和numeric/null响应保持，精确query拒绝未知/重复/非规范游标，no-store。
- 新增HashRecentEvents.vue与hash历史接线。实际构建红测确认面板缺失后实现；按需20项倒序页、较新/更早页、旧run原值、有效ID仅按标识导航、原文pre安全插值、历史失败/401/离页/BFCache迟到响应隔离。
- 新增7项页面行为回归、历史页35/35通过，类型检查和Vite构建通过，完整前端及独立复核进行中。
- 不新增provider/扫描/SQLite，不触碰真实服务账号；当前安装仍04:19:39两轮比较构建，本项不可单凭源码视为交付。

- 全量前端549/549通过（39.950s），未并发重建产物；独立复核尚待回报，浏览器和发布未开始。

- 独立只读复核无确定缺陷，补齐成功翻页/返回、恢复后历史禁页、JSON解码迟到隔离与递归脱敏嵌套大整数验证；最新历史页37/37，Go专项通过（新增UI共9项）。第二轮全量551/551通过（39.280s）。
- 清单已按源码/安装两个口径更新，不将本工作包写成当前已安装，也不删除兼容页；浏览器与同版打包留待下阶段完成。

- IAB真实浏览器仅mock：初始无事件表；按需读取精确序号/嵌套大整数/纳秒UTC/REDACTED，HTML片段安全显示（img数0）；更早页旧run原文无伪链接、较新页返回通过。390x844时页面375/375，表格246/740局部滚动。截图失败，完整视觉未通过；viewport/tab恢复，精确mock PID停止，未启动真实账号服务。
