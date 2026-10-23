# 认证与管理入口 Implementation Plan

**Goal:** 实现阶段 C，交付授权可应用、可保存续期且管理入口受保护的版本。

**Spec:** [auth-design.md](auth-design.md)。用户已授权持续实施；当前会话测试先行、inline 执行，交付前独立审查。

## Task 1：可取消且脱敏的 OAuth/账号 API

- [x] 增加 Context 版本，保留现有命令行调用兼容；校验响应、限制参数与重定向。
- [x] transport 故障测试先观察失败，再实现实际请求取消、错误分类、slow_down；补 pending 等待后成功测试。设备会话和主程序 HTTP 集成通过本地模拟，真实百度授权待验收。
- [x] 百度 uinfo 校验与认证错误识别。

## Task 2：凭证管理与队列集成

- [x] 私有凭证/账号绑定原子记录，启动恢复优先级。
- [x] 串行应用/刷新、轮换先保存、验证账号、自动续期/退避；主程序和优雅退出已集成。
- [x] 下载器认证门禁与 auth_wait 恢复，不把认证失败当下载失败预算；持久化重启与新扫描恢复测试通过。
- [x] credentials.Manager 与 Downloader 的并发集成回归：当前下载 401 唤醒后台刷新、轮换后旧下载迟到 401 两种场景，新扫描后使用新 Token 完成且源操作一次，私有凭证轮换保存与预算不变；本地替身，未代替真实百度续期验收。

## Task 3：管理保护与前端

- [x] 管理 Basic/Bearer、私有随机默认密钥、Origin/WS 校验。
- [x] 网页授权实际应用，服务端设备会话，认证状态与扫码界面不泄漏凭证；实际本地 HTTP 经过真实管理器和下载器验证。
- [x] 健康状态独立反映认证和扫描结果，文本渲染修正；Node 执行真实前端脚本检查状态、文本转义和授权 URL。

## Task 4：验证与交付

- [x] 定向/全量 test、vet、Windows/WSL race、独立 review；最后新增主程序授权应用与 pending 测试也完成两平台 race 复核。
- [x] 更新启动/容器配置、测试说明；v0.3.0-preview.1 三平台包与哈希、Windows/Linux 包内 HTTP/WS/退出验证完成；真实百度、真实浏览器/NAS/Docker/ARM64 边界见包内 TESTING.md 与同级 validation.md。
