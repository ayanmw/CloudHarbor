# v0.6 实际浏览器补充验证

日期：2026-10-23。目标程序为不可变 v0.6.0-preview.1，源码 revision `62586afd2809e1b54d5072a95eed783224830dad`；仅运行发布目录的 verification 副本。没有百度凭证，HTTPS 代理指向拒绝连接的本机地址，任务为预置合成 source_unknown 记录。

## 验证入口与边界

内置 Chromium 浏览器直连 127.0.0.1:18140 时返回 ERR_INVALID_AUTH_CREDENTIALS，没有可操作的原生认证框。因此没有宣称“用户在普通浏览器原生 Basic 登录框中输入后直连已验收”。

另启仅绑定回环的临时 HTTP 转发入口 18142，只有空白 /__probe 测试页公开，其余 HTTP 与 WebSocket 原样转发到实际包内程序，保留 Host/Origin/WWW-Authenticate，不注入 Authorization，也不绕过程序管理门禁。测试页只发起 GET /api/status。

在该只读资源请求上观察到服务端 Basic 挑战（realm 为 Baidu NAS management），通过浏览器的 Fetch.continueWithAuth 提供专用假测试凭证，返回 200。随后清除测试拦截，再导航到受保护的真实任务页。后续页面、资源、POST 和 WebSocket 使用浏览器认证缓存；不是用伪造 API 返回或关闭认证来显示页面。

该结果证明受控 HTTP 转发入口下的浏览器认证缓存与应用配合；不等同直接原生认证框、不同浏览器、TLS 终止反代或真实百度全链验收。

## 已观察到的实际行为

1. 真实任务页显示 WebSocket 已连接、账号尚未授权；收到实际 WebSocket 状态帧，展示 1 条 source_unknown 任务，中文“禁止重发”提示清楚，没有取消/重试按钮。
2. 正常停止实际包内服务，浏览器显示“已断开，3秒后重连”，账号状态显示连接异常；保留旧任务展示但没有把连接标为成功。
3. 重新启动同一个验证目录，无需刷新页面，浏览器自动连接并恢复“尚未授权”，同一条持久化隔离任务仍在。
4. 点击立即扫描，实际管理 POST 成功，页面提示账号未授权会等待；没有实际百度扫描或下载。
5. 导航到真实 /poll，浏览器继续通过认证缓存轮询，展示同一隔离任务及错误，没有操作按钮。
6. 真实 /intake 接收测试 fragment 后，地址栏清为 /intake、路径填入输入框，提示核对账号，记录仍为空；没有自动预检或移动。未授权的云端列表被 409 拒绝。
7. 手动点击预检，页面显示“请先完成百度账号授权或续期”，仍未生成确认卡或执行云端操作。

重连后的实际截图：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.6.0-browser-reconnect.jpg`。截图为实际包内页面，任务数据是隔离的合成记录。

## 清理与交付

主测试标签关闭；网络拦截在导航真实任务页前已清除。临时转发服务与包内程序均已停止，程序日志 Service stopped；Windows Ctrl+C 的外层 PTY exit 1 不当作原生退出 0。没有修改产品代码或重新打包，v0.6 ZIP SHA256 仍为 `b2c5fdc8d4b212f86c02b2fc6233fcd6cab0dc417b801cf4fcc5ea275a8b5a88`。

仍待验收：目标浏览器直连时原生 Basic 输入流程、真实百度/OAuth/自动续期、油猴真实页面及沙箱、NAS/Docker/ARM64、多日与断电，以及可靠异步完成查询。缺少完成凭据的未知目录仍不提供人工放行。
