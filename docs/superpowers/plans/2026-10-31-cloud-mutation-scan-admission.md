# 网盘变更和扫描准入互斥计划

## 当前候选进度

该项为 `.106` 第 5 项，补齐 `.105` 后的本机缓存扫描一致性批次。

## 实施和验证

- [x] 回归覆盖扫描准入窗口内的远端 rename。
- [x] 网盘变更使用 shared inventory scan reservation，先于维护锁、操作日志和百度 API。
- [x] 验证拒绝时 provider action 和 operation journal 都为空。
- [x] 与 persisted duplicate-scan 冲突和目录队列冲突回归一起通过。
- [ ] 跑完整 `.106` 发布门禁，核验 Windows/Linux 载荷与 ZIP。

## 边界

本机自动化验证；不连接真实百度账号或 NAS/SMB，不使用 worktree，不提交或推送。
