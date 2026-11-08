# v0.8.31 当前构建辨识与只读诊断

## 目标

固定版本覆盖后用户需知道当前进程是否为最新构建。设置新增版本与诊断页，仅读取编译内置元数据，不读取用户配置、账号、任务或本地文件，不把安装目录文本当当前进程身份。

## 协议与打包

GET /api/build-info沿用管理鉴权、no-store，其他方法405。返回版本、revision、完整source_snapshot_sha256、built_at_utc、runtime平台、Go版本、observed_at_utc。开发构建缺时间/快照如实为空。-build-info在配置/锁/服务初始化前输出同一JSON后退出，-version现行为不变。

package.ps1在三平台build前固定一次UTC，ldflags注入builtAtUTC/sourceSnapshot，BUILD-INFO使用相同UTC；最终以已安装exe -build-info对照文本/manifest验证。元数据不是签名，不把dirty快照当Git提交。

## 网页

/settings/about复用Header/侧栏，表格显示字段、构建UTC与服务端读取UTC，复制仅这些字段，失败手动复制文本；刷新失败保留旧值标历史并禁用复制，当前401需要登录，迟到401不污染恢复页。没有控制/升级/修改配置动作。链接到上传/下载/扫描/操作记录供诊断，明确当前构建信息不是任务健康证明。

## 实施与验证

先后端read-only/gate/元数据红绿、前端实际编译页面的读取/复制/失败/生命周期回归，再模拟桌面/窄屏、独立复核和完整发布门禁。检查真正compiled值与包元数据一致，不用源码赋值代替。保留账号数据、不启动真实服务、不切版/提交。
