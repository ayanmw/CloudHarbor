# 百度业务错误分类与后台恢复检查点

沿用持续可靠性优化范围。本轮只修错误分类和对应恢复接线，不修改凭证格式、管理认证、重试上限或人工隔离策略。

## 依据与复现

核对[百度官方 Go SDK errors.go](https://raw.githubusercontent.com/baidu-netdisk/baidu-drive-sdk-go/main/baidudriver/api/errors.go)：20016/20017/31045 分别为 Token 过期、无效、验证失败，31034 为频率超限；业务 errno=111 为异步工作仍在运行。该命名空间与原代码保留的 OAuth error_code=111 不能混为一谈。-6 还可能是接口权限问题，不据此强制刷新凭证。

本地失败测试先复现：上述新 Token 错误未归类认证、HTTP 200 限流未重试、业务 errno=111 错判认证，Move 运行状态未保留异步身份。独立审查另发现 Rename/Delete 单项 errno 的遗漏；补反例先 RED 再 GREEN。

## 最终行为

- 所有解析出的顶层与单项 errno 保留业务命名空间；OAuth error_code 保留原 110/111 语义，原有手工构造 APIError 保持兼容。新的 Token 码沿用带指纹的认证撤销/唤醒恢复，不使迟到旧凭证请求撤销新凭证。
- Token 拒绝将任务转 auth_wait，不消耗下载失败预算，不执行源后处理。现有凭证管理器负责续期/重新验证，恢复后仍需新扫描。
- 31034 进入既有有界指数退避和预算，未增加 HTTP/Move 客户端内部重试。入队 Move 仍只允许一次发送。
- Move 全局或任一单项业务 errno=111，和非零 taskid 一样优先于其他失败字段；返回 AsyncMoveError，即使缺少 taskid 也持久保留异步未知状态。目标可见不能据此放行；重复确认/只读核对不重发，不撤销有效 Token。

## 验证

- Windows UCRT 与 WSL 全模块非缓存 race、vet 通过；最后补 Rename/Delete 单项命名空间及多项 Move 异步迹象后，两平台 baiduapi/downloader race 与全模块 vet 再通过。
- Node 13 项通过；独立只读复核最终无新增确定 important 问题，git diff --check 通过。
- 本地 API 响应、实际调度、落盘和任务状态集成覆盖：20016/20017/31045 → auth_wait/预算不变；31034 → retry_wait/非立即重试/恢复一次完成；无 taskid 的运行 Move 在文件与目录两种情况下均 unknown/无新任务/不重发。
- 本检查点最初未独立打包，后续已合入 v0.6.0-preview.1；v0.5.0-preview.1 等旧包不变。没有访问真实账号。

## 后续核查

**已于 2026-10-29 对照当前源码关闭 Rename/Delete 响应边界项。**原先“下一步先复现”的描述已过时：`internal/baiduapi/source_operation.go` 现在禁止重定向重放，要求恰好一个带 errno 的单项结果，并将非零 taskid、业务 errno=111、缺失/不完整响应和非 401 的 HTTP/网络不确定结果视为未确认；`internal/baiduapi/source_operation_test.go` 覆盖不完整回执与 307 单次请求，`internal/downloader/source_operation_test.go` 覆盖不确定结果隔离及重启不重发。对应持久化状态、后处理和页面行为见 `source-operation-plan.md`。

**仍未关闭的外部事项：**可靠异步完成查询没有适用于当前 OAuth 文件管理 Move 的已核实契约，真实百度账号及 NAS/SMB 链路也未验收。因此 taskid/运行中响应仍保持 `source_unknown` 隔离；不使用网页 Cookie 接口，不按路径可见性推断完成，也不开放人工覆盖隔离。
