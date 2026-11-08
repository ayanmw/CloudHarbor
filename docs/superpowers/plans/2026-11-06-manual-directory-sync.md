# 真实网盘基础测试、缓存日志和手动目录同步实施计划

> 按superpowers:executing-plans在主目录内联执行；不创建worktree、不提交或推送。当前用户已授权实施，普通选择不重复请示。

**目标：** 真实基础API验收，缓存逐轮变更可解释，提供有完整SQLite证据的手动单向/双向目录同步。

**架构：** 保留现有缓存刷新原始日志并增加按fs_id关联的变化视图；新建内部同步模块持久两端快照、共同基线、运行和步骤，远端适配现有Downloader操作，Vue工作区提供配置/手动执行/记录。

**技术栈：** Go、SQLite、Vue/TypeScript，现有BaiduAPI客户端。

**设计：** ../specs/2026-11-06-manual-directory-sync.zh-CN.md

## 全局约束

- 账号仅使用releases/v0.8.31/.nas-downloader原件；真实写入限独立测试根目录。
- 首次合并不传播缺失删除；单向网盘→本地，双端冲突保留。
- 计划与intent先落盘，unknown不重发；失败不能发布共同基线。
- 同版本覆盖程序白名单，保留数据；结束停止测试服务并恢复原暂停配置。

## 工作项

- [x] A：基础API真实矩阵及缓存机制调查，保存每步请求身份/时间/回执/远端回读。
- [x] B：缓存刷新运行明细关联移动/改名，保持原始证据及分页精度；真实差异、无差异、重开核对。目标先刷新旧路径丢失已复现并修正；两种顺序真实验证通过。
- [x] C：同步SQLite、双端扫描与三方差异计划；测试首次合并、删除/修改冲突、稳定身份移动、目录子树与失败基线不推进。pull本地偏离不吸入共同基线，跨两轮真实冲突验证通过。
- [x] D：逐项执行、原子本地发布/隔离、远端接线、未知恢复；测试异常及重开，不盲重发。备份后再次核对删除对象、应用身份绑定、路径与整数证据回归通过。
- [x] E：网页配置、手动触发、批次/逐项表格与证据导出；真实双端验证，完整门禁及同版更新。

## 重点复核

扫描失败与根路径消失不能形成删除；双端冲突不覆盖；目录/子目录操作不重复；目标占用与文件身份变化停止；未知intent跨重启仍保持隔离。全量UI/Go/vet/三平台、真实百度、可见UI、NAS分别记录。

## 执行记录

真实验收使用唯一安装目录账号数据，脚本和独立夹具位于发布同级validation-v0.8.31-sync。基础矩阵、外部变更缓存检测、两轮目录同步矩阵均通过；第二轮含pull本地先改/远端后改仍保留冲突、两端内容匹配后继续、重启基线保持。所有写入限独立`/_NAS_Downloader_*Test_20261106_*`目录；服务按finally停止并恢复原暂停设置。

采用writing-plans与executing-plans在主目录执行，requesting-code-review两项定向只读复审发现的问题均修正并加回归；未提交/push/worktree。没有把mock作为真实验收：纯算法和故障注入仅证明冲突/事务/恢复，真实HTTP与供应商链路另存证据。正式包门禁和最终产物核对仍在后续收尾步骤。


Final same-version delivery: 2026-11-06T13:00:43.4496378Z; snapshot e98e80e9ac349b3ffcefc68b52780da383f3b92473431d808c9da4f5f91e9e1e (1066 files); ZIP 9b81ffe98fc1a82d6f01b4e729b9b0538789733d063bfef0ccbd3788bb845f3b. Gate: Vue620/legacy176/script3, 14 Go packages, vet, three CGO0 targets. Independent ZIP14/payload13/source1066/runtime identity audit passed; 56 protected files unchanged across packaging and 3 prechecked SQLite files unchanged. Services stopped, pause restored. One local HTTP reply reset retained as interrupted run without steps; readback and fresh no-change check passed, root cause not reproduced. Candidate cleanup denied by policy; user script cleanup-sync-candidate-20261106.ps1 supplied and parsed. Source delivery documents updated after artifact audit; frozen build identity remains SOURCE-SNAPSHOT.
