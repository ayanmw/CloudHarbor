# 持续优化进度

## 总目标

按已给出的 A/B/C/D 优化路线持续推进，阶段性交付独立可测试版本。首版不是整个目标完成。设计见 [reliability-design.md](reliability-design.md)，当前计划见 [reliability-plan.md](reliability-plan.md)。

## v0.1.0-preview.3（最终交付版）

- 工作树：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-codex`；分支 `codex/downloader-reliability`；基线 `331a8b1`。
- 配置：统一 LOCAL_DOWNLOAD_DIR，保留旧变量兼容。独立监听地址默认回环，容器宿主端口默认回环。
- 传输：Range 忽略后从头请求；验证 Content-Range；元信息绑定临时文件；完整长度、同步、关闭后生成 SHA256 凭据；无覆盖发布；无凭据旧文件报告冲突。
- 目录：子项失败汇总，全部成功才处理顶层；不逐项改名；旧标记子项阻止整体后处理。
- 后处理：显式失败可重试，复用经过 SHA256 检查的本地文件；检查 filemanager 子项 errno。
- 并发：扫描执行前认领顶层 fs_id；客户端替换加锁；任务返回不可变快照；扫描时间用 atomic。尚无限并发队列。
- 分页提前：通过 SDK 源码确认 start/limit 参数，循环读取；重复 FSID 或无成功标志/无列表/HTTP 错误直接失败，避免空目录误判。
- 新增本地 HTTP/磁盘故障测试；先观察 RED 再实现。RED 覆盖忽略 Range、错误范围、配置兼容、短流、目录误成功、无凭据旧文件、后处理失败、完整大小失败残片、1000 项分页、错误目录响应、旧标记子项和官方 server_mtime 字段。
- 独立只读 reviewer 报告的问题已修复并经其非缓存测试复核；无剩余确定的 critical/important 问题。断电、远端并发修改、硬链接/NAS 与配置根路径符号链接属于明确边界。

## 已执行验证

- Windows Go 1.27.1：`go test -count=1 ./...`、`go vet ./...`、`git diff --check` 通过。
- Windows race：默认 GCC 为 MSYS 编译器，初次失败；改为 `C:/msys64/ucrt64/bin/gcc.exe` 后 `go test -race -count=1 ./...` 通过。
- WSL Go 1.22.2：`go test -race -count=1 ./...` 通过，验证最低项目版本与 Linux 文件行为。
- 打包脚本与 Windows 启动脚本 PowerShell 语法解析通过。
- 最新并发扫描/快照测试已通过 Windows/WSL 全模块 race 复核。
- 内部 preview.1 三平台构建通过；解压后 10 个文件哈希通过，Windows/WSL AMD64 --version 通过，ARM64 ELF machine=183 通过。Windows 回环 HTTP 健康/页面/资源/状态/手动扫描通过；全部百度请求强制走拒绝连接的回环代理，不访问真实账号。
- preview.1 冒烟发现 url.Error 会把带 Token 的请求 URL 带入日志；增加先失败的凭据泄漏测试，脱敏各 API 的请求错误。最终对外交付 preview.2，内部 preview.1 保留为不可变历史，不建议测试。
- 最终 preview.2 源码 revision：`d56d114dcbdab79b720ed40a815d17ecc4f5110a`。三平台构建通过，解压后 10 个文件哈希通过；实际运行包内 Start.ps1，Windows 回环 HTTP 健康/页面/资源/状态/手动扫描通过，网络错误日志不再出现假 Token。WSL AMD64 --version 与启动脚本语法检查通过。冒烟进程已停止。
- 交付包：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.1.0-preview.2.zip`；SHA256：`d21a8e936955925575aaa76c45ce694de87533acaacfb493ba1cdc9a9e1efcae`。
- 包含 Windows AMD64、Linux AMD64/ARM64 二进制、两种原生启动脚本、Docker Compose/运行镜像配置、测试指南、版本来源和 SHA256 清单。ARM64 只完成交叉构建与 ELF 架构检查，Docker 运行配置只完成静态检查，未运行 Docker。
- 原项目 main 工作区仍为基线且干净；优化源码位于隔离分支，未合并/推送。

## 下一版本

交付前 reviewer 补查发现下载链接解析失败仍可回传带 Token 的 URL，已补两种下载入口的 RED/GREEN 测试并修复。preview.1/preview.2 保留内部历史，最终使用 preview.3；其源码 revision 与包验证见 releases 目录对应 validation.md，不复用旧包的哈希。

继续 B：持久化任务与状态迁移、有界 worker、取消/优雅退出、超时与分类重试。随后 C：Token 应用/保存/自动续期、管理认证、健康检查；D：入队与管理体验。源码 README 原有 OAuth/油猴功能表述不能当作已完成证明。

## 未验收

真实百度账号下载、真实 NAS/容器运行、突然断电、多日稳定性、非本地文件系统挂载。未推送、未部署、未修改真实网盘。
