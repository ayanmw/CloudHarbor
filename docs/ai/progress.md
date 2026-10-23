# 持续优化进度

## v0.7.9-preview.1（WebSocket 依赖维护与恢复回归）

- 将 `github.com/gorilla/websocket` 从 v1.5.1 更新到 v1.5.3；此版本修复子协议选择和随机值生成等问题。升级后的库不再声明 `golang.org/x/net` 依赖，`go mod tidy` 删除了原 v0.17.0 间接依赖，项目仍保持 Go 1.22 兼容目标。
- 同包纳入队列重启后复用已发布文件的回归，以及下载响应头停滞时从请求开始计时的超时回归。Windows 当前 Go 工具链、WSL Go 1.22.2 全模块 race/vet、Node 15 项和差异检查均通过。
- 独立包构建和包内启动验证在提交后执行，并在交付记录中补充版本 revision、ZIP 哈希与验证边界。

## v0.7.8 后续：已完成文件凭据身份漂移回归（仅测试）

- 新增完成文件身份变化测试：旧版本已有 SHA256 完成凭据、本地目标内容有效时，云端 MD5 变化必须保留原文件，不能下载新内容，也不能执行源操作。
- 当前 transfer 实现符合该约束；Go 全模块测试、`go vet`、WSL 全模块 race 和 diff 检查通过。此项只增加回归覆盖，不改变运行时二进制，因此 v0.7.8-preview.1 仍为当前独立测试包，不重打相同行为包。

## v0.7.8 后续：发布文件后的队列崩溃恢复回归（仅测试）

- 新增持久队列恢复用例，模拟根任务已持久化为 `downloading`，本地最终文件和匹配的 SHA256 完成凭据已发布，但进程在开始网盘后处理前中断。重启并成功扫描后，零次重新请求文件流、原文件内容不变、删除操作恰好一次且任务恢复为 done。
- 定向用例、Go 全模块非缓存测试、`go vet ./...`、WSL `go test -race -count=1 ./...`、Node 15 项和 `git diff --check` 通过。该用例验证既有实现，不改变运行时二进制；本次不生成内容相同的新包，v0.7.8-preview.1 仍为最新独立测试包。

## v0.7.8 后续：下载响应头停滞超时回归（仅测试）

- 新增本地 HTTP 服务器不返回响应头的挂起场景，验证下载空闲计时从请求发起时开始，返回可重试的 idle timeout；已有响应体停滞和持续传输超过 idle 周期的测试继续覆盖另外两种流状态。
- 定向测试、Go 全模块非缓存测试、`go vet ./...`、WSL 全模块 race、Node 15 项和 `git diff --check` 通过。运行时逻辑原已实现，属于失败路径测试补强，不改变二进制且不重打相同行为包。

## v0.7.8-preview.1（身份变化任务恢复已交付）

- 源码 revision：`ead3278c9673c3abb4e758d347f34dd5aa380f34`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.8-preview.1.zip`；SHA256：`1b366832816eb3917ec014ecc5da3046bf450827534730d33d317b1eede60e3a`。
- 源项身份复核失败曾按普通 failed 保存；若下一次列表仍返回旧元数据，用户仍能重试旧任务，造成重复下载且必然再次拒绝源操作。
- 现在持久化 `source_state=changed`：阻止旧身份的手动重试，不隔离源路径；下次扫描确认同一 fs_id 的新元数据后，才替换为新任务。重启后仍保留不可重试状态。结果未知的 `source_unknown` 策略未变化。
- 新 RED/GREEN 覆盖旧列表状态下 Retry 返回冲突、进程重开后仍冲突，以及新目录元数据出现后自动重新入队并完成；Go 全模块测试/vet、WSL race 全模块、Node 15 项通过。发布脚本三平台构建与哈希检查通过；Windows/WSL Linux AMD64 版本/revision 和 ARM64 ELF 架构匹配。正式包 Windows 启动验证认证 health、匿名 401、页面和资源、无百度授权时云目录 409，以及 Ctrl+C 停止和端口释放。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.8-preview.1-validation.md`。

## v0.7.7-preview.1（源项身份复核已交付）

- 源码 revision：`f7b220b333ae98c6cf98140cf1b6b6aec0760a6d`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.7-preview.1.zip`；SHA256：`b918662e5cd783b0f90568af8ab936efe42b76d9112b1a263ca53ca4b4d98522`。
- 用户确认路线 1：保留自动源改名/删除，但操作前重新核对身份。持久化任务现在通过 `fs_id` 读取最新文件元数据；身份字段变化或源项已不存在时不发送改名/删除，任务失败并保留本地下载结果。
- 可比身份字段：`fs_id`、完整路径、名称、大小、`server_mtime`、目录标记和 MD5。API 错误仍走现有重试/认证分类；请求已发送后的未知结果仍持久化隔离，无人工放行入口。
- 尚存边界：身份读取与后续改名/删除不是原子操作，读取完成后云端仍可能立即变化；测试与本地 API 夹具不能证明百度服务端真实并发行为。
- RED/GREEN 覆盖元数据查询参数/解码及源路径变化时不执行删除；Go 全模块测试/vet、WSL race 全模块、Node WebUI/userscript 15 项通过。三平台构建、ZIP payload 哈希/目录一致性、Windows 与 WSL Linux AMD64 版本/revision、ARM64 ELF 架构通过。后续补充了 Windows 正式包实际启动、认证/匿名 HTTP、管理页/脚本资源、未授权云目录拒绝和 Ctrl+C 退出检查。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.7-preview.1-validation.md`。

## 总目标

按已给出的 A/B/C/D 优化路线持续推进，阶段性交付独立可测试版本。首版不是整个目标完成。设计见 [reliability-design.md](reliability-design.md)，当前计划见 [reliability-plan.md](reliability-plan.md)。

## v0.7.6-preview.1（管理页大目录分页优化）

- 源码 revision：`9e018c2a4854c1f6ca1b6f07a5813ef5b32837ee`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.6-preview.1.zip`；SHA256：`cfa810c5b3d0d6f04a5398d1e420e26e1d1a9c67f5844c65501ba299bb79ca9b`。
- `/api/cloud` 过去先完整遍历网盘目录，再裁剪当前 100 项页面；目录越大，每次翻页都重复读取全部目录。现在直接按 Baidu `start/limit` 读取请求窗口，并额外读取 1 项判断是否有下一页，成本与当前页接近。
- 上游列表没有可依赖的准确总数，因此 `total` 明确返回 `null`，管理页显示“总数未知”和是否仍有下一页，不伪造总数。页内路径校验、重复 FSID 防护和既有隔离行为保留。
- 集成回归用 250 项模拟目录请求第 2 页，确认仅向上游发一次 `start=100&limit=101` 请求；Node UI 回归检查未知总数提示和翻页 offset。
- 真实百度目录分页/并发增删一致性尚未验收；变化中的目录采用 offset 分页，翻页期间新增/删除可能改变后续页内容。
- 发布脚本 `go test ./...`、`go vet ./...`、Windows AMD64/Linux AMD64/ARM64 构建通过；WSL `go test -race -count=1 ./...`、Node WebUI/userscript 15 项、payload 哈希、ZIP 内容一致和 Windows 版本命令通过。Windows race 运行受本机 GCC/MinGW 链接器失败影响，未作为通过证据。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.6-preview.1-validation.md`。
- 接续已确认的优化：网盘源项在改名/删除前重新核对身份；保持未知结果隔离且无人工放行入口。

## v0.7.5-preview.1（配置与 API 响应校验测试包已交付）

- 源码 revision：`9061e8aa2badb83bfaaf5e00de4812927c6042c4`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.5-preview.1.zip`；SHA256：`8e6d6c80310fa681a828ef230a4729121afc5af5f721b7d35652256dcae6d57c`。
- 数值环境变量显式设置非数字时启动失败；百度目录列表和下载链接响应超上限时拒绝，避免截断后的合法 JSON 被误认成完整响应。超限 503 仍保留原可重试分类。
- Node WebUI/userscript 14 项、发布脚本中的 Go 全模块 test/vet、Windows AMD64/Linux AMD64/ARM64 构建通过；WSL Go 1.22.2 全模块 race 通过。11 项 payload 哈希、ZIP 与发布目录一致，AMD64 版本/revision 精确匹配，ARM64 ELF 为 AArch64。
- Windows 与 WSL Linux AMD64 正式包副本均实际启动：认证 health=200、匿名=401、版本/revision 匹配；Ctrl+C 后服务停止且端口释放。验证记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.5-preview.1-validation.md`。
- 无真实百度凭证或云端操作；厂商 NAS、ARM64 实机、多日与断电仍未验收。未知结果目录继续隔离，无人工放行。

## v0.7.4-preview.1（Compose 自定义宿主端口/扫描间隔测试包已交付）

- 源码 revision：`5cc32059ad3f7042353b232d9ac6dc40fa5e4ca0`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.4-preview.1.zip`；SHA256：`9766ac64e5598b7da67cb3da2ce02d8cc0342d1466ab6790dab5710f6c977f7b`。
- 发布 Compose 允许在 `.env` 设置 `HOST_PORT` 和 `SCAN_INTERVAL`；默认回环端口仍为 18089，扫描间隔 30 秒。配置展开测试和正式包副本内实际 Compose 容器运行通过，认证 health 200、匿名 401，运行日志确认自定义扫描间隔，版本与 revision 匹配。
- Node WebUI/userscript 14 项、Go 全模块 test/vet、Windows AMD64/Linux AMD64/ARM64 构建通过；发布包 11 个文件哈希匹配，ARM64 ELF 为 AArch64。验收记录：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.4-preview.1-validation.md`。
- 无真实百度凭证或云端操作。厂商 NAS、真实百度、ARM64 实机、多日和断电仍未验收；未知结果继续隔离，无人工放行。未 push/deploy，主工作区干净。

## 数值环境变量格式错误检查（已纳入 v0.7.5-preview.1）

- `SCAN_INTERVAL`、`MAX_PARALLEL`、`MAX_RETRIES`、`RETRY_BACKOFF_SECONDS` 显式设为非数字时，原先静默采用默认值，导致启动范围校验无法发现配置错误。现在仅在变量未设置/为空时采用默认值；显式无效值会进入校验失败路径并阻止服务启动。
- 新增配置测试，覆盖四个变量的无效格式与未设置默认值；先确认新用例在旧行为下失败，再修改解析逻辑。`go test ./internal/config`、`go test ./...`、`go vet ./...` 通过。
- 实现与独立包验证见上方 v0.7.5-preview.1；未连接百度账号或执行云端操作。

## 百度 API 响应大小上限校验（已纳入 v0.7.5-preview.1）

- 目录列表（16 MiB）与下载链接（1 MiB）原先只读取上限内字节，却未识别响应实际超限；若有效 JSON 后跟随足够多的空白，截断内容仍可解析并被当作完整成功结果。现最多读取上限加 1 字节，超过即失败，避免把超限响应当作完整列表/链接。
- 两项本机 HTTP 回归先在旧行为下失败，修复后通过；另验证超限 503 仍保留可重试分类。`go test ./...`、`go vet ./...`、`git diff --check` 通过。超限目录结果失败时，不会被当作空/完整目录交给后续处理。
- 实现与独立包验证见上方 v0.7.5-preview.1；未连接百度账号或执行云端操作。

## v0.7.3-preview.1（Linux 目录项持久性测试包已交付）

- 源码 revision：`3426b016baf5acf4bc89a2d27713b87707984a6a`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.3-preview.1.zip`；SHA256：`378105077648a0be42c5523ec9290305fb7d79596d55dcca2461390649bc9a04`。
- Node WebUI/userscript 14 项、Go 全模块测试/vet、Windows UCRT 与 WSL 全模块 race/vet 通过；Windows AMD64/Linux AMD64/ARM64 构建通过，包内 11 个 payload 哈希与清单一致，ARM64 ELF 为 AArch64。
- Linux 对状态 rename、关键目录创建和最终文件硬链接同步父目录；失败会阻断后续源操作。回归覆盖首次发布与重试时目录同步失败，确认不发送网盘源请求。Windows 不宣称目录项掉电持久性。
- Windows Start.ps1 与 WSL Linux 原生 start.sh 均用正式包副本实际启动；认证健康/页面 API、匿名 401、版本与 revision 验证通过，服务停止后端口释放。全程无真实百度凭证或云端写操作。
- 真实 NAS/设备缓存、物理掉电、ARM64 实机、多日与真实百度验签仍未验收；未知结果目录继续隔离且无人工放行。验证详见发布目录 `v0.7.3-preview.1-validation.md`；未 push/deploy，主工作区保持干净。

## 源码 Docker 镜像版本元数据（源码检查点，待后续独立包）

- 源码 revision：`7357d7de61641136d79d27048ae382323be12476`。源码 Dockerfile 支持 `BUILD_VERSION`/`BUILD_REVISION` linker 参数，Compose 与 `.env.example` 接线；README 给出从 Git 取得并传入版本及 revision 的构建方式，开发构建默认仍为 `dev/unknown`。
- `docker compose config` 验证显式参数可传给 build args；Go 本机 linker 输出验证通过。使用现有 WSL Docker 真实构建源码镜像，并在专用容器验证 `/api/health` 精确返回版本与完整 revision、匿名返回 401；验后移除本轮专用容器与镜像。
- 独立发布包使用 `release/Dockerfile` 启动已注入版本的预构建二进制，此变更针对源码仓库多阶段 Dockerfile，未改变当前 v0.7.3-preview.1 包或其文件；后续预览包与其他产品改进合并交付。真实 NAS 不在本地 Docker 验证范围内。

## 源码 Docker 直接端口映射修复（源码检查点）

- 源码 Dockerfile 原来没有覆盖应用默认 `SERVER_HOST=127.0.0.1`；直接 `docker run -p` 虽然映射端口，容器服务却只监听容器 loopback。先用原镜像实际复现：日志为 `127.0.0.1:8080`，宿主映射的健康请求失败。
- 容器内默认改为监听 `0.0.0.0:8080`；README 的直接运行示例只发布到 `127.0.0.1` 并设置管理员密码，移除没有被应用当作本地队列读取的 `/NAS_Download_Queue` 挂载。
- 修复后实际构建/运行源码镜像：认证 health=200、匿名 health=401，版本与完整 revision 匹配；日志确认容器监听 `0.0.0.0:8080`。Compose 配置解析也断言容器监听与宿主回环发布地址；测试容器/镜像已清理。没有配置百度凭证或访问真实账号。
- revision：`93b5f4805b1c0870250c514f0e7dce26eb084b03`。该源码 Dockerfile 与 README 修复尚未进入独立预览包；当前 v0.7.3-preview.1 不变，后续合并应用功能后再打新包。厂商 NAS 网络/防火墙验收仍未进行。

## 源码 Compose 与部署说明对齐

- revision：`2ae69e40cb0d96b8e3321571afd752577d743a7f`。源码 Compose 现读取 `HOST_PORT`、`QUEUE_DIR`、`SCAN_INTERVAL`，`.env.example` 不再列看似可配置但实际被 Compose 忽略的本地目录/服务端口/监听地址；README 更新网页设备授权、Compose 构建和 keep/delete 说明。
- Compose 使用 `.env.example` 展开验证默认远端队列、30 秒扫描、宿主 `127.0.0.1:8080` 通过；注入自定义队列、77 秒间隔、19190 端口及 build metadata 的展开断言也通过。`git diff --check` 通过。
- 移除了旧 Kubernetes `LoadBalancer` 样板：该示例会公开管理服务，并把远端队列路径误作为容器本地卷。README 现明确 Kubernetes manifest、卷权限和集群网络未经维护/验证。未改变当前 v0.7.3-preview.1 包。

## v0.7.2-preview.1（下载 Token 查询参数修复测试包已交付）

- 源码 revision：`855ab0a8b4cf427e4c417f314db69e5f5b3de59a`；包 `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.7.2-preview.1.zip`，10,000,484 字节；SHA256 `e3dd0e74c6e8af62d0f9e03ae2a20c93eb69cff8dbf30d9a4762234a3a19e9a3`。
- 干净提交执行打包门禁：Node WebUI/userscript 14 项、Go 全模块测试/vet、Windows AMD64/Linux AMD64/ARM64 构建通过；包内 11 项哈希再次核对一致。Windows 与 Linux AMD64 --version/revision 匹配；ARM64 ELF machine=AArch64。
- 包内 Windows Start.ps1 实际启动：认证 health、首页、intake、status=200，匿名 health=401，源码版本/revision 精确匹配；固定假密码、无百度凭证、百度 HTTPS 指向拒绝连接代理。Ctrl+C 后 Service stopped，端口确认释放。
- 从包副本运行 Linux AMD64 原生 start.sh：认证 health=200 且版本/revision 匹配；匿名 health=401；SIGTERM 后 Service stopped、进程退出 0。测试仅在隔离 WSL home 目录进行，状态 owner/mode 不覆盖发布目录。
- 新 Token 链接查询参数的七项实际 HTTP 回归及 Windows UCRT/WSL Go1.22.2 全模块 race/vet 在打包前通过；进程强制终止后未知操作保持隔离的回归也随源码包含。
- 包内代码尚未对真实百度签名链接验签；无百度真实下载、厂商 NAS/ACL、ARM64 实机、多日或断电验收。未知目录继续隔离，没提供人工放行。验证明细在发布根目录 `v0.7.2-preview.1-validation.md`；未推送或部署。

## 下载链接查询参数修复（源码检查点，待后续包）

- 本机真实 HTTP 回归先 RED 复现：裸拼接 Token 遇到查询分隔符/片段会改变或丢失凭据；strings.Contains 把相似参数/路径误判为已有 Token，编码参数名则被重复追加。不能据此声称当前百度链接实际使用了这些形式。
- 改为解析 URL 的真实 access_token 键，仅编码新追加值；保留链接中已有 Token（含显式空值）与已有 RawQuery 原文，不重新编码签名参数。解析错误沿用脱敏与 timer/context 清理；Range、认证拒绝与重试预算不变。
- 七项实际 HTTP 用例 GREEN：特殊字符、fragment、相似参数、路径同名文本、已有 Token、编码参数名、显式空值，同时检查既有 %2f/%20 签名原文保留。Windows UCRT、WSL Go1.22.2 全模块非缓存 race/vet 均通过，diff 检查通过。
- 独立只读审查无确定 important/critical，复跑新用例及既有 URL 脱敏、取消、Range 和 idle timeout 通过。本轮未访问真实账号，尚未验证百度服务端签名验签；后续独立包纳入。当前 v0.7.1-preview.1 不含此修复，旧包不改动，未知目录仍隔离，无人工放行入口。

## 源操作请求中进程强制终止验证（测试检查点）

- 新增 crash_source_test.go，补足此前正常 Shutdown/合成记录重开之外的真实 Process.Kill 窗口。keep/delete 分别启动子进程，真实本机 HTTP 服务收到 rename/delete 请求后保持响应未完成；父进程核对 sending/source_unknown 已落盘且最终文件内容完整，再强制结束子进程，不执行优雅退出。
- 重开真实 Downloader 成功取得写锁；fresh scan 后仍 source_unknown，保留原模式，CanCancel/CanRetry=false，Cancel/Retry 返回冲突；直到重新打开的实例关闭，源操作请求计数始终为 1。父子进程所有 provider 请求均改写到本机夹具，仅使用假凭证。
- Windows 定向 race 及连续 5 次 race、WSL Go1.22.2 定向 race、Windows 全模块非缓存 test/vet、diff 检查通过；独立只读审查无确定 important/critical，并独立复跑定向测试通过。
- 本轮仅新增测试，没有发现需修改产品代码的缺陷，不重打相同行为包。当前可测版本仍为 v0.7.1-preview.1，ZIP SHA256 未变。证据只证明进程被强制终止后的恢复，不证明断电、文件系统丢写或真实百度服务端操作结果；不增加人工放行入口。原 main 保持干净，未推送/部署。

## v0.7.1-preview.1（普通用户容器运行测试包已交付）

将可选 RUN_AS、普通用户目录准备/迁移说明纳入独立包。程序行为沿用已验证的 v0.7.0，保留 HTTPS 来源、认证与未知隔离规则；旧包不改动。

- 源码 revision：`b5ed21a7050896e57c5647c5339fbfc2f7cbb48b`；发布根目录 `v0.7.1-preview.1.zip`，10,000,002 字节；SHA256 `8ceb6f5047fa597e5a13fdd6c6bc5556e637fcfea7d038840b50e2aae483c798`。
- 干净提交打包，Node 14 项、Go 测试/vet、三平台构建通过；最终 ZIP 解压 11 项哈希、ARM64 ELF machine=183、Windows --version/revision 核对通过。本轮未修改 Go 程序，未重复全部 race 测试。
- 实际包 Dockerfile 构建 AMD64 镜像，并用包内 Compose 加专用镜像/network none 测试 overlay 运行，未通过命令行覆盖 --user。RUN_AS=1000:1000、exec id、inspect、认证 health 证明身份/版本正确；无宿主端口映射，临时 ext4 状态目录 0700、manifest/锁 0600，owner=1000:1000。
- 同一目录第二容器退出 1 且报告 already in use；正常 stop/start 后 health 恢复，两次 stop 均 exit 0。两个专用容器和专用镜像标签已精确清理，保留日志；没有 prune 或操作其他容器。
- Windows verification 副本实际 Start.ps1 启动，health、首页、intake/status HTTP 200，匿名 401；无百度凭证，HTTPS 指向拒绝连接代理。Ctrl+C 后 Service stopped（外层 PTY exit 1），测试服务已停止。初次复制活动 verification 目录遇到 writer.lock 锁定，改用未运行的正式包副本构建，未带入活动状态。
- 发布包保持不可变，验证明细在发布根目录 v0.7.1-preview.1-validation.md。真实百度/NAS ACL、ARM64 实机、多日与断电尚未验收；未知目录继续隔离，不提供人工放行。持续优化目标仍进行中，未推送或部署。

## 源码镜像实际构建与部署配置修复（源码检查点）

- 复现源码 Compose 漏掉 BAIDU_REFRESH_TOKEN/ADMIN_PASSWORD：配置中设置假值却未转发。现补为可选变量，实际 Compose config 精确值通过；空值仍保留原行为。
- 新增根 .dockerignore，排除已知本地 .env/Git/下载/私有状态和输出。独立 git archive 加假标记，实际 scratch COPY/export 先证明标记可见；补规则后 .env/.git/downloads 被排除，根 .env.example、go.mod 和嵌入 UI 保留。没有发送工作区真实配置或数据。
- 使用原源码 Dockerfile 和原基础镜像地址实际构建成功，未拿替代镜像冒充。源码 Compose 以普通 UID 1000:1000、network none、临时 ext4 挂载运行，指定管理员密码有效，health has_refresh_token=true（仅假 Token），/app 与程序 0755、manifest owner/mode 正确；停止 exit 0。
- 源码镜像按原构建方式仍显示 dev/unknown；本轮没有改变 Go 程序、版本注入或基础镜像，也未打新包。证据在发布目录 source-build-20261023，厂商 NAS、真实百度、ARM64、多日和断电仍待验收。
- 独立只读审查未发现确定 important/critical，前后导出/配置/容器元数据独立核对通过，diff 检查通过。专用容器确认 exited 后精确清理，专用镜像标签已删除，保留测试日志与假标记副本；未执行 prune 或操作其他容器。

## NAS 普通用户部署与实际容器验证（源码检查点，待后续包）

- 源码镜像 runtime WORKDIR 从 /root 移至 /app，CMD 使用绝对路径；两份 Compose 增加可选 RUN_AS=UID:GID，默认 0:0 保留现有行为，env 示例与迁移说明已补充。不自动修改已有下载目录 owner/ACL。
- 本轮现场发现 WSL 内 Docker client/server 29.7.2、Compose 5.5.0 可用（此前 Windows CLI 不可用的记录不代表 WSL）。从 v0.7.0 正式包副本使用原发布 Dockerfile 实际构建 AMD64 镜像，本地已有 Alpine 3.22；未安装宿主 Docker，apk 证书包仅在专用镜像构建中安装。
- 专用测试容器 network none、无百度凭证，以 1000:1000 在临时 ext4 bind mount 启动，管理 health 正确；状态目录 0700、manifest/锁 0600，owner=1000:1000；第二实例 exit 1，停止/重启后健康恢复、正常停止 exit 0。
- 只读挂载及普通 0500 目录分别报 read-only/permission denied、exit 1，目录未生成状态。实际 Compose run 未传 --user，inspect/exec id/health 证明 RUN_AS 被应用，使用专用测试 overlay 固定镜像与 network none，无发布端口；默认及非默认身份的两份 Compose config 均解析通过。
- 证据位于发布目录 nas-permission-20261023；源码多阶段镜像未构建，不能扩大发布镜像证据。厂商 NAS ACL、SMB/FUSE、ARM64、真实百度及多日仍未验收。Go 程序与 v0.7.0 ZIP 未变，新的 RUN_AS 行将进入后续包。
- 独立只读审查未发现确定重要问题，两份 Compose 空/指定身份均独立解析通过，差异检查通过。五个专用测试容器确认 exited 后精确清理，专用镜像标签已删除；未执行 prune 或操作其他容器，验证输出保留。

## v0.7.0-preview.1（HTTPS 终止代理支持测试包已交付）

- 源码 revision：`eaf332c9cfe2ed77f79da6d3b02bbed499a72b81`；ZIP：发布根目录 v0.7.0-preview.1.zip，9,999,005 字节；SHA256 `19173bbc81dcfffd25d5e5950ee41bc05cca9d33c7c959969e3a2f78af3226c4`。
- 干净提交 Node 14 项、Go 测试/vet、三平台构建通过；最终 ZIP 解压 11 项哈希、ARM64 ELF machine=183 和 Windows --version/revision 核对通过。
- 仅在 verification 副本运行 Windows Start.ps1 -PublicOrigin 与 WSL Linux AMD64 start.sh+PUBLIC_ORIGIN。临时本机 TLS 代理逐一转发到实际包内进程，刻意改写后端 Host/伪造 Forwarded 头；认证 health/POST 200、WSS 状态帧、恶意 Origin HTTP/WSS 403、匿名 401 均通过。证书只在测试客户端信任，未安装到系统。
- 两平台预置合成 sending/source_unknown 记录：隔离状态与精确大整数 ID 保留，can_cancel/can_retry=false，TLS 入口操作 409。配置固定 HTTPS 后，直接 HTTP 浏览器 Origin 403；无 Origin 的认证 CLI 仍可读状态。没有百度凭证，HTTPS 指向拒绝连接代理。
- TLS 探针已退出；Windows Ctrl+C 后 Service stopped（外层 PTY exit 1），Linux 精确核对进程后 SIGTERM、Service stopped、exit 0，测试服务均已停止。正式包无验证状态，旧包不变；详情见发布目录 v0.7.0-preview.1-validation.md。

- 新增可选 PUBLIC_ORIGIN 固定外部 HTTPS 来源。原行为保持为按实际 TLS/Host 校验；显式配置由 Gate 验证后放入私有 request context，外层 API 与 WebSocket 共享 CheckOrigin。拒绝 cross-site，认证仍必须，不读取 Forwarded/X-Forwarded-* 作为授权依据。
- Config/main、原生 PS/sh、根/发布 Compose 和 env 示例接线。配置拒绝 HTTP、用户密码、路径前缀、query/fragment、非法/空端口；规范化默认 443、前导零端口，支持标准 IPv6 方括号。
- 先 RED：HTTPS Origin 经 HTTP 后端被 403 拒绝。修复后真实本机 TLS 代理 → HTTP applicationHandler 的管理 POST 与 WSS 状态帧通过，代理刻意重写后端 Host；匿名 401、恶意 Origin 的 POST/WSS 403。TLS 客户端仅信任夹具证书，没有跳过校验。
- 独立审查发现 :443 与浏览器 Origin 省略端口不一致，先 RED 后修，四项端口/IPv6 回归通过；复核无新确定重要问题。Windows UCRT/WSL 全模块 race/vet、Node 14 项、PS/sh 语法与 diff 检查通过；端口补项后两平台 main/management/config race 与全模块 vet 再通过。
- 设计与步骤见 [reverse-proxy-plan.md](reverse-proxy-plan.md)，使用见 release/TESTING.md；新配置已纳入 v0.7.0-preview.1。未安装代理软件，未操作真实 NAS 或账号；本机协议验证不替代浏览器原生 Basic 框、厂商代理/证书部署验收。

## v0.6.3-preview.1（自动扫描与迟到认证拒绝恢复测试包已交付）

- 源码 revision：`5f0da58ba09c940e9933caf3644d6c031f1bc61c`；ZIP：发布根目录 v0.6.3-preview.1.zip，9,992,945 字节；SHA256 `b68fb9c02f7a00331892bae3c379209fa73039af831a9db0221db69ad64dd96b`。
- 干净提交 Node 14 项、Go 测试/vet 和三平台构建通过；最终 ZIP 解压 11 项哈希及 ARM64 ELF machine=183 检查通过。Windows/Linux AMD64 包内脚本运行，版本/revision/等待授权正确，认证页面与状态可读，匿名 401。
- 两平台验证副本预置合成 sending/source_unknown、大整数 ID 记录；恢复后仍显示 source_unknown=1，任务 ID 精确，can_retry/can_cancel=false，操作均 409。无真实百度凭证，HTTPS 指向拒绝连接代理。正式包与旧版本不含本轮验证状态。
- Windows Ctrl+C 后 Service stopped（外层 PTY exit 1）；Linux 精确核对进程后 SIGTERM、Service stopped、exit 0，服务均已停止。明细见发布目录 v0.6.3-preview.1-validation.md；原 main 工作区干净，无推送或部署。

- 主程序实际 applyCredentialUpdate 回调、runScanner、后台 Manager.Run 与下载器集成发现时序问题：轮换成功并完成新扫描后，旧下载才返回 401，complete 又把任务设为 auth_wait；续期扫描通知已被消费，需要等下次定时扫描。
- 先 RED 后修复：只在拒绝带非空旧凭证指纹、当前 managed 新凭证有效、该根任务拥有完全匹配的新扫描资格时恢复 pending；否则保持 auth_wait，不消耗失败预算。同一凭证再次拒绝不会形成无预算重试。
- 主程序原回调提取为共享 applyCredentialUpdate，main 与测试使用相同函数，逻辑不变。扫描周期设为 3600 秒，两种认证时序均无需手动扫描即可在测试限时内完成，新 Token 下载精确 abcdef、源操作一次；Shutdown 后管理器 closed，同目录可重新打开证明锁释放。
- 七项准入用例覆盖新扫描匹配、无扫描、版本变化、已过期、相同 Token、非托管与缺失指纹；后六项仍等待。既有 unknown/cancel 处理优先级不变。
- 独立审查指出扫描时间戳可能误认旧扫描，已改为在新 Token 的 list 请求处设可取消 barrier，等其实际结果发布再返回旧 401；移除修复重新 RED、恢复后 GREEN。两平台全模块 race/vet、Node 14 项通过；最终测试同步与退出补项后两平台 main/downloader race/vet 再通过。
- 集成测试不含进程 main() 的监听/信号路径或真实百度 OAuth；文件流为本机 HTTP，其他百度接口是替身。包内进程启动/停止证据见本节交付记录，不据此扩大为真实凭证续期验收。旧包保持不变。

## 凭证轮换与下载并发集成验证（未改产品、未重新打包）

- 新增实际 credentials.Manager、Downloader、私有磁盘存储与本机 HTTP 文件流的跨模块测试，使用与主程序一致的凭证更新/认证拒绝回调；测试显式控制扫描，没有运行主程序扫描循环。
- 当前旧下载返回 401，实际后台 Manager.Run 被拒绝通知唤醒，完成一次刷新；另覆盖先轮换成功、旧下载随后返回 401，迟到拒绝不撤销新凭证。两种场景均保留失败预算、等待新扫描，随后以新 Token 下载精确 abcdef，并仅以新 Token 后处理源项一次。
- 直接读取真实 credentials.json 对应存储，核对轮换后的 Access/Refresh、同账号绑定和 Generation=2；未输出凭证。Windows UCRT 与 WSL 全模块 race/vet、diff 检查通过。
- 独立只读审查未发现确定重要问题；按审查建议补上超过 worker 轮询间隔的观察窗口及扫描资格为空断言，两平台定向 race 再通过。该观察不替代主程序自动扫描接线验收。
- OAuth Refresh/Account 和百度文件管理是替身，文件流为真实本机 HTTP；不等同于真实百度续期、多日运行或跨重启凭证读回验收。现有 v0.6.2-preview.1 产品源码不变，继续使用该独立测试包。

## v0.6.2-preview.1（分块短流恢复测试包已交付）

- 源码 revision：`88807c8302134223195b9918e29dfa7f22f5b92f`；ZIP：发布根目录 v0.6.2-preview.1.zip，9,992,101 字节；SHA256 `323bbdb948d800fdbd98bf7c0c89205a5ab07af871a5f06913072c5dab9b1ddb`。
- 干净提交打包 Node/Go/vet 门禁、三平台构建、11 项解压哈希和 ARM64 ELF machine=183 检查通过。Windows/Linux AMD64 包内脚本实际启动，健康版本/revision/等待授权正确，认证主页、入队页、状态 API 200，匿名 401。
- 运行仅在 verification 副本，无百度凭证，HTTPS 指向拒绝连接代理。Windows Ctrl+C 后 Service stopped（外层 PTY exit 1）；Linux 精确核对进程后 SIGTERM、Service stopped、exit 0。验证服务已停止，正式包无运行数据；详情见发布目录 v0.6.2-preview.1-validation.md。

- 实际 HTTP 分块响应没有 Content-Length，正常 EOF 但不足网盘元数据大小时，旧实现只报普通长度错误，持久化队列直接 failed，不进入后台重试。已先 RED 复现。
- 现该短流错误保留 io.ErrUnexpectedEOF 错误链，复用既有有界退避和预算；不生成完成凭据、不发布、不执行云端后处理。超长响应、错误范围、声明长度不匹配的拒绝逻辑不变。
- 新增真实本机 chunked HTTP/临时磁盘/持久化调度器测试：保留 3 字节后以 Range bytes=3- 恢复为 abcdef，只后处理一次；持续短流耗尽一次重试预算后 failed，无最终文件、无源操作。Windows UCRT 与 WSL 全模块 race/vet、Node 14 项通过。
- 独立只读审查及非缓存短流/Range/源操作定向测试通过，未发现新增确定重要问题；错误范围、超长数据、取消及磁盘写入错误不会被该分支覆盖。git diff --check 通过。
- 补充实际关闭/重开持久化下载器验证：retry_wait、启动次数和重试截止时间保留，直接读回临时内容 abc；截止时间过去也不发起文件流请求；新扫描后 Range bytes=3- 续传，持续短流按已保存预算耗尽，无最终文件/源操作。两平台全模块 race/vet 通过；最后补充内容读回断言后两平台定向 race 再通过。独立审查未发现确定重要问题。
- 没有访问真实百度账号或部署；v0.6.1 原包保持不变。

## v0.6.1-preview.1（校验取消修复测试包已交付）

- 源码 revision：`d488e25bbe2e66fcad3924c4a8b7d4a9bde3b703`；ZIP：发布根目录 v0.6.1-preview.1.zip，9,991,746 字节；SHA256 `55559ad63316664253e0c2a1756a1090efdeb6b8438aa489de7c3768024d177e`。
- 干净提交打包门禁、三平台构建、11 项解压哈希和 ARM64 ELF machine=183 检查通过。仅在 verification 副本运行，Windows/Linux AMD64 启动、版本/健康、认证页面/API 200、匿名 401 通过；无百度凭证、HTTPS 指向拒绝连接代理。
- Windows Ctrl+C 后 Service stopped（外层 PTY exit 1），Linux 精确核对进程路径后 SIGTERM、Service stopped、exit 0。服务均停止，原 main 工作区干净，无推送/部署；详细记录在发布目录 v0.6.1-preview.1-validation.md。

- 三处 SHA256 校验接入任务 context，分块读取前后检查取消；包括最后一次读取同时返回 EOF 的情况，取消后不生成有效摘要。
- 已复现完整临时文件恢复校验被取消后误走重置分支、将文件截断为 0 的问题；现收到取消直接退出，保留绑定的临时文件。正常摘要和非取消恢复规则不变。
- 先 RED 后 GREEN：读取取消、最后读取取消、打开文件前取消，以及真实 64 MiB 临时文件保留均有回归测试。Windows UCRT、WSL 全模块 race/vet、Node 14 项和独立只读审查通过。
- 取消检查在读取块之间生效，无法打断已阻塞在操作系统内的磁盘 Read。未访问真实百度账号，unknown 目录仍隔离，无人工放行。

## v0.6 实际浏览器补充验证（未改产品、未重新打包）

- 从 verification 副本运行实际 v0.6 包，使用隔离合成任务、无百度凭证。内置浏览器直连无法操作 Basic 原生框，保留该未验收项。
- 经临时本机 HTTP 转发入口，在只读资源请求中完成实际 Basic 挑战、清除拦截后，真实任务页通过浏览器认证缓存建立 WebSocket；状态帧显示 source_unknown 与禁止重发，实际截图保存在发布目录 v0.6.0-browser-reconnect.jpg。
- 服务停止后显示断线/3 秒重连，服务重开后无需刷新自动恢复并保留隔离任务。轮询页、实际扫描提交、fragment 只导入和未授权手动预检拒绝也完成实际页面验证。
- 详细条件、证据和边界见 [browser-validation.md](browser-validation.md)。不把受控 HTTP 入口证据扩大为原生登录框、TLS 反代或真实百度/NAS 已验收；所有测试服务已停止，原 ZIP 不变。

## v0.6.0-preview.1（源后处理完成与重放保护测试包已交付）

- 源码 revision：`62586afd2809e1b54d5072a95eed783224830dad`；ZIP：发布根目录 v0.6.0-preview.1.zip，9,991,187 字节，SHA256 `b2c5fdc8d4b212f86c02b2fc6233fcd6cab0dc417b801cf4fcc5ea275a8b5a88`。
- 干净提交 Node/Go/vet 门禁、三平台构建、11 项解压哈希和 ARM64 架构检查通过。Windows/Linux AMD64 包内程序从验证副本实际运行，合成 sending 恢复为 source_unknown；状态/健康正确，取消与重试 409，匿名 401。没有配置百度凭证，HTTPS 指向拒绝连接代理。
- Windows Ctrl+C 日志停止（外层 PTY exit 1）；Linux 精确核对进程后 SIGTERM、Service stopped、exit 0。所有验证服务已停止，正式包无测试状态。完整证据在发布目录 v0.6.0-preview.1-validation.md。

- Rename/Delete 改为严格同步单项完成解析：顶层成功不足以证明完成，异步 taskid/业务 111、缺字段、多项、截断/读取失败、网络/5xx/重定向保持未知；禁止 POST 跟随重定向，原 307 反例重放 10 次已修。
- 根任务发送前原子保存 source_state=sending/source_unknown；确认成功先保存 confirmed/done。回复或凭据保存丢失、崩溃重开保持 unknown，不自动/手动重发；发送前保存失败无请求。
- 隔离覆盖扫描/claim、Cancel/Retry、管理入队预检/登记及浏览资格。独立审查发现的同 ID 换路径覆写已先 RED 后修，关闭重开仍保留旧路径；同路径换 ID 也不可绕过。
- 根任务本地完成不再提前显示 done；网页状态与健康新增 source_unknown，显示禁止重发。旧无字段记录可读，旧二进制不能处理新不确定状态；升级前停旧实例并备份状态目录。
- 两平台全模块 race/vet 和 Node 14 项通过；最后补项后受影响 baiduapi/downloader/intakeapi/main 两平台 race/vet 再通过，独立最后复核无新确定重要问题。设计/证据见 [source-operation-plan.md](source-operation-plan.md)。
- v0.6.0-preview.1 已将下述业务错误恢复一并纳入；无真实账号操作，真实百度/NAS、同步响应兼容和异步完成查询仍未验收。

## 百度业务错误恢复源码检查点（已合入待发布 v0.6）

- 核对官方 SDK 后补 20016/20017/31045 的带指纹认证分类、31034 的现有有界重试；任务层验证 auth_wait 不耗预算、限流退避后恢复一次完成。
- 区分业务 errno 与 OAuth error_code 的 111：业务运行状态不再误撤销凭证；Move 无 taskid 但出现运行迹象也按异步未知隔离，文件/目录均不登记、不重发、不人工放行。
- 先 RED 后 GREEN，独立 review 发现的 Rename/Delete 单项命名空间遗漏已修并复核。两平台全模块 race/vet 通过；最后补项后两平台 baiduapi/downloader race、全模块 vet 再通过，Node 13 项和 diff 检查通过。
- 细节与后续：[provider-error-recovery.md](provider-error-recovery.md)。下一步专门核查源 Rename/Delete 的严格完成响应和 POST 重放边界；本轮分类通过不等于该契约完成。无真实账号操作，未发新包，当前独立测试版仍为 v0.5.0-preview.1。

## v0.5.0-preview.1（油猴选中项衔接已交付本地测试包）

- 源码 revision：`f5abf5500c6bdf2a7076113de09cff057fc0a70d`；ZIP 位于发布根目录 v0.5.0-preview.1.zip，9,982,087 字节，SHA256 `db5b7a164a41e60c4ea85964404e2d783f0d829024e2992c7cd8328f5ddeefaf`。
- 从干净提交打包 Node/Go/vet 门禁、三平台构建、解压 11 项哈希及 ARM64 架构检查均通过；油猴已纳入包及清单。
- Windows 与 Linux AMD64 包内启动、管理认证及新版嵌入 JS、未授权 cloud 409 验证通过。Windows Ctrl+C 日志停止（外层 PTY exit 1），Linux 精确核对进程后 SIGTERM、exit 0。验证目录与正式包隔离，所有服务已关闭；详见发布目录 v0.5.0-preview.1-validation.md。

- 原占位脚本已替换：从旧 context/已知 Vue selectedList 提取明确选中路径，多来源不一致/空选/无路径失败，不回退 FILEINFO；沙箱从 unsafeWindow.document 读取页面 Vue 对象。
- GM 仅存管理地址，默认 localhost；拒绝远端 HTTP、URL 凭证和附加参数。点击先展示管理地址/路径，再用 fragment 交给管理页；没有 API、Cookie、Token 或管理员密码桥接。
- 管理页在 API 请求前清除并解析 fragment，只填输入并提示核对账号，仍需手动预检/勾选/确认；无效载荷和无法清除时不导入。
- Windows/WSL Go 全模块测试和 vet、两平台 Node 13 项、独立只读 review、diff 检查通过。内置浏览器隔离夹具验证导入/URL 清除/账号提示/未自动预检；夹具服务已停止。
- 计划及兼容边界见 [userscript-plan.md](userscript-plan.md)。真实百度网页、真实油猴沙箱及所有 SPA 视图未验收。unknown 目录隔离规则不变；异步完成查询仍待可靠契约。
- 已交付新独立版本 v0.5.0-preview.1；已有 v0.4.0-preview.1 不变。继续推进真实适配证据与可靠异步完成查询，不据本轮夹具宣称整项目完成。

## v0.4.0-preview.1（管理页入队与任务操作已交付本地测试包）

- 源码 revision：`a232efd4b84e0d16fb6c30e58a59d19d0e15bb86`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.4.0-preview.1.zip`，9,976,240 字节。
- ZIP SHA256：`a0b5027ef2bafc7d1d953a9abda61f01b9739a265b85450dbce8cd016248a3ee`。三平台构建、解压 10 项哈希及 ARM64 架构检查通过。
- Windows 与 WSL Linux AMD64 均从 verification 副本运行包内启动脚本；无百度凭证且 HTTPS 指向拒绝连接代理。新入口匿名 401、认证后页面/脚本/记录/健康 200、未授权 cloud 409；Windows 另核对预检 409、跨站 403、release 404。
- Windows Ctrl+C 后 Service stopped（外层 PTY exit 1）；Linux 核对 /proc/exe 后 SIGTERM、Service stopped、exit 0。全部验证服务已关闭。完整证据在发布目录的 v0.4.0-preview.1-validation.md。

### 服务端入队与管理页已实现

- 设计：[intake-design.md](intake-design.md)。预检、确认、只读核对和独立 intake/ 原子日志；固定队列目标和任务模式，严格路径/身份/冲突，认证 epoch/进程绑定的短时预检。
- MoveFile 同步无覆盖、严格单项响应、禁 307/308 POST 重放。每项 sending 先落盘再发送，落盘后再查认证；未知结果隔离目标，重启不自动重发，同批逐项报告。
- 扫描和源后处理与移动按 cloud 读写锁协调；隔离目标不注册/认领/后处理，不影响无关项目。已登记根任务与完成凭据之间崩溃时保留 pending，不把刻意隔离误判成源丢失。
- 目录只有已持久化同步完成凭据时，才可按源/目标身份恢复；异步受理和无凭据未知目录持续隔离。丢失异步标记的崩溃窗口已有 RED/GREEN 回归，记录 async taskid 供后续处理。
- 本地真实 HTTP 验证 move 编码与请求参数；故障和磁盘测试覆盖部分失败、新冲突、认证撤销、发送前保存失败、回复丢失、重启隔离、目录凭据保存丢失及成功恢复。独立 reviewer 最后复核完成凭据边界通过。
- 最终 Windows UCRT / WSL 全模块 race、vet 通过；最后补的 Move 本地 HTTP wire 契约测试在两平台 baiduapi race 复核通过。前端原有 5 项执行通过，git diff --check 通过。
- 已接入受管理认证保护的 /intake、云端列表、预检、确认、记录和只读核对路由；页面展示固定目标/策略，确认必须勾选，提交前消费本地预检，异常不自动重发。
- 用户明确决定：缺少完成凭据的 unknown 目录继续隔离，暂不提供人工放行。已同步设计、计划和测试指南，API/UI 无人工覆盖入口。
- 实际本地 HTTP → 下载器 → 磁盘日志 → 重启读回验证通过，重复提交只移动一次，大整数 ID 保真；新路由匿名 401，Node 7 项、Windows/WSL 全模块 race/vet、独立审查通过。浏览器隔离夹具验证选择/预检/明确确认/unknown 提示；不能等同管理登录与真实百度验收。
- 异步任务可靠完成查询、油猴适配和真实百度/NAS 链路仍待完成；没有操作真实账号，不以本次交付代替 D 的全部验收。

- 计划：[experience-plan.md](experience-plan.md)。先补已有队列管理，入队/油猴仍待实施；原脚本经核查只是打印 FILEINFO，不能可靠认定选中项，没有移动操作。
- 两种任务页增加立即扫描和根任务取消/重试、按任务防重复提交、状态冲突反馈、取消未排空时等待可重试；显示根任务固定后处理策略、启动次数和下次重试时间。
- HTTP/WS 新增精确 task_id 字符串、can_cancel/can_retry。操作入口不从 JS Number 构造 ID，保留旧 fs_id 数字兼容查询；能力根据服务端认证、扫描资格和 active worker 计算，Retry 再次检查认证。
- 大于 2^53 的任务标识实际 HTTP/WS 往返和精确路径重试命中（409 而非 404）通过；取消未排空/源扫描资格/认证过期能力与前端五项脚本测试通过。
- Windows UCRT、WSL 全模块 race 和 vet 通过；独立 reviewer 未发现确定 important/critical，git diff --check 通过。GetTasks 的队列/任务/客户端锁顺序已复核。
- 独立包 v0.4.0-preview.1 已构建并验证，旧版 v0.3.0-preview.1 保持不变。
- 下一步继续油猴的可靠选中项适配和安全衔接，异步状态契约需独立证据；不把本次管理页交付作为 D 全阶段完成。

## v0.3.0-preview.1（阶段 C 已交付本地测试包）

- 源码 revision：`fec54acc83a0015e1ddcba5b398b4543c2b189a6`；ZIP：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.3.0-preview.1.zip`（9,828,655 字节）。
- ZIP SHA256：`6171ce7a98f7e30e67e2031913d52e8567cc1c215c7dcf4fcc9679d4cc93eb86`。三平台构建、解压 10 项哈希、ARM64 架构检查通过。
- Windows 包内启动、匿名 401、认证页面/API、Origin/WS 403、真实 WebSocket 设置持久化、第二实例拒绝、重启密码文件不变和锁释放验证通过；两次 Ctrl+C 均有 Service stopped，外层 PowerShell/PTY exit 1 不当作原生 exit 0。
- Linux AMD64 包内 start.sh 实际运行、Basic 健康/页面和 Bearer WebSocket、匿名 401 验证通过；SIGTERM 后 Service stopped，进程 exit 0。包验证只在 verification 目录，不改最终 ZIP/供用户使用的解压目录；未提供真实百度凭证且强制拒绝连接的回环 HTTPS 代理。
- 验证明细位于 releases 同级 `v0.3.0-preview.1-validation.md`。真实百度、目标浏览器原生 Basic+WS、NAS/Docker/ARM64、多日/断电和反代未验收。服务已停止，未推送或部署，原 main 工作区保持不变。

- 集成网页设备授权、凭证保存/应用/续期、管理 Basic/Bearer 和 Origin/WS 校验；无 Token 也可启动管理入口，下载先等待授权。随机管理员密钥放在私有 admin-key.json，日志不输出密码。
- 下载器按认证状态和有效期限制扫描、新任务及源操作；auth_wait 持久化且不消耗下载失败预算，恢复后必须新扫描。Token/有效期变化递增 epoch，旧扫描结果不能解锁执行；旧请求认证拒绝带凭证指纹。
- Manager/Scanner 与 HTTP 请求均纳入取消生命周期，后台退出后才关闭共用存储。健康结果区分管理连接、账号可用、扫描结果；主界面按实际认证状态显示。
- 本地真实 HTTP/WS 测试验证整站匿名 401、Bearer/Basic、Origin 403、WebSocket 状态；本地主程序 HTTP 授权测试经过实际 Manager/Downloader/文件存储，成功应用但不回传 Token。设备 session 的 interval/TTL 由服务端持有，新会话取消旧轮询。
- Windows UCRT / WSL 全模块 race、vet 通过；最后新增 main/OAuth 测试两平台定向 race 通过；Node 前端脚本执行 3 项通过；PS/sh 语法和 git diff --check 通过。独立 reviewer 最终未发现新的确定 important/critical。尚未将这些本地验证当作真实百度/真实浏览器验收。
- 更新原生启动、Compose 环境与测试指南；交付目录、源码 revision、哈希和包验证见本节开头。

## 阶段 C：认证核心源码检查点（历史，3c4dfc8）

- 设计与计划：[auth-design.md](auth-design.md)、[auth-plan.md](auth-plan.md)。OAuth 增加 Context、请求/响应上限、拒绝重定向、稳定且不含 Token/原始响应的错误；保留旧函数签名兼容现有调用。
- 新增 credentials 管理器和独立 credentials.json / account.json。手动授权先校验账号再保存；自动刷新先保存轮换结果再校验；启动优先恢复已保存凭证，避免环境旧 Token 覆盖。只持久化应用标识和 Token，不保存 App Secret；对外 Status 不含凭证。
- 串行应用与续期、提前刷新、暂时失败退避、账号绑定、迟到旧 Token 拒绝隔离。失败只能保留现有 Ready，只有成功验证并保存后的 publish 才能重新授予 Ready。
- RED/GREEN 回归覆盖：取消执行中的刷新不永久阻断续期；BindAccount 暂时失败保留候选；退避期间到期及时撤销；手动失败唤醒后台并真正重试；退避中的真实认证拒绝立即撤销；暂时刷新失败不能复活已被拒绝的凭证；轮换后账号查询取消仍可恢复。
- 独立 reviewer 发现的恢复与调度问题均修复并定向复核，最后完整 credentials 非缓存测试通过；主线程最终 Windows/WSL 全模块 race 与 vet 通过。没有访问真实百度账号。
- 当前仅是核心模块和下载器存储桥接，尚未接入 main 的网页授权/后台生命周期，尚无管理认证、Origin/WS 校验、下载 auth_wait 门禁和新健康状态；不能据此认定网页登录/自动续期已经在运行中的程序生效。
- 没有为这个中间源码检查点重复打包。当前可测试版本仍为下面的 v0.2.0-preview.1；完成 C 集成、HTTP/WS 冒烟与包验证后再交付下一独立版本。原 main 工作区保持不变，没有推送或部署。

## v0.2.0-preview.1（阶段 B 已交付本地测试包）

- 源码 revision：`4a1b7e398f87d7a6855beaeaefe606350b4346d7`；包：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.2.0-preview.1.zip`。
- ZIP SHA256：`40386eae3747042106fbea49016efe0082e8a8b705703eee705be9971d6ad138`。三平台静态构建通过，最终解压 10 个文件哈希通过，ARM64 ELF machine 核对通过。
- 最终 Windows UCRT/WSL Go1.22.2 全模块 race、go vet、git diff --check 通过，PowerShell 与 sh 启动脚本语法通过。
- Windows 包内 Start.ps1、版本/并发配置、页面/资源/状态/扫描/未知任务 404 路由通过。相同目录第二实例退出 1；WebSocket 保存模式后 Ctrl+C 日志 Service stopped；重启同一目录并再次读取模式验证持久化与锁释放。
- Linux AMD64 包内 start.sh 实际启动，通过本机健康检查，SIGTERM 后 Service stopped 且进程 exit 0。
- 打包后运行均仅用假 Token，百度 HTTPS 强制走拒绝连接的回环代理。验证目录的模式切换只影响测试目录，不在最终 ZIP 或供用户使用的解压目录内。
- Docker/ARM64 实机、真实百度账号/NAS、多日运行、突然断电未验收；没有推送或部署，原 main 工作区未修改。

- 目标：持久化根任务与默认模式、有界 worker、取消/重试、故障退避和服务优雅退出。设计与实施记录：[queue-design.md](queue-design.md)、[queue-plan.md](queue-plan.md)。
- 存储采用逐任务原子 JSON，不增加 SQLite 依赖；避免每次变化重写整份历史。OS 文件锁保护新版同一下载目录单写入，进程退出自动释放。损坏记录/未知版本/队列绑定变化拒绝启动。
- 根任务固定入队时的后处理模式；默认模式单独持久化。重启中的任务等待扫描确认后恢复，根任务离开队列时转为需人工核对的 failed；不依据旧路径执行源操作。
- MAX_PARALLEL 默认 2，MAX_RETRIES 默认 3，退避从 5 秒开始、上限 5 分钟。失败预算与启动次数分离，重启/取消不会耗尽失败重试预算。
- 全链路 context 取消；元数据请求总超时 30 秒；流下载空闲 90 秒超时，持续传输可以超过空闲时限。保留 HTTP 429/5xx、断流、网络超时错误链，其他错误不盲目自动重试。
- 增加根任务取消与手动重试 API，状态输出 is_root / attempts / next_attempt / download_mode。扫描请求合并，不为每次点击创建 goroutine；SIGINT/SIGTERM 停止 worker、扫描和 WebSocket。
- 故障测试：进程锁排他与崩溃释放、重启扫描门禁、队列/设置往返、并发上限、固定策略、取消源操作保护、退避预算、完整错误链、3000 子项失败后重开、状态锁阻塞时 Shutdown deadline。
- 独立 reviewer 两项 important（非 JSON 503/读取错误误分类、超大错误记录导致重启失败）已修并有回归测试；最新复核无新增 important/critical。补充了持续有数据的下载总耗时超过 idle limit 的测试。
- 包验证明细见 releases 同级的 `v0.2.0-preview.1-validation.md`；根队列持久化与网络故障链路是本地真实 HTTP/磁盘测试，不等同于百度 E2E。
- 下一阶段继续 C：OAuth 应用/保存/续期、管理认证、健康状态；随后 D：入队与任务管理体验。整体目标仍进行中。

## v0.1.0-preview.3（已交付历史版本）

- 最终源码 revision：`5a270f1b2050e1fa8483af3960c16a17951eea0e`。
- 当前交付：`D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.1.0-preview.3.zip`。
- 当前 ZIP SHA256：`8be839e5664818a7063db68cd6b1811af61db6c0d9129e49f1a3a2d632146440`。
- 最终代码 `go test -race -count=1 ./...`（Windows UCRT）、`go test ./...`、`go vet ./...` 通过。包内 10 个文件哈希通过；实际 Start.ps1、Windows 本地 HTTP 页面/API/资源/手动扫描通过并已停止；Linux AMD64 包内版本命令运行通过，ARM64 ELF machine 检查通过。Docker/ARM64 实机与真实网盘/NAS 尚未验收。
- 下面 preview.1/preview.2 记录为内部候选历史；使用此节的 preview.3 包和哈希。

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

## 下一阶段

继续 D：任务取消/重试等管理界面和入队体验。先核查原油猴脚本实际能力、选中文件数据来源与新管理认证/Origin 边界，再确定可验证的入队流程。A/B/C 的本地实现和预览包不是全目标或真实网盘无人值守验收完成。

## 未验收

真实百度账号下载、真实 NAS/容器运行、突然断电、多日稳定性、非本地文件系统挂载。未推送、未部署、未修改真实网盘。
