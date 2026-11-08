# 上传重启恢复关键事件 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 重启导致的上传状态变化有原阶段、时间和分片量证据，且事件与状态原子保存。
**Architecture:** 沿用现有 SQLite events，不增表/不迁移结构。启动恢复用独立事务，先 INSERT SELECT 保存原阶段与原updated UTC、sent、目的状态及恢复UTC，再更新 items。重复打开无新中断状态就不重复记事件。
**Tech Stack:** Go、SQLite、临时目录测试。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-C/F。

## Global Constraints
固定v0.8.31；原目录、不提交、不worktree、不操作真实账号。hashing/uploading重新排队不代表可信分片恢复；submitting/finalizing仍unknown，不重发。

## Review Focus
四种中断阶段；原sent保留；事件失败回滚；二次重启无重复；恢复UTC不替代原UTC。

### Task 1
- [x] 四阶段恢复事件与二次打开测试先失败。
- [x] 启动恢复事务与原子失败测试。
- [x] uploads完整测试、受影响接口定向检查/审查，更新版本说明；不发布。

## Ledger
本阶段完善恢复证据，尚不提供分片uploadid恢复或速度ETA，也不声称用户端可以读取尚未接入的事件。

2026-11-05：新增四阶段事件测试先观察无事件失败；补事务后完整 uploads/baiduapi 测试和 uploads vet 通过。事件插入失败/状态更新失败均回滚，原阶段/原UTC/分片量保持，二次重启不重复。独立只读审查无问题。接口定向命令未匹配测试，不计接口验收。首次失败测试遗留目录已逐文件清除并确认不存在；真实数据及运行包未动。未添加数据库结构或前端事件读取能力，后续继续接入。
