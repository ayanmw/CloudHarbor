# 计划：全盘扫描覆盖快照历史

Spec: `docs/superpowers/specs/2026-11-02-full-scan-coverage-history.zh-CN.md`

1. **持久化与迁移**：先写 RED 回归；schema v37 nullable 字段，v36→v37 migration 保留旧记录空快照；完成转移时 coverage + 完成事件 + run 摘要事务一致。
2. **API 与证据**：历史运行 DTO 和导出 DTO 增加快照字段；公共 JSON 排除账号 fingerprint；旧事件数据不补造。
3. **网页历史**：展示历史快照、时间与当前覆盖状态的差异，空快照清晰标为未记录。
4. **验证与发布**：定向 RED/GREEN、全仓 Go/vet、WebUI/userscript、三平台构建；生成 `.139` 包、哈希、源码清单与验收记录。仅本机模拟和 SQLite，不访问百度。

约束：原目录内联，不用 worktree；不提交、不推送。保持现有工作区其他未提交变更。
