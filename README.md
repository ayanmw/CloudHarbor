# 百度网盘 NAS 一键下载系统

> 持续优化分支已提供可靠性预览版的构建与测试说明：[独立版本测试](release/TESTING.md)、[设计](docs/ai/reliability-design.md)、[进度](docs/ai/progress.md)。持久化队列、网页授权应用/保存/续期、管理员表单登录和管理页入队已实现；油猴提供选中路径衔接，需真实页面适配验收。真实百度/NAS 验收仍待进行，以进度文档与当前代码为准。原生宿主程序默认仅监听 `127.0.0.1`；容器内监听 `0.0.0.0`，示例只将端口发布到宿主回环。管理员用户名为 `admin`，密码来自 `ADMIN_PASSWORD` 或下载目录 `.nas-downloader/queue/admin-key.json`；网页使用登录表单，CLI 使用 Bearer。可不带百度应用凭证启动，再在网页首次配置并授权。

## 项目简介

### 新版下载工作台（preview.150）

默认首页采用 Vue 3 + TypeScript，显示根任务总览；点击任务进入文件详情，支持状态筛选、搜索和分页。目录仅汇总数量，不再作为 0 B 文件传输行。网盘管理、路径设置、清理与历史使用统一导航和表格布局。

前端资源由 Vite 编译后嵌入 Go 程序，运行不依赖 CDN 或 Node.js。修改前端或从源码重新生成资源时：

```powershell
npm --prefix frontend ci --no-fund --no-audit
npm --prefix frontend run build
npm --prefix frontend test
go build .
```

发布脚本会自动完成前端构建和交互回归。`/poll` 保留兼容视图。旧版未持久保存的历史子文件明细不会被虚构为空目录；当前根任务完成记录仍保留。

百度网盘 NAS 一键下载系统是一个自动化文件下载解决方案，专为 NAS 用户设计。通过油猴脚本和 Go 服务端的协同工作，实现从百度网盘到本地 NAS 的自动文件传输。

## 核心功能

### 🔄 双端协同自动化
- **油猴脚本端**：读取百度网页明确选中的路径，交给管理页登录、核对和预检；确认后由服务端移动到固定队列
- **Go 服务端**：监控协同目录，自动下载文件到本地 NAS 存储

### ⚡ 断点续传与速度展示
- 支持断点续传功能，网络中断后可从中断处继续下载
- 实时显示下载速度（B/s, KB/s, MB/s, GB/s）
- 自动跳过已存在的完整文件，避免重复下载

### 🖥️ 多种前端界面
- **主页面**：功能完整的下载状态监控界面（WebSocket 实时版）
- **轮询兼容版**：为不支持 WebSocket 的环境提供降级支持
- **OAuth 管理页面**：通过网页界面获取和管理 Access Token

### 🔧 灵活的下载后处理
- 支持两种下载后处理模式：
  - `keep`：保留网盘文件并重命名为 `.可删除` 后缀
  - `delete`：下载完成后自动删除网盘文件

## 技术架构

### 后端技术栈
- **语言**：Go (Golang)
- **API**：百度官方 xpan API
- **部署**：单文件部署，所有前端资源嵌入二进制文件
- **通信**：WebSocket 实时通信 + Ajax 轮询降级支持

### 前端技术栈
- **核心**：原生 HTML/CSS/JavaScript
- **实时通信**：WebSocket
- **兼容性**：Ajax 轮询降级支持

### 部署环境
- **推荐环境**：绿联 DXP4800 + Docker
- **其他环境**：支持任意支持 Docker 的 NAS 设备

## 系统特性

### 📁 文件管理
- 自动扫描协同目录 (`/NAS_Download_Queue`)
- 支持文件和文件夹的递归下载
- 下载完成后自动重命名或删除源文件

### 📊 状态监控
- 实时显示下载任务状态（等待中、下载中、已完成、失败）
- 显示文件大小、下载进度、开始/完成时间
- 统计信息展示（总任务数、下载中、已完成、失败）

### ⚙️ 配置选项
- 可配置的扫描间隔
- 可切换的下载后处理模式
- 环境变量配置（Access Token、目录路径等）

### 🔐 OAuth 认证管理
- 提供基于网页的 OAuth 授权流程
- 支持扫码登录获取 Access Token
- 支持 Refresh Token 自动刷新
- 可在运行时动态更新认证信息

## API 接口

### RESTful API
- `GET /api/health` - 健康检查接口
- `GET /api/status` - 获取下载任务状态
- `POST /api/scan` - 手动触发扫描
- `POST /api/oauth/device-code` - 获取 OAuth 设备码
- `POST /api/oauth/token` - 获取 Access Token
- `POST /api/oauth/refresh` - 刷新 Access Token

### WebSocket 接口
- `/ws` - 实时状态推送通道

## 部署指南

### 环境准备
1. 在[百度网盘开发者平台](https://yun.baidu.com/union)创建应用，复制 AppID、AppKey、SecretKey、SignKey。
2. 可将四项配置写入项目 `.env`，也可启动后在 `/oauth` 页首次填写：
   ```
   BAIDU_APP_ID=your_app_id
   BAIDU_APP_KEY=your_app_key
   BAIDU_SECRET_KEY=your_secret_key
   BAIDU_SIGN_KEY=your_sign_key
   ```
   对四项应用凭证，非空进程环境变量优先于 `.env`；四项最终值都为空时，程序回退到下载目录 `.nas-downloader/queue/baidu-app.json`。四项须成组提供，部分设置会报错。网页首次保存到私有 sidecar 并立即生效，刷新后仅显示掩码，之后网页不再允许修改。该 sidecar 与 OAuth 用户 Token、任务和事件数据库分开；改配置时请停止服务并按需编辑 `.env` 或删除 sidecar。扫描间隔、下载目录等其余运行配置仍通过进程环境变量或 Docker Compose `.env` 传入。

   本工具使用 OAuth 设备码流程，不需要 OAuth 回调页地址。若百度控制台针对所选应用类型强制要求回调地址，请先核对应用类型；当前工具没有授权码回调处理端点，不能用普通管理页地址代替。宿主程序默认只监听 `127.0.0.1`，管理员密码可通过 `ADMIN_PASSWORD` 设置，否则首次启动会生成随机密码并写入下载目录的私有状态文件。浏览器会话不自动过期，点击“退出登录”撤销当前会话；若设备丢失，可停服后运行 `baidu-nas-downloader -revoke-admin-sessions` 撤销全部网页会话。通过 HTTPS 反向代理时配置 `PUBLIC_ORIGIN`。

### Access Token 获取方式

#### 方式一：通过网页 OAuth（推荐）
1. 启动服务后访问 `http://localhost:8080/oauth`
2. 使用管理员账号登录，点击“获取授权码”
3. 在百度授权页面输入用户码，或扫描二维码并授权
4. 返回管理页，点击“已完成百度授权，开始验证”
5. 验证成功后凭证会安全保存并应用于后台任务；页面不显示完整 Token

#### 方式二：命令行工具
项目提供了一个便捷的命令行工具用于获取百度网盘的 Access Token：

```bash
# 进入命令行工具目录
cd cmd/get_token

# 设置必要的环境变量
export BAIDU_APP_KEY=your_app_key
export BAIDU_SECRET_KEY=your_secret_key

# 编译工具
go build -o get_token .

# 运行工具获取 Token
./get_token
```

命令行工具会显示授权链接和二维码，并在完成后输出 Token。命令行输出不会自动保存到服务端凭证文件；需谨慎保管，生产使用优先选择网页授权。授权步骤：
1. 在浏览器中打开链接或扫描二维码
2. 登录百度账号并授权
3. 工具会自动获取并显示 Access Token 和 Refresh Token

### Docker 部署

#### 使用 Docker 命令直接运行
```bash
# 构建镜像
docker build \
  --build-arg BUILD_VERSION="$(git describe --tags --always 2>/dev/null || echo dev)" \
  --build-arg BUILD_REVISION="$(git rev-parse HEAD)" \
  -t baidu-nas-downloader .

# 运行容器
docker run -d \
  --name baidu-nas-downloader \
  -p 127.0.0.1:8080:8080 \
  -e BAIDU_APP_ID=your_app_id \
  -e BAIDU_APP_KEY=your_app_key \
  -e BAIDU_SECRET_KEY=your_secret_key \
  -e BAIDU_SIGN_KEY=your_sign_key \
  -e ADMIN_PASSWORD=replace_with_a_password_of_at_least_10_bytes \
  -v $(pwd)/downloads:/下载 \
  baidu-nas-downloader
```

容器内监听 `0.0.0.0:8080` 以接收 Docker 端口转发；示例只发布到宿主机回环地址。远程访问请使用 SSH 转发或按 HTTPS 反向代理章节配置，不要把未受保护的管理端口直接发布到公网。

#### 使用 Docker Compose（推荐）
项目提供了 `docker-compose.yml` 文件，可以更方便地部署：

```bash
# 复制并编辑 Compose 配置；QUEUE_DIR 是百度网盘远端目录，HOST_PORT 是宿主机端口。
cp .env.example .env
# 源码构建时写入版本和 revision；不设置时显示 dev/unknown。
export BUILD_VERSION=$(git describe --tags --always 2>/dev/null || echo dev)
export BUILD_REVISION=$(git rev-parse HEAD)

# 构建时将版本与源码 revision 写入镜像；以后启动不需要重新构建
docker compose build

# 启动服务
docker compose up -d
```

### Kubernetes 部署

项目目前没有维护 Kubernetes manifest、持久卷权限迁移或集群网络验证。旧版示例使用公网 `LoadBalancer`，还把百度网盘远端队列路径误当成本地容器卷，不应继续照此部署。当前经过验证的容器方式见上方 Docker Compose；Kubernetes 部署需要按集群自行配置持久化下载目录、单实例写入和仅内网/受控代理访问。

## 使用流程

1. 在百度网盘网页端安装油猴脚本并明确选中文件或文件夹
2. 从脚本打开管理页，登录管理员账号；也可直接在管理页浏览选择云端项目
3. 核对百度授权账号、源路径、固定队列目标和下载后处理策略；需要授权时先完成网页设备授权
4. 预检并明确确认后，服务端才会把项目移动到下载队列
5. 后台扫描并下载；默认 keep 会在完成后将队列顶层项目改名为 `.可删除`，delete 模式会删除顶层项目
6. 通过管理页面查看进度、失败和需要人工核对的状态

## 项目结构

```
.
├── cmd/get_token/          # Access Token 获取工具
├── doc/                    # 百度 API SDK 文档
├── internal/
│   ├── baiduapi/          # 百度 API 客户端
│   ├── config/            # 配置管理
│   ├── downloader/        # 核心下载器
│   ├── oauth/             # OAuth 认证管理
│   └── webui/            # Web 界面
├── .env.example           # 环境变量示例
├── go.mod/go.sum          # Go 模块依赖
├── main.go               # 主程序入口
├── Dockerfile            # Docker 镜像构建文件
├── docker-compose.yml    # Docker Compose 配置文件
└── tampermonkey_baidupan_nas_queue.user.js  # 油猴脚本
```

## 注意事项

1. 请妥善保管您的百度 App Key 和 Secret Key
2. 确保 NAS 存储空间充足
3. 遵守百度网盘使用条款，合理使用 API
4. 建议定期清理已下载的标记文件

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request 来改进这个项目。
