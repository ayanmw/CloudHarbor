# UE工作台减负 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 在v0.8.32内完成已批准UE审阅的首轮优化并真实跑测。
**Architecture:** 渐进改现有Vue模板与样式，保留API/路由/持久化/安全边界，原目录内联实现。
**Tech Stack:** Vue3/TypeScript/Vite/Node测试、现有Go服务。

## Task 1: 文件浏览减负
- [x] 新增折叠视图/高级搜索/目录工具以及退出搜索显示条件回归，先确认失败。
- [x] 修改CloudFilesWorkspace.vue，保持原控件testid、URL与门禁；收敛视图与维护面板，添加Escape关闭和焦点返回。
- [x] Vite构建后跑ui-cloud-files、ui-cloud-selection、ui-layout及cloud-files-navigation。

## Task 2: 按场景组织工作台
- [x] 分析模式初始来源及切换无POST、同步按钮后果、上传/清理折叠维护区域回归先失败。
- [x] 修改DirectoryAnalysisWorkspace、FolderSyncWorkspace、UploadWorkspace、CloudCleanupWorkspace，保留原请求与历史证据合同。
- [x] 构建后跑analysis/sync/upload/cleanup/layout相关回归并修正现有断言。

## Task 3: 下载总览与真实验收
- [x] 五列表头、计量次行、操作可达回归先失败；改App.vue和style.css。
- [x] 独立审查，串行完整打包门禁（已包含全量前端、Go/vet、旧网页与三平台），不另跑竞争的Go全量。
- [x] 真实账号桌面/390px逐项跑测，发现同类问题可修复后回归；恢复原队列、关闭服务。
- [x] 更新本版说明，覆盖程序白名单；核对源码/包/编译身份与账号数据。所有开发改动不提交、不推送。

实测追加收敛：缓存面板归属、扫描状态摘要、桌面排序说明/待更新提示、紧凑工具区与搜索范围文案均已修正并回归。当前环境截图采集不可用；真实交互、焦点及布局尺寸有记录，不宣称像素视觉验收。见发布验证目录 ue-pass-20261108/final-ui-audit.json。
