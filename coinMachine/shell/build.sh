#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")

cd "$PROJECT_DIR"

# 有 Node 依赖时重新打包前端；没有则直接使用已提交的 public/app.js。
if command -v npm >/dev/null 2>&1 && [ -d web/node_modules ]; then
    echo "构建前端…"
    (cd web && npm run build)
else
    echo "跳过前端构建（未安装 web/node_modules），使用 public/app.js"
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o coinMachine .
echo "已生成 Linux 可执行文件：$PROJECT_DIR/coinMachine"
