# 源后处理完成与不确定结果

持续可靠性修复：本地下载完整后，源 rename/delete 也必须得到严格完成凭据；不把异步受理当成功，不重放 POST，不把崩溃后的不确定操作当普通下载恢复。仅测试本地替身，不操作真实账号。

- API：保留 RenameFile/DeleteFile 签名，共用严格单项同步 filemanager 请求。禁止重定向，响应有界，检查 errno/error_code/info 单项与字段存在；taskid 或业务 111 优先为不确定。网络、读取、格式、5xx/重定向等返回带不确定标记的错误，保留安全错误链；明确业务拒绝及 401 可沿原恢复处理。
- 持久化：根任务新增 source_state（sending/unknown/confirmed）。发送前将 sending 与 source_unknown 状态原子落盘；旧版遇新状态拒绝启动。发送后仅严格同步成功可保存 confirmed/done；明确拒绝清除标记；其他结果 unknown/source_unknown。启动 sending 转 unknown，confirmed 保留完成。
- 调度：unknown 不认领、不取消覆盖、不手动 retry，扫描不覆盖其记录；源路径和 keep 的标记目标隔离，阻止换 ID 后从同路径重新入队。无关任务仍执行。不增加人工放行。发送前落盘后重新校验认证与取消，尚未发请求则可清除标记。
- 兼容：历史无 source_state 记录正常加载；main 的持久化下载器启用该事务。非持久化 NewDownloader 的旧测试/库用法只验证严格 API，不能代替跨重启保证。
- 证明：先 RED 再修 API 缺字段/异步/307；模拟回复丢失和崩溃记录，真实磁盘重开后不重发；保存失败不发请求，明确拒绝允许原策略重试，正常完成保留 done；页面清楚显示 source_unknown 且无重试按钮。独立 review、两平台 race/vet、现有 Node 回归。

可靠异步状态查询仍无契约；本轮不根据“源不见/标记目录出现”自动解除不确定状态。需要实机验收的项目继续保留，不据本地替身宣称真实网盘/NAS 已通过。

## 实施证据

- 已复现原接口缺失单项、异步 taskid 均返回成功，307 实际重放 10 次；严格同步 parser 与禁重定向修复后通过。响应模型及同步单项结构核对[官方 SDK](https://raw.githubusercontent.com/baidu-netdisk/baidu-drive-sdk-go/main/baidudriver/api/file_manager.go)，不据 SDK 类型代替真实账号验收。
- 已复现丢回复重启不确定状态丢失；持久化 source_state/source_unknown 后，keep/delete 两种模式均不重发。发送保存失败零请求，完成凭据保存失败保留旧 sending，重开后 unknown；历史记录和正常同步完成重启兼容。
- 根任务本地完成曾短暂显示 done，现由 scheduler 发布最终状态。API/页面区分 source_unknown，禁止 Cancel/Retry；源隔离并入扫描、入队预检/登记和浏览选择资格。
- 独立审查补出“同 ID 换路径”覆盖不确定日志的反例，已先 RED 后修；重开后原源路径隔离仍在。原有“同路径换 ID”和管理页入队绕过反例也通过。
- Windows/WSL 全模块 race/vet、Node 14 项通过，最终审查与后续补项检查后再纳入独立版。
