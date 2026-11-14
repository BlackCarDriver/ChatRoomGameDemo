#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
if [ -x "$SCRIPT_DIR/coinMachine" ]; then
    # 发布包可以把脚本和执行文件放在同一目录。
    APP_DIR="$SCRIPT_DIR"
else
    APP_DIR="$PROJECT_DIR"
fi
PID_FILE="$APP_DIR/coinMachine.pid"

if [ ! -f "$PID_FILE" ]; then
    echo "服务未运行（未找到 PID 文件）"
    exit 0
fi

PID=$(cat "$PID_FILE")
case "$PID" in
    ''|*[!0-9]*)
        echo "错误: PID 文件无效: $PID" >&2
        rm -f "$PID_FILE"
        exit 1
        ;;
esac

if ! kill -0 "$PID" 2>/dev/null; then
    echo "服务已停止，清理过期 PID 文件"
    rm -f "$PID_FILE"
    exit 0
fi

echo "正在停止服务，PID: $PID"
kill -TERM "$PID" 2>/dev/null || true

count=0
while kill -0 "$PID" 2>/dev/null && [ "$count" -lt 10 ]; do
    sleep 1
    count=$((count + 1))
done

if kill -0 "$PID" 2>/dev/null; then
    echo "服务未响应 TERM，强制停止"
    kill -KILL "$PID" 2>/dev/null || true
fi

rm -f "$PID_FILE"
echo "服务已停止"
