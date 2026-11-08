# 上传批次总览与阶段进度 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 上传批次能直观看到完成、处理中、等待、失败和待核对数量，以及可信的阶段进度。
**Architecture:** 独立纯 TypeScript 模块用 BigInt 计算批次容量/分片比例；复用后端现有状态，不把 sent 当实际网络流量或完成凭据。页面增加摘要与阶段解释，保留筛选/分页/操作语义。
**Tech Stack:** Vue/TypeScript、node:test/JSDOM；不改变上传协议或数据库。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-C。

## Global Constraints
固定 v0.8.31；原目录实现，不提交、不使用 worktree；不启动真实账号；本轮不发布，不虚构速度/ETA或分片恢复。

## Review Focus
大于2^53字节仍精确；unknown不计完成；零字节文件只凭confirmed完成；非法sent/超过size响应拒绝；合并不显示文件已完成。

### Task 1
- [x] 先添加摘要/比例/非法输入纯函数测试并观察失败。
- [x] 实现精确摘要与阶段说明，接入响应校验。

### Task 2
- [x] 上传详情新增摘要、分阶段进度与解释；UI测试验证unknown/处理中/大字节。
- [x] 构建、全前端回归、独立审查，更新版本说明；不覆盖已发布程序。

## Ledger
后端 sent 仅记录当前尝试的已确认分片，confirmed写入size也可能来自快速创建；因此不将总sent宣传为实际网络上传字节。速度/ETA仍需真实活跃计时后实施。

2026-11-05：纯函数测试先观察缺模块失败，实现后3/3；UI新增排序定位误用行号曾失败，改按文件路径定位后，上传相关17/17、完整前端358/358通过。Vue类型检查/构建、独立只读审查、CRLF感知diff检查通过。未做本轮可见浏览器/真实上传验收，未发布；运行包仍为下载闭环 ca40fd2341ac7ee6，程序/账号状态未改动。
