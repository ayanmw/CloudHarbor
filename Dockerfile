# 构建阶段
FROM swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/golang:1.22-alpine AS builder
# 设置工作目录
WORKDIR /app
# 复制 go mod 和 sum 文件
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
# 下载依赖
RUN go mod download
# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o baidu-downloader .





# 最终阶段
FROM swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/alpine:latest
# 安装 ca-certificates 以支持 HTTPS 请求
RUN apk --no-cache add ca-certificates
# 创建工作目录
WORKDIR /root/
# 从构建阶段复制二进制文件
COPY --from=builder /app/baidu-downloader .
# 创建必要的目录
RUN mkdir -p /下载
# 暴露端口
EXPOSE 8080
# 设置环境变量默认值
ENV BAIDU_ACCESS_TOKEN=""
ENV BAIDU_APP_KEY=""
ENV BAIDU_SECRET_KEY=""
ENV QUEUE_DIR="/NAS_Download_Queue"
ENV LOCAL_DOWNLOAD_DIR="/下载"
ENV SCAN_INTERVAL="30"
ENV SERVER_PORT="8080"

# 启动应用
CMD ["./baidu-downloader"]
