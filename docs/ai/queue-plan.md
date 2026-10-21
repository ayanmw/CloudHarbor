# 持久化后台调度 Implementation Plan

**Goal:** 实现阶段 B，形成下一份有明显使用收益的独立测试版本。

**Spec:** [queue-design.md](queue-design.md)

**Execution:** 用户已授权持续实施；当前会话 inline 执行，测试先行；阶段末独立 review。保留原 main 与旧发布包。

## Task 1：独占的逐任务存储

- [x] `internal/taskstore` 增加 Open/Load/Save/Close 与 Windows/Linux OS 锁。
- [x] 先失败测试覆盖持久化、损坏拒绝、队列绑定、并发实例排他与崩溃锁释放。
- [x] 原子写单任务，无全量队列快照放大；保护状态目录中的符号链接。

## Task 2：可取消的 API 与错误分类

- [x] `baiduapi.Client.WithContext` 保留现有接口，所有 HTTP 调用传播 context。
- [x] 元数据超时、类型化 HTTP/业务错误、脱敏错误链。
- [x] 先失败测试覆盖挂起请求取消、HTTP 429/5xx 与永久错误区分。

## Task 3：持久化有界调度

- [x] 新增根任务队列、固定 worker、恢复后扫描门禁、mode 固定、重试时间。
- [x] 接入下载器与目录错误链；停止/取消前检查 context，保留临时文件。
- [x] `main` 接入启动检查、signal 优雅退出和合并扫描；显示调度信息。
- [x] 故障测试证明并发上限、持久化重启、取消不处理源、退避和重复扫描。

## Task 4：交付

- [x] 更新配置、启动脚本与测试说明。
- [ ] 全模块 test/vet/race，Windows/Linux 构建，独立 review 与包内冒烟。
- [ ] 新版本包、哈希、准确验证边界；C/D 阶段仍保留在持续目标中。
