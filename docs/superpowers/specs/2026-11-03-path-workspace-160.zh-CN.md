# .160 下载路径与迁移工作台

目标：把当前/cloud长页面中的路径设置与当前迁移状态整理为独立Vue入口/settings/paths，减少在缓存/清理工具间寻找配置。沿用已有管理接口和迁移算法，保留旧云端入口及完整迁移历史/事件查询，不在本版重做整个云端文件管理。

本版五项用户可见交付：
1. 路径总览与独立导航：清楚区分当前网盘监控目录、本地下载目录和服务端返回的可编辑来源；.env/启动环境锁定时只读。路径以服务端实际值为准，不在浏览器猜测workDir或绝对路径。
2. 云端路径变更影响预览：紧凑表格显示会影响的既有任务及策略，保留50项分页和服务器digest。输入变化立即使旧预览失效，身份/游标不经过不精确Number冒充原值。
3. 路径保存与迁移选择确认：独立呈现仅切换/迁移原目录文件的差别，最终展示固定前后路径和确认内容；提交一次消费确认。运行中/迁移中或配置锁定不能保存，后端同锁最终判断；无可靠只读运行信号时补一个最小的只读锁定字段，不以当前分页没有任务推断空闲。
4. 当前迁移状态：按ID展示来源/目标/阶段、文件/字节进度、开始/更新/结束UTC和错误。轮询单在途、可见空状态、失败保留历史且锁定不依赖旧快照。running/failed/completed沿用后端，不把本地状态推断成远端移动或过去成功。
5. 保存结果恢复与当前证据导出：200保存/202迁移/409影响变化/未知响应分别反馈，刷新设置和迁移后才能判断当前状态。响应未知不自动重发，最新迁移若无本次回执绑定不冒认本次结果。导出绑定已展示迁移ID、原始blob保精度；保留/cloud#migration-history历史明细入口。

API：GET/POST /api/settings/paths（queue_dir、local_download_dir、migrate_files、queue_change_preview_digest）；GET带queue_dir/offset/limit取queue_change_impact。GET /api/settings/local-migration；GET /api/settings/local-migrations/{id}/export。本版复用已有历史页/接口。先核实所有DTO/枚举与阶段；数字身份若不安全采用既有项目的可选字符串响应方式或明确不展示为精确身份，不改旧默认格式、不升级SQLite。

现有后端持久化/目标校验/原目录退休删除/任务锁、未知隔离和重启恢复保持。不新增删除本地目录的捷径、不从浏览器直接访问磁盘、不改变NAS/SMB验收边界。输入失败、迟到、401、换任务和导出上下文都需回归。

模块：PathSettingsWorkspace.vue负责当前路径/草稿与保存，PathChangeImpact.vue负责影响表格/预览上下文，LocalMigrationStatus.vue负责当前迁移/精确进度/导出，path-settings-types.ts负责实际DTO。共享Sidebar增加路径与存储导航，main.ts/webui/main.go接同一受保护页面。旧/cloud原表单暂保留作为兼容入口，不删历史控件或误改其脚本。

原目录内联，不worktree/自动提交/push；不操作真实下载目录或唯一凭据。用临时合成目录/进程内后端和模拟UI验证，全部完成后完整门禁及ZIP快照校验才发布.160，.159冻结。

Task3后端细化：POST可选expected_paths（queue_dir/local_download_dir），旧调用不传时兼容；新页须传本次确认所依据的当前路径。前置条件在pathOperations写锁内校验，维护准入成功后还须再次核对当前路径，覆盖旧迁移发布路径→释放维护状态的交接窗口。GET新增queue_busy/queue_busy_reason，与TryBeginMaintenance同queue.mu计算，只是只读观察，不是完整保存许可；入队/扫描/最终维护检查保留。
