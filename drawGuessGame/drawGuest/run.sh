#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
BINARY="$PROJECT_DIR/drawGuess"
PID_FILE="$SCRIPT_DIR/drawGuess.pid"
LOG_FILE="$SCRIPT_DIR/drawGuess.log"

if [ ! -x "$BINARY" ]; then
    echo "错误: 未找到可执行文件 $BINARY，请先运行 ./shell/build.sh" >&2
    exit 1
fi

if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
        echo "服务已在运行，PID: $OLD_PID"
        exit 0
    fi
    rm -f "$PID_FILE"
fi

cd "$PROJECT_DIR"
nohup "$BINARY" "$@" >> "$LOG_FILE" 2>&1 < /dev/null &
PID=$!
echo "$PID" > "$PID_FILE"

sleep 1
if ! kill -0 "$PID" 2>/dev/null; then
    echo "错误: 服务启动失败，请检查日志 $LOG_FILE" >&2
    rm -f "$PID_FILE"
    exit 1
fi

echo "服务已启动，PID: $PID"
echo "PID 文件: $PID_FILE"
echo "日志文件: $LOG_FILE"
