# HTTPS 终止代理 Implementation Plan

沿用用户持续实施授权，inline 执行，使用 writing-plans 和测试先行流程。

**Goal:** 支持 NAS HTTPS 终止代理到 HTTP 后端的管理 API 和 WebSocket，保留认证与跨来源保护。

**Architecture:** 可选 PUBLIC_ORIGIN 固定外部 HTTPS 来源。Gate 验证配置后在私有 request context 传递预期来源；现有 CheckOrigin 同时供外层 Gate 和 WebSocket 使用。未配置仍按实际请求 TLS/Host 校验，不读取 Forwarded/X-Forwarded-*。不新增代理信任列表、CORS、证书管理或子路径部署。

**Tech Stack:** Go 标准库、现有 gorilla/websocket、PowerShell/sh/Compose。

## 取舍与边界

- 采用显式固定 HTTPS 来源，避免信任客户端可伪造的转发头；仅有一个管理站点，无须多来源规则。
- 配置只允许 HTTPS scheme、非空 Host 和可选根斜线，拒绝用户密码、路径前缀、query/fragment 和无效端口。浏览器 Origin 必须与配置 scheme/Host 匹配。
- Bearer/Basic 仍必需。无 Origin 的认证 CLI 仍可使用；cross-site Fetch Metadata 仍拒绝。未知任务隔离不变。
- 代理需转发 Authorization、Origin 和 WebSocket Upgrade；后端限制在回环或私有网络。本轮不操作用户 NAS 或真实账号，不安装代理软件。

## Tasks

- [x] 管理层测试：HTTPS 外部来源经过 HTTP 后端放行，恶意来源和伪造转发头不放行；非法配置拒绝。先观察 RED，再实现共享规则。
- [x] 加入 Config.PublicOrigin / PUBLIC_ORIGIN，并接入 main Gate；现有 New(password) 调用保持兼容。原生启动脚本/Compose/env 示例携带可选配置。
- [x] 实际本机 TLS 代理 → HTTP applicationHandler 的 POST 和 WSS 状态集成，验证匿名 401、恶意 Origin 403、认证状态帧。
- [x] 两平台 race/vet、Node 回归、独立审查和说明；保存源码检查点，后续独立测试包纳入，不改旧包。

## 已发现的兼容反例

审查发现显式 :443 配置被浏览器序列化为无端口 Origin，原比较误拒绝。先 RED，再规范化配置：去除默认 443、数字端口去前导零；空冒号端口拒绝。包含 IPv6 默认端口回归。

## 证据范围

本机 TLS 代理和协议客户端证明协议与应用兼容，不等同浏览器原生 Basic 框、用户 NAS 厂商代理配置、证书部署或真实百度验收。
