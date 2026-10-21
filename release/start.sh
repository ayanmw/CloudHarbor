#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
: "${BAIDU_ACCESS_TOKEN:?Set BAIDU_ACCESS_TOKEN before starting}"
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo 'Only Linux AMD64 and ARM64 are packaged.' >&2; exit 1 ;;
esac
export QUEUE_DIR="${QUEUE_DIR:-/NAS_Download_Queue_preview}"
export LOCAL_DOWNLOAD_DIR="${LOCAL_DOWNLOAD_DIR:-$(pwd)/downloads}"
export SCAN_INTERVAL="${SCAN_INTERVAL:-30}"
export MAX_PARALLEL="${MAX_PARALLEL:-2}"
export MAX_RETRIES="${MAX_RETRIES:-3}"
export RETRY_BACKOFF_SECONDS="${RETRY_BACKOFF_SECONDS:-5}"
export SERVER_PORT="${SERVER_PORT:-18089}"
export SERVER_HOST="${SERVER_HOST:-127.0.0.1}"
chmod +x "./bin/linux-$arch/baidu-downloader"
exec "./bin/linux-$arch/baidu-downloader"
