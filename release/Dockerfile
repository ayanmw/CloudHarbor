FROM alpine:3.22
RUN apk --no-cache add ca-certificates
ARG TARGETARCH
COPY bin/linux-${TARGETARCH}/baidu-downloader /app/baidu-downloader
RUN chmod +x /app/baidu-downloader
WORKDIR /app
EXPOSE 8080
ENV SERVER_HOST=0.0.0.0 SERVER_PORT=8080 LOCAL_DOWNLOAD_DIR=/downloads
ENTRYPOINT ["/app/baidu-downloader"]
