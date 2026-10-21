# 下载可靠性 Implementation Plan

> 使用 executing-plans 在当前会话逐项实施；用户已授权按优化路线持续开发并交付本地测试版本。

**Goal:** 交付 v0.1.0-preview.1，保证下载结果和源文件处理不会误报成功。

**Architecture:** 延续 Go 单进程/xpan。统一文件下载路径，目录只负责聚合；元信息与本地 SHA256 凭据辅助恢复，尚不替代后续持久化任务调度。

**Tech Stack:** Go 1.22 兼容源码、标准库、现有 gorilla/websocket。

**Spec:** [设计](reliability-design.md)

## Global Constraints

- 仅在隔离工作树修改，不推送，不访问真实网盘执行写操作。
- 不依赖未经核实的云端 MD5 语义。
- 每个交付包有版本、哈希、操作说明和验证边界。

## Review Focus

- Range 200 不得追加；206 偏移/总长错误不得污染临时文件。
- 短流与目录子项失败不得触发源文件处理。
- 同名已有文件保留，临时数据必须绑定云端身份。
- 源文件处理失败可复用完成文件重试。
- 断电窗口、符号链接与状态快照竞争。

## Task 1：传输协议和目录配置

Files: internal/baiduapi/client.go、client_test.go；internal/config/config.go、config_test.go；Dockerfile、docker-compose.yml。

- [x] 写测试：200 续传被拒绝；错误 Content-Range 被拒绝；正确 206 返回后缀；规范变量优先和旧变量兼容。
- [x] 执行测试并记录预期失败。
- [x] 实现 ErrRangeIgnored 与严格范围检查；统一部署变量。
- [x] 执行定向测试通过。

## Task 2：文件与文件夹完成契约

Files: internal/downloader/downloader.go、transfer.go、downloader_test.go。

- [x] 写本地 HTTP/磁盘集成测试：短流失败；已有文件不覆盖；断流恢复；目录失败不改名；后处理失败重试；嵌套结构正确；快照不可变。
- [x] 执行并记录预期失败。
- [x] 合并重复下载实现，返回 error；实现版本绑定临时文件、SHA256 凭据、安全路径和快照。
- [x] 执行定向测试及可用的 race 检查。

## Task 3：独立版本交付

Files: cmd/get_token/main.go；scripts/package.ps1；docs/ai/progress.md；发布包使用说明。

- [x] 修复已确认 vet 错误；添加版本显示与本地打包脚本。
- [ ] go test ./...、go vet ./...、git diff --check、Windows/Linux 交叉构建。
- [ ] 审查差异，验证包内二进制和哈希；写明真实网盘/NAS 未验收。
- [ ] 本地提交并保留不可变版本包；继续下一阶段，不把首版当作整个目标完成。
