# 上传分片计时与统计 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 用真实分片请求观察支持上传活跃时长、平均速率和ETA，排除离线与非传输阶段。
**Architecture:** 仅在分片HTTP请求中，在pacer后使用单调计时，统计请求体中file payload被传输层读取的字节；每个请求结束保存attempt_id/part_index/UTC/duration/payload观察到既有SQLite events。后续只聚合有证据的样本，不用state更新时间相减，也不把请求读取等同远端确认。
**Tech Stack:** Go io.Reader/http/SQLite，Vue后续消费。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-C。

## Global Constraints
固定v0.8.31，原目录、不提交、不worktree、不真实账号操作。未知提交保持隔离；不增加表结构；离线、排队、哈希、合并和pacer等待不计为分片请求时长。

## Review Focus
multipart开销排除；部分读取失败仍有真实观察；快速创建没有分片样本；pacer等待排除；样本保存失败不得继续合并；崩溃前未保存区间不补算。

### Task 1: 请求测量并逐分片落盘
- [x] 先写payload与请求样本测试并观察失败。
- [x] 增测量上传入口，兼容原入口；runner显式传样本回调，SQLite保存attempt与样本。
- [x] provider/manager测试、构建及审查，记录证据。

### Task 2: 聚合与前端
- [x] 聚合已落盘样本/活跃状态，展示时间、平均请求速率和有条件ETA，明确历史/当前、样本覆盖与分片确认口径。
- [x] 跨重启/重试/零文件/快传/失败验证和UI回归。

### Task 3: 交付
- [ ] 桌面/窄屏可见验收及完整发布门禁，同版覆盖，保留账号数据。

## Ledger
本次先完成Task1，不把观察字节称为线缆流量或远端已收字节。响应等待在请求持续时间内；进程崩溃前未持久化的单个请求区间只能缺失，不能用墙钟补造。

2026-11-05 Task1：先观察缺reader编译失败，再实现；模拟极快请求曾测得0纳秒，测试改为允许真实0值，聚合不得补时长。验证multipart头尾排除/Content-Length不变/pacer后起计/部分读取失败/快创建无样本/保存失败停止合并/跨重启原样保留且不改item.sent。主程序、baiduapi、uploads测试及相关vet通过；独立只读审查无阻断。前端未变，无重复前端回归；未真实上传、未发布。Task2/3继续，不将Task1视为速度ETA已完成。

2026-11-05 Task2部分完成：新增受管理鉴权保护的显式GET transfer-statistics，SQLite只读事务按批次/文件汇总BigInt样本；均速仅使用正时长样本字节，零时长字节仍保留总读取量，离线/跨重启不补时长。网页懒展开/手动刷新，展示请求时长、包含重试的均速、覆盖文件/样本数、每页100文件，保持历史口径及生命周期取消。
Ruling：ETA仍需当前attempt和实际剩余分片计划证据，现有样本不足，Task2的原ETA要求保持未完成；本阶段累计统计不是ETA替代品。
验证：uploads/baiduapi测试、类型构建、23项上传相关与364项全前端通过；BigInt大数、跨重启/年级墙钟间隔、零时长排除、批次隔离/坏证据、页面迟到401与分页均验证。审查无阻断；相关vet通过。
主程序回归首次在ScannerAutomaticallyRecoversDownloadAfterRenewal出现3秒完成等待失败，未改测试或实现来掩盖；单独复现通过，串行全主程序24.318s通过，目标连续3次11.975s通过。根因未确定，保留待观察，日志在releases/v0.8.31-upload-statistics-main.log与v0.8.31-scanner-renewal-repeat.log。未操作真实账号、未做本轮可见验收或发布。

2026-11-05 Task2收尾：样本增加可选的本次计划总量/分片数、当前分片长度和MD5确认结果；预校验完整block list后才发送，服务端已有块不计入计划。worker保有仅本进程的attempt身份/阶段revision；统计SQL事务前后快照一致且数据库uploading才估算，不用历史尝试、墙钟或读取量充当已确认量。剩余=计划-确认，按本次有效请求比率向上取整。legacy/失败/零时长/零payload/完成/合并/结束/重启/换attempt不估算。
页面展示“本次剩余分片请求估时”及计划/确认/剩余/样本依据；明确排除限速等待、合并和最终核对，不当完成时间或实时倒计时。逐分片账本只为活跃尝试构建，历史累计仍完整。
验证：主程序串行26.984s、uploads/baiduapi完整测试、相关vet通过；新增provider已有块/重复计划拒绝、跨尝试/重启等测试通过；24上传相关与365全前端、类型构建、diff及独立复审通过。本轮没有再次触发此前待观察的scanner续期超时，但不将此前问题标为已解决。Task3仍待可见验收/整包发布；真实账号与运行包未改。

2026-11-05 Task3可见验收：1440×1100与390×844模拟页面核对批次阶段、恢复原时间/恢复时间、大序号、请求统计与当前条件估时，Enter展开估算依据可达。手机摘要改双列，表格只在自身容器滚动，document宽375≤viewport390。截图保存在releases/v0.8.31-upload-*.png。模拟服务已停止、脚本已删除，更新前46真实文件摘要BEEE952EE89E703A0D58B83AADC6678C8E1A6CD610528A802DC6EA9571C6B610，目标程序进程0。完整门禁和发布核对以外部validation最新段收尾，源码冻结后不改包内文档避免快照不一致。
