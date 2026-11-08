# 入队下载等待原因 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 对已关联且 pending 的根任务展示当前阻碍，历史观察不用于推测当前可下载。
**Architecture:** 在已有 GetIntakes 队列锁快照内读取停止/存储错误/维护/暂停/授权/隔离/eligible，按实际调度限制优先级生成可选 wait_reason；只读不改队列。
**Tech Stack:** Go、Vue、现有模拟测试。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-B。

## Global Constraints
固定 v0.8.31；原目录，不提交、不操作真实账号；等待原因是读取快照，非持久化历史事实。

## Review Focus
暂停优先于缺扫描；授权不可用；隔离不重试；身份变更不可调度；非 pending 不显示过时等待。

### Task 1
- [x] 表驱动 mock 测试先失败，再实现只读等待原因。
- [x] UI 标签/响应校验与回归；独立审查，更新记录。

## Ledger
上一轮完整 Go suite 仍在原 session 18771 运行，不重启；本轮另做新逻辑定向测试。

2026-11-05：新增测试先观察缺 wait_reason 失败；实现后覆盖全部原因与非 pending 清空，通过；38 项入队 UI、353 项全量前端、Vue 类型检查/构建、独立只读审查和 CRLF 感知 diff 检查通过。未进行本轮可见浏览器验收、整包发布或真实账号操作。
