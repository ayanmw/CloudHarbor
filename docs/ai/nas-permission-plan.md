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

本轮实际 Docker/Compose 为 WSL Ubuntu、AMD64、临时 ext4 bind mount。发布镜像构建与普通用户容器已验证；源码多阶段镜像只做路径静态审查，未重新构建。不能据此声称厂商 NAS ACL、SMB/FUSE、ARM64 实机、真实百度或多日已验收。
