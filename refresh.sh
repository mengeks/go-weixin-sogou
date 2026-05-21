#!/usr/bin/env bash
# 一键刷新 ConnectEd 公众号文章 Excel 资源库
# 用法:
#   首次全量抓取:  ./refresh.sh
#   增量刷新:      ./refresh.sh --refresh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$SCRIPT_DIR/cmd/scrape/scrape"

echo "=== 编译抓取工具 ==="
go build -o "$BINARY" "$SCRIPT_DIR/cmd/scrape/"

echo ""
echo "=== 开始抓取 ==="
"$BINARY" "$@"

echo ""
echo "=== 完成 ==="
echo "Excel 文件: $SCRIPT_DIR/ConnectEd_articles.xlsx"
