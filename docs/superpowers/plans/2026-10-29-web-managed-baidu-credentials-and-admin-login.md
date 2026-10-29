# 网页配置百度密钥与管理员登录实施计划

> **For agentic workers:** 由当前执行者按任务顺序实施，每项先写失败测试，再实现并回归。

**Goal:** 支持 `.env` 优先、queue sidecar 回退的百度网盘 AppID/AppKey/SecretKey/SignKey 配置，网页首次设置即时生效，以及可跨重启、手动登出的管理员表单会话。

**Architecture:** 百度 AppID/AppKey/SecretKey/SignKey 存在 queue 目录的私有 `baidu-app.json`，通过运行时配置对象同步更新 OAuth Server 与 credentials.Manager。管理员会话采用随机 Cookie token，SQLite 仅保存 token 哈希及创建/撤销时间；Gate 同时处理浏览器 Cookie 与 CLI Bearer。

**Tech Stack:** Go 1.22、net/http、modernc SQLite、嵌入式 HTML/JS。

**Spec:** `docs/superpowers/specs/2026-10-29-web-managed-baidu-credentials-and-admin-login.zh-CN.md`

## Global Constraints

- AppID、AppKey、SecretKey 与 SignKey 成组设置；`.env` 完整配置优先，空配置回退 queue sidecar。
- 首次网页写入原子且只允许一次，当前运行进程立即生效；已配置时不允许网页覆盖或清空。
- Secret 只展示服务端掩码，不进入日志、任务事件、浏览器存储或可读状态 API。
- 管理员 Cookie 不自动过期、跨服务重启有效；登出撤销服务端会话并清除 Cookie。CLI 保留 Bearer，移除 Basic。
- SQLite 持久化会话 token 哈希与 UTC 时间，不保存 Cookie 原文或管理员密码。

## Review Focus

- `.env` 部分配置、空值及 sidecar 损坏必须得到确定的错误或回退行为；四项凭证不得跨来源拼接。
- 同时发生授权/刷新/账号验证与首次保存时，不能混用密钥。
- SQLite schema v2 必须安全升级，旧任务、事件和绑定不得丢失。
- 注销后旧 Cookie 重放必须失败；服务重启后未撤销 Cookie 仍有效。
- 未登录页面、API 与 WebSocket 的响应行为必须一致且无 Basic challenge。

---

### Task 1: 配置来源解析与私有应用密钥 sidecar

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/taskstore/store.go`, `internal/taskstore/*test.go`, `main.go`。

- [x] 测试 `.env` 成对值优先、空值回退、部分配置报错、queue sidecar 持久化/重复保存冲突及掩码不泄漏。
- [x] 执行针对测试，确认因新配置加载和 store 接口缺失而失败。
- [x] 实现确定的 `.env` 加载优先级及 `Store.LoadBaiduAppConfig/SaveBaiduAppConfigIfEmpty` 原子接口。
- [x] 运行 `go test ./internal/config ./internal/taskstore ./internal/downloader -count=1` 通过。

### Task 2: 运行中应用密钥的原子重配置与网页首次设置

**Files:** `internal/credentials/manager.go`, `internal/credentials/manager_test.go`, `internal/authorization/server.go`, `internal/authorization/server_test.go`, `internal/webui/webui.go`, `internal/webui/index_oauth.html`, `main.go`, `scripts/webui.test.cjs`。

- [x] 测试 manager 保存后立即用新 AppKey 验证授权，AppKey 冲突在持久化前拒绝；测试 authorization server 动态启用设备授权。
- [x] 针对缺失 ConfigureApp 接口运行测试，观察预期编译失败。
- [x] 增加串行化密钥更新、受认证状态/首次保存 API、服务端掩码及前端空/只读状态。
- [x] 运行 `go test ./internal/credentials ./internal/authorization ./internal/config ./internal/taskstore` 和 `node scripts/webui.test.cjs`，通过。

### Task 3: 持久管理员会话与表单登录

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/migration.go`, 新增会话存储文件及测试，`internal/management/auth.go` / tests，`internal/webui` 登录页面及测试，`main.go`。

- [x] 测试 SQLite v2 升级、随机会话哈希持久化、撤销、重启恢复和 UTC 时间；Gate 测试 Cookie/Bearer、匿名拒绝、Basic 拒绝、Origin 防护和限流。
- [x] 针对缺失 session store/Gate API 运行测试，观察预期编译失败。
- [x] 实现 v2→v3 会话表迁移、hash-only session API、表单登录/登出路由、Cookie 认证和登录页重定向；保持 WebSocket 认证，并增加 CLI 全局撤销。
- [x] 运行 `go test . ./internal/management ./internal/taskstore ./internal/intakeapi`，通过；集成验证重启仍有效及 logout 后重放拒绝。

### Task 4: 回归、界面与发布验证

**Files:** 本变更涉及文件及用户文档。

- [x] 覆盖启动配置组合、敏感信息不回显、页面登录跳转、授权页首设及只读掩码。
- [x] 运行 `go test ./...`、`go vet ./...`、`node scripts/webui.test.cjs`、`git diff --check` 与项目发布构建脚本；均通过。
- [x] 审阅本轮涉及的 API pacing 改动与验证结果；真实百度 OAuth、目标浏览器和 NAS/SMB 的验收边界写入发布说明，仍需目标环境实测。
