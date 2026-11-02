# 只读网盘请求网络错误诊断实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让手动添加预检和网盘目录读取在 DNS、网络超时或连接失败时显示安全、具体的中文提示。

**Architecture:** 仅扩展 `internal/intakeapi/server.go` 的只读 `readFailure` 分类，按错误类型返回固定文案。沿用 text/plain 错误响应与已有 UI 展示，不暴露底层错误或请求 URL。

**Tech Stack:** Go、`net`/`errors`、现有 API 测试、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-10-30-readonly-network-error-diagnostics.zh-CN.md`

## 全局约束

- 写操作的 `failure` 与未知结果安全处理不变。
- 不返回 URL、令牌、原始错误字符串或堆栈。
- 不添加自动重试或外部网络请求。
- 不创建 worktree、不提交、不更新用户运行目录。

## Review Focus

- `*net.DNSError` 也实现 `net.Error`：DNS 分支必须先匹配，不能退化成普通连接错误。
- 包装在 `url.Error` 中的错误：分类使用 `errors.As`，展示文案不得来自其字符串。
- `context.DeadlineExceeded` 保留现有提示与状态码。
- `baiduapi.APIError` 继续展示 HTTP/百度错误码。
- 非网络普通错误继续使用通用安全提示。

### Task 1：安全网络错误分类

**Files:**

- Modify: `internal/intakeapi/server.go`
- Test: `internal/intakeapi/server_test.go`

**Interfaces:**

- Consumes: `readFailure(http.ResponseWriter, error)` 当前错误映射。
- Produces: 对 DNS、网络超时、dial/read 错误返回固定中文提示；不改变 handler 或响应格式。

- [x] 先为带敏感 URL 的 DNS、超时、连接错误添加 API 回归。
- [x] 运行定向测试，确认当前通用提示不能满足新分类断言。
- [x] 使用 `errors.As` 在 `readFailure` 的普通错误分支分类，并保持 APIError、context 和写操作映射原样。
- [x] 运行只读错误分类定向测试及 `go test ./internal/intakeapi`。

### Task 2：页面展示和预览发布

**Files:**

- Test: `scripts/webui.test.cjs`
- Update: `docs/ai/progress.md`、`docs/ai/release-versioning.zh-CN.md`、`docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] 添加页面回归：预检 API 返回 DNS/超时提示时，页面准确显示安全文案。
- [x] 确认没有自动重试、确认提交或显示原始 URL 的行为。
- [x] 运行 WebUI/userscript、全仓 Go 测试和 vet。
- [x] 生成 `.77` 三平台测试包，核对 ZIP、校验和、源码快照、私有文件排除、版本和 ELF 架构。
- [x] 在发布记录中说明本版本无真实百度写操作/部署，记录最终 SHA-256 和外部验收限制。
