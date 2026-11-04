# 八项反馈实施计划

Spec: ../specs/2026-11-04-cloud-browser-166.zh-CN.md

- [x] Task1 排查子目录mkdir：定向复现+浏览器/日志；修复根因与操作反馈。
- [x] Task2 format-bytes.ts统一BigInt容量展示，图标与紧凑操作CSS；定向边界验证。
- [x] Task3 实时浏览主入口、常显搜索、页合计和缓存状态。
- [x] Task4 /intake?path二级确认与独立/intake-history，侧栏调整，保持原流程/兼容。
- [x] Task5 持久批量上传：核实官方接口，独立详细计划后按provider/SQLite/调度/API/界面顺序实施。
- [x] Task6 完整回归、最终一次fresh-context审查、桌面/窄屏/键盘，整版打包/哈希/账号唯一迁移。

Ledger: .165已发布且唯一真实.nas-downloader由.150移动至.165，44文件逐项hash匹配、SQLite integrity ok。当前用户正在运行.165 PID67220，仅只读调查，不提前结束用户服务。八项属于本批，不宣布上传完成或编造mkdir根因。

Ruling: 旧/intake无path保留兼容浏览，正常侧栏入口已合并，新下载按钮均使用path二级预检且不重复浏览。避免破坏旧书签/脚本，spec同步；不新增主菜单浏览入口。

Task1-5实现与验证：mkdir普通子目录可打开；复现unknown结果时按钮可点而handler静默拦截，禁用条件与handler统一并说明原因。容量BigInt/IEC及原始title，主入口下载path精确传递、独立入队记录；常显缓存搜索，不完整明确部分范围，实时目录不由缓存替代。页大小含目录缓存，搜索父子重叠提示。图标与紧凑边框工具区。
上传：独立状态目录uploads/uploads.sqlite，FULL/WAL、UUID暂存Sync/rename→queued、单worker、每次阶段与UTC事件落库。hashing/uploading重启可安全重排（整文件重新预上传），submitting/finalizing未知隔离；未知不自动重发。失败手动重试，取消仅待处理，启动清理自有终态.data/UUID.part，保留其他文件。分片4MiB/10000片，每批最多10000文件，默认保留远端原文件并自动另存。浏览器不会提供空目录；未接收文件重选。秒传缺身份证据保守unknown。真实上传尚未验证。
最终唯一review_166发现扫描互斥和取消暂存回收两项，均修复且同一review复核关闭；旧URL兼容Ruling同步spec。另自查父目录创建嵌套path读锁，改为每次MutateCloud自己持锁，文件上传阶段再取path锁，避免等待writer时递归RLock死锁。
前端302/302，provider上传+uploads持久回归通过，管理HTTP集成4.143s。CUA本机mock桌面紧凑表格、下载二级预检、子目录新建取消/Escape焦点恢复、上传任务排序与390px浏览通过；截图同级166-browser/uploads/intake/mobile。fixture已停且脚本移除，viewport复原/tab关闭。未触碰真实云端；用户当前.165服务健康，两项下载中、一项source_unknown。进入完整package门禁，发布后再安全停服迁移唯一状态并恢复。

首轮完整package通过：前端302/302、legacy172/172、userscript3/3，全仓Go -p1（downloader234.241s）/vet/三平台。其后仅补启动账号readiness门禁与定向回归，uploads4.481s通过，同一最终review复核无Important；接下来同版重包全门禁，最终产物信息以外部166-validation.md为准。
账号状态已在旧服务自然停止且writer.lock独占可取时，从.165移动至.166，46文件逐项SHA一致、旧目录消失，SQLite只读integrity_check=ok。保持服务停止，不重启真实自动下载，不执行新增真实上传/移动/删除测试。本地下载目录设置保持原有路径。没有worktree、commit或push。
