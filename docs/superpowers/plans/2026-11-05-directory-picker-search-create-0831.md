# v0.8.31 目标目录选择器搜索与新建

用户已授权自主实施，沿用brainstorming/writing-plans与main内联executing-plans；不worktree/commit/push，不切版。上一turnprogress已发布拒绝反馈。完整目标仍为目标目录选择器支持缓存目录搜索、在线核对和内联新建，并复用至相关入口。

方案：SQLite加directories_only=1过滤，在SQL计数/分页前限制目录；不选前端过滤混合文件页（会造成空页及错误总量），不新增远端全盘搜索/扫描。保留现有混合搜索默认和cache_generation；授权账号隔离、分页代次与覆盖元数据不变。搜索结果仅作为位置建议，点击后由picker读取该路径实时目录，成功后才可使用，禁止源目录及子目录。

UI：选择器内联独立CloudDirectorySearch组件，不新增模态；手动提交1..128字符，说明已缓存范围、代次、观察UTC，目录全路径清晰。搜索打开时禁用当前目录“使用”，失败旧结果不能操作；编辑查询/关页/卸载/新请求取消旧结果和401，换代回第一页仅一次，不混合旧游标。验证只读，不操作真实账号。

新建：下一阶段先厘清picker内创建的busy/unknown如何传到外层单项/批次与根列表，避免嵌套模态、外层确认丢失或unknown后仍可提交。确认名称/目标与mkdir操作记录，成功在线重读目标，unknown终止外层未发送请求；不能仅塞一个POST按钮。此项未实现前不得宣称选择器功能全部完成。

- [x] Task1 taskstore目录过滤、downloader授权包装、API参数/响应合同；默认兼容、账号隔离、分页总数/代次与无provider请求测试，先RED再GREEN。
- [x] Task2 内联搜索组件/picker接线；候选实时核对、禁止自身/后代、旧结果/查询编辑/换代/迟到401测试。
- [x] Task3 内联新建状态与外层确认协调，明确busy/成功刷新/unknown隔离，保持一层对话框，故障测试。
- [ ] Task4 统一补审、完整门禁/可见桌面窄屏、版本说明与同版程序覆盖，账号指纹保留。

分goal turn执行，未完成阶段保留清单，不能把只读搜索等同新建已做；不因该计划生成新版本。最终发布证据写包外validation，真实provider/NAS验收另记。
Task1/2 ledger：目录过滤方法先缺符号RED，再GREEN；taskstore目录/旧混合搜索定向通过6.168s，API全包count1通过3.887s；downloader授权+账号隔离+无provider读取集成通过1.485s。SQL目录计数先过滤且不额外计算递归目录大小。API只允许directories_only=1且拒绝重复；默认混合搜索不变，代次和精确字符串fsid保留。
前端搜索入口先缺按钮RED，再GREEN；全Vue334通过，当前工作流27项通过。覆盖缓存候选在线成功/失败、搜索打开禁用直接使用、缓存不完整提示、禁止源/子目录、编辑pending查询后late401被忽略、换代重读首页不混旧页。三个模块vet及diff-check通过。未接入真实账号、未启动服务、未更新运行包，仍保留上轮已发布拒绝反馈版。
Task3实施细化：用独立内联mkdir确认组件，不循环嵌套CloudFileActions。picker创建期间busy向外层和root传播，原始单项/批次确认禁止发送；成功后在busy仍为true时调用只读列表刷新callback（保持原action挂载，避免根open清除确认），再在线打开新目录；保持原目标名和固定清单，需用户再次选择目标/勾选。创建成功记录在内联结果及operation链接，不能冒充外层move/copy已发送。unknown向外层传播结果并消耗/停止原未发送确认，root隔离写入口；401/离页/结果持久化失败各自沿用明确语义。root刷新失败不推翻mkdir已确认结果，busy解除必须覆盖卸载/收起场景。下一turn先故障/状态测试再实现，Task4完整review/可见/发布门禁仍待做。

Task3 ledger：内联CloudDirectoryCreate独立确认/一次POST，picker busy及已确认读取阶段上报至外层/root；根只读刷新时保留原action，原目标名/批次保留。confirmed通过auxiliaryResult单独展示，未发外层项目不冒充已发；unknown消耗外层确认并锁写入口。新目录成功读回才选用；刷新失败保留mkdir回执可只读重查。5项mkdir故障/状态测试RED→GREEN，全Vue339通过（焦点补丁前），相关API全包4.233s、授权缓存集成0.445s及三模块vet通过。
可见桌面/390px完成缓存搜索→在线归档→内联新建→重新选目标/确认复制，单一modal、目标名保留、两次独立回执；mock截图picker-search/created/mobile.png。发现单项完成后焦点落body，新增RED并修复刷新后的焦点回原动作/表头/刷新按钮，相关6项GREEN。统一review无Important/Critical，焦点delta补审进行中。临时mock服务/脚本/标签清理，未涉及真实账号。Task4完整门禁/原位覆盖最终证据写包外validation；不因完成此阶段而切下版。
