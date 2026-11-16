# v0.8.34 本站文件库管理 Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline; one independent final review. No worktree.

**Goal:** 单文件库基础写管理、可恢复删除、持久操作证据及真实UI闭环。
**Architecture:** internal/library SQLite意图日志＋受限文件句柄无覆盖操作，Vue工作台复用浏览状态。
**Tech Stack:** existing Go1.27.1 / os.Root / x/sys / SQLite / Vue3。
**Spec:** ../specs/2026-11-17-library-management-0834.zh-CN.md

## Global Constraints
- 不覆盖用户文件、无永久删除、无Client、无账号副本；只有本版收尾tag与Release。
- 幂等ID及未知隔离必须先于写入口；文件系统与SQLite跨事务不假装原子。
- 新写操作只接受新鲜身份与root版本；链接／私有区／越界拒绝。

## Review Focus
- 上传取消／短读、磁盘失败、提交后掉响应不得覆盖或盲重发。
- Windows相对句柄rename与Linux NOREPLACE，目标被外部创建不能覆盖。
- 目录及回收对象身份变化、恢复目标被占、根离线／换根。
- mutex顺序、下载互斥、重启unknown，历史读不可恢复执行权。
- 前端配置代际、页面离开、批量部分失败、宽／窄屏及键盘交互。

### Task 1: durable writes
- [x] 红测intent/幂等/重启隔离/上传及目录操作/回收恢复/无覆盖链接边界。
- [x] 实现operation journal和平台无覆盖rename，真实文件定向转绿。
### Task 2: HTTP integration
- [x] 红测上传流限制、短读取消、鉴权／同源、历史结果读取；实现路由接线并转绿。
### Task 3: frontend
- [x] 红测新建／行操作／回收恢复／批量上传／失响应查原ID，完成UI与构建回归。
- [x] 真实样本HTTP及浏览器宽窄屏、下载字节、重启验收，关闭服务。
### Task 4: delivery
- [x] 独立审查并修复，完整串行package及产物验证。
- [x] 唯一账号移动至34，保留下载路径、SHA及SQLite完整性；清理自有样本或手动脚本。
- [ ] 提交、推送、手动工作流、最终tag、Release、公开下载与本地同版覆盖，关闭目标。

## Execution ledger
2026-11-17：用户明确要求切下一版、先安排目标与验收再直接实施。工作区干净，v33最终Release已完成。采用无覆盖上传、软删除／恢复和unknown不重放；不是额外请求审批，不创建启动tag。旧v33清理被拒绝目标不重试。

Ruling: 私有区marker绑定持久owner token和根身份，操作记录独立保存配置版本；这样账号版本目录移动不会丢失所有权，相同根重新配置也不会使回收站失效。根身份不同仍拒绝恢复。取消上传仅清理已核对属于本次操作的临时文件，未知结果不清理。

Ruling: 系统明确返回的存在、缺失、权限、共享占用和不支持／跨文件系统错误记失败，关闭占用或修正参数后可新操作；I/O结果不明及后置校验／持久化失败仍unknown。已以Windows真实无删除共享句柄复现普通占用误隔离，补红绿回归。

2026-11-17实施验收：完成真实文件和HTTP红绿回归；独立审查发现并修复staging替换身份接受，另外复现并修复目标父目录替换、普通文件占用误隔离；补发布前重读SHA。真实网页测试多文件／目录上传、改名移动、回收恢复、子目录冲突后续成功、批次切目录不转投、折叠、键盘及390px布局。手机按钮遮名、子目录双斜杠及目录准备停止后继续创建均有红绿回归并修复。真实进程中断上传后重启unknown、禁止写及切根无重放，8个SQLite完整性通过，服务已关闭。证据在releases/validation-v0.8.34。尚待完整package与最终发布，不自动35。

2026-11-17发布前冻结：完整package成功（715/176/3及Go/vet/三平台）；13文件SHA、源码快照和架构通过。32真实账号文件同SHA移动到34，源路径不存在、3个SQLite完整性正常。测试服务关闭。自有47文件样本清理被自动审批拒绝，提供清单校验手动脚本（未执行）。尚待提交／手动工作流／最终tag／Release和公开资产复核，结案记录将保留在外部validation目录，不能预先把线上验证记为通过。
