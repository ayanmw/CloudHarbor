# .165 扫描历史工作台实施计划

> 使用superpowers:executing-plans在原目录内联、逐项TDD；仅最后一个fresh-context整体review。用户已有常规实施/打包授权，不worktree/自动提交/push。

Goal：将三类扫描的本地运行、筛选、精确分页、固定事件证据及导出合并为统一工作台。
Architecture：CloudHistoryWorkspace负责类型、auth与导航；CloudHistoryRuns负责筛选/表格/分页；CloudHistoryEvents绑定固定source+run；CloudHistoryExport绑定运行与条件。cloud-history-route/types处理明确的三类协议，不造通用历史框架。Go最小可选字符串DTO/证据原文投影，不改历史数据/schema。
Tech：既有Vue3/TypeScript/Vite、Go、SQLite，无新依赖。
Spec：docs/superpowers/specs/2026-11-04-cloud-history-165.zh-CN.md。

## 全局约束

只读本地、无百度请求或扫描副作用。nullable历史/运行与worker/原UTC与本机UTC分开；int64精度；query白名单兼容；旧高级能力保留；失败/401/离页/BFCache/迟到隔离。冻结.164包不重建。

## Tasks

- [x] Task1：逐项核实三类runs/events/exports DTO和query/分页/枚举，精确协议及脱敏先RED→GREEN；独立/cloud/history保护路由与三类历史表格骨架。
- [x] Task2：状态/UTC日期/已有覆盖筛选，apply/clear/URL条件恢复；切类型清不兼容条件，先错误/重复/边界参数回归再接UI。
- [x] Task3：精确稳定游标、前后页与URL回退、固定run深链；变化/空页/读失败/迟到回归先行。
- [x] Task4：固定source+run的事件分页与完整原因/nullable覆盖证据、脱敏JSON原文；跨运行/类型/认证/BFCache隔离，明确历史未记录。
- [x] Task5：单轮及已支持类型的筛选摘要导出、413/401/精度/脱敏校验；一个最终review、可见UI及完整发布门禁后发.165。

## Ledger

.164最终整版已验证：前端278/278、legacy172/172、userscript3/3、完整Go/vet/三平台、ZIP13文件/12载荷/686源码快照与架构。临时脚本清理后同版重包并校验，最终SHA见spec；旧包冻结。本批仅新建规划，尚无产品代码或测试包。
Pre-flight: Task1协议→Task2过滤与Task3游标需类型区分；full/refresh使用before_sequence，hash runs使用before_start_sequence，hash事件是/runs/{id}/events，不可硬套同一URL。Task4显示依赖现有完整证据字段，.163精简DTO须补投影而非冒认为完整。Task5 full目前只单轮export，refresh/hash有筛选摘要export，UI需能力明确。所有新增query须更新已有strict白名单并测试旧默认。
下一步：读具体DTO/nullable/枚举及现有脱敏函数，写精确历史回归，不先搭空页面；不得启动真实扫描。


Task1–4实现完成（最终整体验收在Task5）：精确hash runs/events格式及strict query兼容，masked evidence_json以文本传输，full覆盖快照/比较与refresh nullable增删/重核字段补投影。DTO null/大int/负差值/脱敏先RED→GREEN；intakeapi全包通过。独立/cloud/history路由404和三类表格先RED→GREEN；main保护路由接线，旧.164冻结。
筛选apply/clear/URL恢复、类型切换去不兼容覆盖条件，精确字符串分页/本页返回及浏览器popstate；invalid/duplicate/unsupported参数不请求API。初次TS构建因DOM.Iterable未启用失败，按现有约定改URLSearchParams.forEach，不改tsconfig。构建/TS和定向历史UI11/11通过。
固定source+run抽屉、nullable覆盖与前轮比较、原文哈希证据；摘要仅从同source/同列表context/同ID读回匹配，deep ID不在本页时不借用其他摘要。事件分页保持路径ID+游标，切ID迟到401不使新页失效。full/refresh精确事件响应补run_id作用域字段，旧默认保持。最近一次全前端285/285为前7项历史回归阶段，后补4项待最终全套；intakeapi最新4.152s通过。此前Go exec8542跨用户消息句柄消失，mainManagement需最终重跑，不将丢失输出当新成功。
下一步Task5导出，已核实cap：full单轮10000事件、refresh单轮100事件/摘要5000条、hash单轮与摘要10000条；保持原文，不JSON再序列化。无真实账号/服务/目录操作，无新包，无worktree/commit/push。

Task5代码与定向验收：新增CloudHistoryExport，原始JSON Blob，固定run及摘要filter/条数/UTC校验，413不下载，跨类型迟到401无副作用。端日期契约实际为started_through_utc；回归先RED发现错误key，改正后15/15。完整前端293/293；intakeapi2.180s/webui1.310s/root管理集成4.095s。三个工作台加历史入口；导出不增加API能力/限额。
最终唯一fresh-context review_165无Critical/Important。CUA本地fixture桌面与390px详情、Escape恢复原按钮、无文档水平溢出通过，已关tab/恢复viewport/停止fixture并移除脚本。不启动真实服务。现进入完整package门禁；最终包哈希与source记录写releases外部validation避免循环快照。

首轮完整package通过：前端293/293、legacy172/172、userscript3/3、完整Go -p1/vet、Windows AMD64/Linux AMD64/ARM64。独立检查ZIP13项、12载荷SHA、699源码逐项与manifest自hash、PE/ELF架构；无私有授权或临时脚本。完成结果记录后同版重包，最终哈希以releases外部165-validation.md与artifact-check.json为准。五项5/5，无已知本版阻塞，无真实账号/NAS验收。
