# v0.8.31 真实账号写入与传输排查

用户明确授权使用唯一正式 .nas-downloader（不复制授权）建立独立可识别网盘目录，验证新建、上传、下载。真实HTTP已复现mkdir409 not_started；历史上传错误为暂停哈希扫描阻塞，当前云端哈希scan paused。启动前只改队列暂停两项设置并记录原值，旧任务/上传无活动队列，防止测试外下载。测试目录与本机fixture记录在发布同级validation-v0.8.31-real-account/test-context.json。

修复：当前执行器互斥预约保持。持有扫描/上传准入租约后，将没有执行器的持久哈希检查点记failed/restart_required（本地准入证据，不声称已发生远端变更），保留原RunID/累计/部分检查点，附事件且更新历史摘要；再次显式开始使用新RunID重扫，旧cursor不发布。该失效标识不再阻塞namespace操作；普通active/persisted队列冲突继续保护。不无条件删扫描数据、不自动恢复整盘扫描。

- [x] SQLite原子检查点失效/历史事件/注入失败回滚/不跨作用域/关闭重开持久化；新轮重扫接线静态复审；两处写入路径及UI阶段说明。
- [x] 真实HTTP测试新建独立目录、空目录、Unicode文件名、小文件/多分片上传、远端回读和真实下载内容SHA核对，失败保留状态不盲重发。实际下载68.30秒通过；监控后处理及可见UI未验。
- [x] 同版打包更新，记录真实证据、测试目录/文件范围、账号未复制、恢复原队列暂停设置并停止测试服务；残留按用户规则处理。

真实验证进一步发现PCS分片成功响应没有XPan的errno，旧解析器错误记unknown；仅为固定superfile2上传端点接受HTTP200、无错误码且合法MD5的响应，保留调用方精确摘要校验。预创建/合并仍必须有errno，失败/缺字段不自动重试。首次unknown测试记录不重新提交；round2使用新文件并已收到两项confirmed，空目录confirmed。补充测试覆盖PCS与XPan差异、错误码/坏摘要；真实下载由显式环境开关的只读验收测试执行，默认测试不读取账号。


Final package: 2026-11-06T07:50:25.2313455Z; snapshot 348ebab6280b230d23e5730103dc8ee88b01004d7c818eac97126d7b07d602d2; ZIP a0af0cbe5e4639a16ab162d51000caeacddd9345c07ab76734baecdf46f3b5fa. Frontend 617, legacy 176, userscript 3; 13 Go packages and vet passed, downloader rerun 389.397s. First gate failed on persisted-running fixture; corrected to model real active-worker admission and assert no invalidation/provider/journal. First log preserved. 53 protected files unchanged across packaging; services stopped. Manual candidate cleanup script supplied after policy rejection. Source documentation synchronized after artifact audit; frozen source is SOURCE-SNAPSHOT.txt.
