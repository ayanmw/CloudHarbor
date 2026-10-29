# NAS 普通用户运行 Implementation Plan

沿用用户持续优化授权，使用 writing-plans，inline 执行；不安装 Docker、不部署或修改真实 NAS。

**Goal:** 提供可选 UID:GID 运行方式，避免源码镜像把程序置于仅 root 可访问的路径，并用真实 Linux 普通用户权限验证启动。

**Architecture:** 源码构建镜像的 runtime WORKDIR/CMD 使用 /app；两个 Compose 文件增加可选 RUN_AS，默认 0:0 保留现有行为。应用不新增身份切换、chmod/chown 或自动提升权限逻辑，下载目录由部署者预先准备。

**Tech Stack:** Dockerfile、Compose 标准 user 属性、现有 Go 程序与 Linux 文件权限。

## Tasks

- [x] 源码 Dockerfile runtime 使用 /app 和绝对 CMD，两个 Compose 增加 user: ${RUN_AS:-0:0}；env 示例说明空值保持兼容。
- [x] 测试指南说明 UID:GID、bind mount owner、旧私有状态迁移边界，不自动 chown/chmod 已有目录。
- [x] 现场发现 WSL Docker daemon 可用，改用真实 Docker 构建发布包镜像，在临时 Linux ext4 目录以普通 UID 验证管理启动、状态 owner/mode、重启、第二实例排他、只读挂载与 mode 0500 不可写目录失败。无 Go 代码变化。
- [x] 独立只读审查与 diff/配置检查通过，记录 Docker/Compose 证据及厂商 NAS 未验证边界；后续测试包纳入。

## 依据与限制

[Compose user](https://docs.docker.com/reference/compose-file/services/#user) 覆盖容器进程用户；[Dockerfile WORKDIR](https://docs.docker.com/reference/dockerfile/#workdir) 决定运行命令的工作目录。显式可选身份避免假定 NAS 的 UID 恰好为 1000，不改变现有目录所有权。

前一检查点使用 WSL Ubuntu、AMD64、临时 ext4 bind mount 验证发布镜像，源码镜像当时仅静态审查；后续源码构建结果见下一节。不能据此声称厂商 NAS ACL、SMB/FUSE、ARM64 实机、真实百度或多日已验收。

## 源码镜像后续验证与修正

继续构建实际源码 Dockerfile，使用 git archive 与独立假配置，绝不把实际 .env/下载目录送入构建。

- [x] 已 RED 复现源码 Compose 缺少 Refresh Token/管理员密码转发，补齐与发布配置一致的可选变量并检查实际解析值。
- [x] 已 RED 用 scratch COPY/export 证明缺少 dockerignore 会包含假 .env/.git/状态；加入忽略规则后复核排除，以及 go.mod/嵌入 UI 资源仍存在。
- [x] 原源码多阶段 Dockerfile 实际构建成功，未替换基础镜像；普通 UID、专用 Linux 卷、network none，通过源码 Compose 启动并确认凭证环境输入/权限/exit 0。源码镜像保持原 dev/unknown，未当作正式发布版。
- [x] 独立只读审查通过，保存前后配置、上下文导出与容器证据，精确清理本轮容器和镜像标签，无 prune；旧包不变。

[Docker build context](https://docs.docker.com/build/concepts/context/) 说明匹配 dockerignore 的文件在发送给 builder 前移出上下文。本项目只排除已知本地配置与状态；自定义下载目录仍建议放在源码树外。
