# v0.8.31 远端变更未开始与未知结果反馈

沿用用户最佳推荐自主实施，main内联executing-plans，不worktree/commit/push，不切版。上turn为progress已交付复制。此工作落实P0-E错误/恢复交互，修复已有明确执行前冲突被笼统显示未知。

设计：HandleCloudMutation仅在无operation_id且错误为既定Invalid/Stale/Conflict/AuthenticationWaiting时返回结构化{outcome:not_started,code,message}，400/409保持HTTP语义；带operation_id的confirmed/failed/unknown及其他网络/服务器异常不改。不得只靠HTTP409判定未执行。明确code与状态匹配，无operation/operation_id混合字段，消息非空有界，才接收未开始证据；畸形/矛盾/未知code仍按不确定处理。旧400输入错误兼容保留，旧409纯文本保持未知。

页面：单项显示失败/未执行原因并关闭已消耗确认，刷新当前目录；批次该项失败、刷新当前目录后停止，其余未发送，不自动重发。刷新失败不将确定未执行改为未知。维护现有generation/active迟到保护，返回登录不得被旧响应触发。其他错误不篡改已持久回执。通用读取接口不改。

- [x] Task1 API结构化拒绝合同，测试白名单/无操作ID、已发送与非白名单不误标，状态及旧回执保持。
- [x] Task2 共享纯解析、单项/批次接线、失败刷新/停止/迟到/模糊响应测试；先RED再GREEN。
- [ ] Task3 完整前端、相关Go、同一review补审、mock可见验收；版本说明及同版发布门禁/原位覆盖，数据保留独立核对。

不新增SQLite记录或改变远端执行流程；无prepared记录意味着handler未向provider提交，不能把no ID推导为任意网络请求都未执行。真实账号不用作失败测试。最终验收/包hash写包外记录。
Ledger：API测试先旧纯文本导致RED，再结构化GREEN；intakeapi全包count1通过3.921s。UI先未刷新RED，再GREEN；完整Vue329通过，覆盖明确拒绝刷新、batch刷新失败仍failed且停止、旧409/未知code/矛盾封装仍unknown、离页迟到JSON不刷新。默认400兼容、401登录和已有operation回执保留。
同一review补审无Important/Critical；桌面/390px本机mock身份变化拒绝、第一项failed/下一项未发送、列表刷新、Escape焦点回位可见通过。截图releases/v0.8.31-not-started.png/mobile.png。临时服务/脚本/标签清理；无真实百度操作。Task3完整发布门禁/同版覆盖后在包外validation闭环，源计划快照保留构建前状态。
