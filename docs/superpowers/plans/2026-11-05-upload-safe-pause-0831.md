# 上传安全检查点暂停 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 能请求暂停当前上传，持久化意图并在安全检查点停下，保留本机暂存和未知提交隔离。
**Architecture:** 复用SQLite events保存暂停/继续意图，不覆盖正在提交的阶段；内存缓存意图，worker在下一阶段副作用前检查。queued直接paused，活跃请求不强制取消；提交已确认可正常完成，未知结果仍unknown。重启hashing/uploading且有暂停请求时paused，submitting/finalizing仍unknown。
**Tech Stack:** Go/SQLite、Vue后续接线。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-C。

## Global Constraints
固定v0.8.31；原目录、不提交、不worktree、不真实账号操作；不新增表结构。继续以新尝试执行，可能重传分片，不宣称uploadid恢复。

## Review Focus
请求中的提交不强停；未知不可放行；请求跨重启；写失败原子回滚；暂停保留文件/停止才回收；claim和控制互斥。

### Task 1: 持久化控制与worker
- [x] queued暂停/继续、活跃安全点/未知、重启与原子失败测试，先红后绿。
- [x] 暂停缓存、检查点、恢复/回收接线，相关Go验证及审查。

### Task 2: API与UI
- [x] 受鉴权按item暂停/继续接口，明确requested与paused，行操作及状态/恢复历史兼容。
- [x] 页生命周期、失败核对、跨批次、暂停期间继续接收等测试。

### Task 3: 交付
- [ ] 桌面/手机可见与完整门禁，同版覆盖保留账号。

## Ledger
本轮先落实Task1；未提供API/UI前不发布。暂停请求不会取消网络调用，最迟在当前请求/本机校验块结束后的下一个安全回调检查。

2026-11-05 Task1：queued可持久暂停/继续，活跃项只记请求，在后续安全回调停下；已准入请求可能继续执行，不保证暂停请求后零新网络活动。哈希每4MiB检查，重复同阶段/同字节回调不写重复事件。回调暂停时保留刚确认的bytes；未知提交优先隔离，成功提交显示confirmed。暂停跨重启及取消回收已验证，估时快照排除待暂停项。
审查发现旧暂停意图在失败后重试/重启复活；新增回归先观察失败，再由Retry/Resume共用事务重排清除意图修复，复审通过。原子写失败/保留文件/未知拒绝/成功完成不伪暂停等8个顶层测试通过（含多阶段子例）。主程序24.906s、uploads9.606s、baiduapi及相关vet通过；hash检查点中断前不发远端请求已单独验证。
Task2 API/UI及恢复历史paused兼容尚未接入，源码未发布、未操作真实账号。旧运行包仍是上传中心统计版04939d9b69e1a89b。

2026-11-05 Task2：逐文件POST暂停/继续已接入管理鉴权，严格核对batch/item与回执；UI分别表达待安全点、已暂停和新尝试，不自动重发。控制时清除旧估时，保留浏览器正在接收的其他文件。恢复历史接受安全阶段转paused，提交阶段仍拒绝伪paused。32项上传相关测试、373项全量前端、类型检查和构建通过；主程序25.016s、uploads10.017s、baiduapi1.062s通过，uploads/baiduapi vet通过。Task3可见验收与整包门禁尚未进行，本轮未覆盖运行包、未启动真实服务。
2026-11-05 Task3可见：桌面1440x1100与390x844本机模拟通过，暂停显示等待安全点、继续显示新尝试且焦点回位，unknown无继续入口；窄屏文档宽375<=390，表格容器内滚动。截图保留releases目录；mock服务、脚本和浏览器页已清理。完整门禁/最终覆盖证据待包外验收登记。
