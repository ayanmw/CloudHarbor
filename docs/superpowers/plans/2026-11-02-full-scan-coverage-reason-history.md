# 计划：全盘扫描覆盖原因快照历史

Spec: `docs/superpowers/specs/2026-11-02-full-scan-coverage-reason-history.zh-CN.md`

1. 持久化扫描完成时的 9 类原因计数，与覆盖总数、generation、UTC 和完成事件同事务提交。
2. SQLite v37→v38 迁移保留旧运行空值；新增回归覆盖重启、缓存后续变化、迁移和完成事件回滚。
3. 列表 API 与单轮 JSON 导出携带快照，旧 DTO 不伪造值，account fingerprint 仍排除。
4. 历史 UI 展示原因分布与“结构原因可重叠”，并区分历史缺失原因与当前实时值。
5. 完成 taskstore/API/UI 和全量发布门禁；修订路线图、进度和验证记录后更新现有 `.140` 包。保持 `.nas-downloader` 唯一副本，不做百度远端写操作。

## 执行记录

- SQLite v38 原因快照、v37→v38 空值迁移、完成事务、历史列表/导出、UI 展示完成。
- 定向 taskstore/API 回归、全仓 Go 测试/vet、WebUI 161/161、userscript 3/3 通过。
- Windows AMD64/Linux AMD64/Linux ARM64 构建和 ZIP 13 项/12 项载荷校验通过。
- 唯一 `.nas-downloader` 已移至 `.140`；schema v38、SQLite integrity `ok`、OAuth ready、队列 0、管理页 HTTP 200。
- 未执行百度全盘扫描或远端写操作；NAS/SMB 与 ARM64 原生运行未验收。
