# v0.8.31 上传页目标目录选择器复用

沿用用户自主实施、brainstorming/writing-plans，main内联executing-plans，不worktree/普通commit/push，不切版。上turnprogress已交付选择器搜索与内联新建，整版Task4以包外v0.8.31-validation.md闭环，不重复旧工作。

范围：仅新建上传批次可浏览/缓存搜索/内联新建远端目录；已有batch补传固定目标保持只读。选择器内联在上传表单，不新增modal。选择/手动编辑target取消上传ack；选择器打开/创建中或unknown时禁止上传POST/PUT及重试，保留文件选择。新建成功只更新目录/显示独立回执，不创建批次；unknown关闭选择器并锁本页新上传，不自动释放/重发。已有后台批次行为不改，不把mkdir回执当作上传完成。

生命周期：picker busy与confirmed读取阶段分开；离页前无回执时保留unknown，已确认mkdir不能降成unknown。历史轮询不能清掉目录结果/unknown锁，不能切换批次破坏正在创建的目录。关闭/选用后焦点回选择器按钮或确认；401仍登录，迟到401不影响恢复页。网络/空目录/缓存不完整边界沿用既有选择器说明。

- [x] Task1 UI/tests：复用Picker，target/ack、固定批次只读、文件保留、目录busy/unknown/结果、history轮询隔离；先RED再GREEN。
- [x] Task2 故障/生命周期：创建不隐式上传、unknown阻止POST/PUT、离页/迟到401、confirmed读回阶段、手动改目标取消ack。
- [ ] Task3 同一review补审、完整前端/相关Go、桌面/窄屏可见验证、版本说明及同版完整打包，数据指纹保留。

不引入上传协议/SQLite迁移，不擅自真实百度操作，不启动真实服务；模拟文件仅在内存/本机测试。剩余上传恢复/速度ETA等仍另列路线图，不借这次选择器接线宣称已完成。
Ledger：上传Picker入口与手改target取消ack先RED再GREEN，固定批次无Picker、文件保留、mkdir独立回执/unknown锁/轮询不清除、pending离页late401、confirmed读取阶段离页、Escape及当前401均覆盖。新增测试一处括号语法错误已定位修正；最终12项上传测试和全Vue348通过。共享Create的当前401先failed/释放busy再登录，避免误标unknown，通用说明改为“外层操作”适配上传。
同一review补审无Important/Critical。桌面/390px mock缓存搜索→在线目录→内联mkdir→选用上传目标，上传ack未勾选、未创建批次，Escape焦点回位通过；fixture计数mkdir1、batchCreates0、puts0。可见流程未选择本机文件，文件保留/确认有独立自动化证据。截图upload-target/mobile.png；当前mock进程/脚本/标签已清理。Task3完整门禁/原位覆盖及数据指纹以包外validation最终闭环，不改已校验快照形成自引用。
