# .158 添加网盘项目工作台实施计划

> 使用superpowers:executing-plans，原目录内联；按任务先写失败测试、实现、运行相应完整检查。无自动Git提交。

Goal：完成添加项目页面五项体验改进，保留原预检/提交/核对安全边界。
Architecture：IntakeWorkspace.vue承担目录与选择；IntakePreview.vue承担固定预检确认；IntakeRecordDetails.vue承担历史明细。沿用WorkspaceSidebar/modal、main.ts入口与webui.writeEmbeddedWorkspace。
Tech Stack：现有Vue3/TypeScript/Vite、Go和已有SQLite；不新增依赖。
Spec：docs/superpowers/specs/2026-11-03-intake-workspace-158.zh-CN.md。

## 全局约束与审查重点

- .157已冻结：最终ZIP哈希见同级.157-validation.md，不重打旧包。
- 不能以source.fs_id JSON数字冒充精确身份；使用source_id字符串。
- 目录切换、过期、认证失效、修改选择后旧预检不得提交；服务端核对仍是最终依据。
- 提交未知/断网不重发、不人工放行，recheck只调用已有核对语义。
- 列表失败保留已显示证据，分页不能混用新条件；迟到响应与换批次隔离。
- 本地模拟、路由测试、打包与真实服务验收明确分开。

## 任务

- [x] Task1：阅读intake.js全部控件及server.go路由/DTO，记录能力对照；frontend/scripts/ui-intake.test.mjs先复现/intake未挂Vue、目录为空容器/上一级/表格缺失，再接IntakeWorkspace.vue、main.ts、webui.go与workspace权限回归。验证前端全套及Go webui/主路由定向。
- [x] Task2：先覆盖跨页/目录保留选择、20项边界、手工输入、不可选、移除/清空及迟到目录响应，再接已选清单。ID精度测试要求source_id原字符串，目录大小不伪装已统计。
- [x] Task3：先覆盖预检失效、过期、确认勾选、重复点击一次提交、网络未知不重发，再接IntakePreview.vue。核实Expires及认证变化语义，保留固定队列和策略；已过期预检不得因页面重开变新。
- [x] Task4：先覆盖历史opaque cursor、过滤失败/分页失败/迟到不串数据、空结果、批次详情精确字段，再接历史表格与IntakeRecordDetails.vue。缺字段不推算，不扩展API历史能力。
- [x] Task5：先覆盖recheck仅核对、认证失效、未知隔离/无放行入口、提交后刷新及失败反馈；完成旧能力对照。最终独立审查一次、桌面/390px/焦点检查、完整发布门禁、三平台及ZIP快照校验，才创建.158测试包。

Ledger：.157已完成五项、完整门禁和最终独立产物校验；本版规划已建立，尚未修改产品代码，下一步Task1先补能力对照和失败测试。窄屏分页文字换行可在本版统一相关控件样式时修正，不单独计为新增版本条目。

## 能力对照与实施记录

旧页已核查：目录GET offset/limit=100；每批20个路径，预检保存固定queue_dir/download_mode/expires；修改输入使预检失效；提交POST /api/intakes/{id}/commit仅一次；历史GET独立opaque cursor与unresolved；POST /{id}/recheck只核对不重发。状态ready/sending/moved/failed/not_sent/unknown和各项错误说明必须保留。source_id是字符串，不用source.fs_id数字显示身份。旧脚本/模板暂保留至完整迁移对照结束。

Task1：新页测试先观察未挂Vue、目录容器/导航缺失而4/4失败；Go入口回归也先失败。接IntakeWorkspace.vue及main.ts/HandlePageIntake，目录表格、文件大小独立列/目录隐藏0B、空目录容器、上一级及分页，响应校验、401处理、取消及代次隔离。随后4/4绿，前端全套86/86、Go资源1.342s、管理HTTP/全局历史5.278s通过。

Task2：新增跨目录保留、手工输入同步、重复路径去重、20/21边界、不可选项、移除/清空测试，先3条失败后实现选择清单。展示路径而非不精确文件ID，手工路径仍需未来预检在线核对，不把选择当作可执行证据。最终构建、前端89/89（本页7/7）、Go资源1.390s、管理HTTP/全局历史5.529s、diff通过。

当前2/5，尚未接预检/提交/历史，不打包这个开发页；.157冻结不修改。无模拟服务启动、无真实账号调用、授权复制、队列修改或Git提交。下一步Task3预检确认生命周期与单次提交。

Task3：先观察5条预检/确认回归失败，再接IntakePreview.vue及intake-types.ts。预检核对固定路径顺序/数量/ready状态，展示source_id字符串，不展示source.fs_id JSON数字。父页请求代次/Abort隔离迟到预检，选择同步变化立即作废；本地500ms刷新过期提示，提交前Date.now再核对，最终由后端Boot/Epoch/Expires/队列目录和来源重查约束。公开OAuth状态没有认证代次，不假装浏览器可识别所有ready→ready换凭据；后端拒绝时旧确认被消费，需重新预检。

确认按钮要求勾选，提交时先清除可用预检再发一次POST，期间禁改选择；响应中断/批次不匹配不重发。新增已展示预检改回原路径仍失效、运行中过期、后端认证变化拒绝回归。前端全套97/97（本页15/15）、构建、Go资源1.326s、管理入口/全局历史5.946s、diff通过。

当前3/5。历史GET返回records/total/next_cursor，cursor应原样传递，offset仅旧兼容分支；下一任务历史紧凑表格/抽屉、过滤与游标失败隔离，再接recheck和授权状态展示。尚未可见UI验收/独立审查/完整门禁，不打包开发页；无真实账号、服务启动或Git提交。

Task4：先观察4条历史表格/抽屉/分页/筛选迟到回归失败，再接IntakeHistory.vue及IntakeRecordDetails.vue。表格保留批次ID、状态、首项路径/项目数、队列与原时区创建时间；抽屉完整展示逐项来源/目标/source_id字符串、错误说明、异步ID和同步确认，不用历史预检作为新的确认入口。侧栏/主区域inert及共享modal管理焦点。

历史游标原样传递，只校验格式/上限/非循环；不解码后转Number或假定固定快照。成功读回才更新页面位置和游标链；失败保留旧行，同筛选可重试，筛选不一致禁用旧导航；Abort/代次隔离筛选迟到结果。构建、前端101/101（本页19/19）、Go资源1.278s及管理入口/全局历史回归通过。清理本任务新建但未使用的cursor副本，行为不变。

当前4/5，尚未接回recheck按钮/授权状态和提交后刷新；不发布当前开发页。最终可见UI、独立审查与完整门禁待Task5完成。无真实账号请求、服务启动、凭据迁移或Git提交。

Task5代码：先6条回归失败，再接只核对/recheck、共享动作互斥、提交后目录及历史刷新、账号状态读取/手动刷新/不可用禁预检和旧确认作废。用户脚本交接旧能力补回：setup阶段先清片段再挂载子组件/发GET，严格20项/路径/键集合校验，仅导入选择，不自动预检/移动。前端109/109通过。公开状态不含Epoch，后端依旧是最终依据。

Final: fixed 跨批次反馈串入旧详情（Important）—独立审查复现A核对→B提交→重开A误显成功。新增完整流程回归先失败，再预检清反馈绑定、提交绑定本批ID，消除串批。可见检查发现主区未用现有布局类被侧栏遮挡，新增共享shell接线回归先失败，再接workspace/main类；目录列宽独立覆盖，账号工具条/输入与确认区样式补齐。最终前端111/111通过。审查其余范围无确定缺陷，不新增二次审查。

可见本机模拟验收：桌面主区/目录表格、选择→预检固定内容/确认勾选、390px概览和详情、Shift+Tab焦点循环与Escape返回行按钮通过。仅fixture允许预检模拟POST，阻止commit/recheck；未调用真实账号。截图保存在releases同级.158-desktop/preview/mobile/mobile-details.jpg。临时标签页关闭、viewport reset、模拟服务停止且夹具删除；完整发布门禁待执行。

Task5最终：完整package退出0，Go全包/vet、前端111/111、legacy172/172、userscript3/3及三平台构建通过。独立ZIP校验13项/12载荷、609源码快照逐项匹配、PE/ELF架构与私有状态排除通过。审查Important和可见布局问题均先红后绿修复，无未处理审查缺陷；文档补齐后更新同版并再次校验最终哈希。真实百度/NAS/SMB和打包二进制启动未验收，未迁移真实状态/提交Git。
