# 百度网盘 NAS 一键下载系统

## 项目简介

百度网盘 NAS 一键下载系统是一个自动化文件下载解决方案，专为 NAS 用户设计。通过油猴脚本和 Go 服务端的协同工作，实现从百度网盘到本地 NAS 的自动文件传输。

## 核心功能

### 🔄 双端协同自动化
- **油猴脚本端**：在百度网盘网页端操作，将选中的文件/文件夹移动到指定协同目录
- **Go 服务端**：监控协同目录，自动下载文件到本地 NAS 存储

### ⚡ 断点续传与速度展示
- 支持断点续传功能，网络中断后可从中断处继续下载
- 实时显示下载速度（B/s, KB/s, MB/s, GB/s）
- 自动跳过已存在的完整文件，避免重复下载

### 🖥️ 多种前端界面
- **主页面**：功能完整的下载状态监控界面
- **WebSocket 实时版**：通过 WebSocket 实现实时状态更新
- **轮询兼容版**：为不支持 WebSocket 的环境提供降级支持

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

## API 接口

### RESTful API
- `GET /api/health` - 健康检查接口
- `GET /api/status` - 获取下载任务状态
- `POST /api/scan` - 手动触发扫描

### WebSocket 接口
- `/ws` - 实时状态推送通道

## 部署指南

### 环境准备
1. 获取百度网盘 API Access Token
2. 配置环境变量：
   ```
   BAIDU_ACCESS_TOKEN=your_access_token
   BAIDU_APP_KEY=your_app_key
   BAIDU_SECRET_KEY=your_secret_key
   QUEUE_DIR=/NAS_Download_Queue
   LOCAL_DOWN_DIR=/下载
   SCAN_INTERVAL=30
   SERVER_PORT=8080
   ```

### Access Token 获取工具
项目提供了一个便捷的命令行工具用于获取百度网盘的 Access Token：

```bash
# 进入 get_token 目录
cd cmd/get_token

# 设置必要的环境变量
export BAIDU_APP_KEY=your_app_key
export BAIDU_SECRET_KEY=your_secret_key

# 编译工具
go build -o get_token main.go

# 运行工具获取 Token
./get_token
```

运行后，工具会显示授权链接和二维码，您需要：
1. 在浏览器中打开链接或扫描二维码
2. 登录百度账号并授权
3. 工具会自动获取并显示 Access Token 和 Refresh Token

### Docker 部署

#### 使用 Docker 命令直接运行
```bash
# 构建镜像
docker build -t baidu-nas-downloader .

# 运行容器
docker run -d \
  --name baidu-nas-downloader \
  -p 8080:8080 \
  -e BAIDU_ACCESS_TOKEN=your_access_token \
  -e BAIDU_APP_KEY=your_app_key \
  -e BAIDU_SECRET_KEY=your_secret_key \
  -v $(pwd)/downloads:/下载 \
  -v $(pwd)/queue:/NAS_Download_Queue \
  baidu-nas-downloader
```

#### 使用 Docker Compose（推荐）
项目提供了 `docker-compose.yml` 文件，可以更方便地部署：

```bash
# 设置环境变量
export BAIDU_ACCESS_TOKEN=your_access_token
export BAIDU_APP_KEY=your_app_key
export BAIDU_SECRET_KEY=your_secret_key

# 启动服务
docker-compose up -d
```

### Kubernetes 部署

#### 创建 Deployment 和 Service
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: baidu-nas-downloader
  labels:
    app: baidu-nas-downloader
spec:
  replicas: 1
  selector:
    matchLabels:
      app: baidu-nas-downloader
  template:
    metadata:
      labels:
        app: baidu-nas-downloader
    spec:
      containers:
      - name: baidu-nas-downloader
        image: your-registry/baidu-nas-downloader:latest
        ports:
        - containerPort: 8080
        env:
        - name: BAIDU_ACCESS_TOKEN
          valueFrom:
            secretKeyRef:
              name: baidu-secrets
              key: access-token
        - name: BAIDU_APP_KEY
          valueFrom:
            secretKeyRef:
              name: baidu-secrets
              key: app-key
        - name: BAIDU_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: baidu-secrets
              key: secret-key
        volumeMounts:
        - name: downloads-volume
          mountPath: /下载
        - name: queue-volume
          mountPath: /NAS_Download_Queue
      volumes:
      - name: downloads-volume
        persistentVolumeClaim:
          claimName: downloads-pvc
      - name: queue-volume
        persistentVolumeClaim:
          claimName: queue-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: baidu-nas-downloader
spec:
  selector:
    app: baidu-nas-downloader
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
  type: LoadBalancer
```

#### 创建 Secrets
```bash
kubectl create secret generic baidu-secrets \
  --from-literal=access-token=your_access_token \
  --from-literal=app-key=your_app_key \
  --from-literal=secret-key=your_secret_key
```

#### 应用配置
```bash
kubectl apply -f deployment.yaml
```

## 使用流程

1. 在百度网盘网页端安装油猴脚本
2. 选中需要下载的文件/文件夹
3. 点击油猴脚本按钮，文件将被移动到协同目录
4. Go 服务端自动检测并开始下载
5. 通过 Web 界面实时监控下载进度

## 项目结构

```
.
├── cmd/get_token/          # Access Token 获取工具
├── doc/                    # 百度 API SDK 文档
├── internal/
│   ├── baiduapi/          # 百度 API 客户端
│   ├── config/            # 配置管理
│   ├── downloader/        # 核心下载器
│   └── webui/            # Web 界面
├── .env.example           # 环境变量示例
├── go.mod/go.sum          # Go 模块依赖
├── main.go               # 主程序入口
├── Dockerfile            # Docker 镜像构建文件
├── docker-compose.yml    # Docker Compose 配置文件
└── tampermonkey_baidupan_nas_queue.user.js  # 油猴脚本
```

## 注意事项

1. 请妥善保管您的百度 Access Token
2. 确保 NAS 存储空间充足
3. 遵守百度网盘使用条款，合理使用 API
4. 建议定期清理已下载的标记文件

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request 来改进这个项目。