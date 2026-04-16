#!/bin/bash
# =============================================================================
# 微博-WeCode 桥接服务 启动脚本
# =============================================================================

set -e

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(dirname "$SCRIPT_DIR")"

# 加载环境变量
if [ -f "$DEPLOY_DIR/.env" ]; then
    echo "Loading environment from .env..."
    set -a
    source "$DEPLOY_DIR/.env"
    set +a
fi

# 设置默认值
WORK_DIR="${WORK_DIR:-/home/ubuntu/workspace}"
DATA_DIR="${DATA_DIR:-/home/ubuntu/.cc-connect}"
LOG_FILE="${LOG_FILE:-$DATA_DIR/logs/cc-connect.log}"

# 创建必要目录
mkdir -p "$WORK_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$DATA_DIR/logs"
mkdir -p "$DATA_DIR/sessions"

# 检查二进制文件
if [ ! -f "$DEPLOY_DIR/bin/cc-connect" ]; then
    echo "Error: cc-connect binary not found at $DEPLOY_DIR/bin/cc-connect"
    exit 1
fi

# 复制配置文件到默认位置
if [ -f "$DEPLOY_DIR/config/config.toml" ]; then
    cp "$DEPLOY_DIR/config/config.toml" "$DATA_DIR/config.toml"
    echo "Config file copied to $DATA_DIR/config.toml"
fi

# 导出 Claude API 环境变量
export ANTHROPIC_API_KEY="${CLAUDE_API_KEY:-}"
export ANTHROPIC_BASE_URL="${CLAUDE_BASE_URL:-}"

# 如果使用 Router
if [ -n "$CLAUDE_ROUTER_URL" ]; then
    export ANTHROPIC_BASE_URL="$CLAUDE_ROUTER_URL"
    export ANTHROPIC_API_KEY="${CLAUDE_ROUTER_API_KEY:-}"
    export NO_PROXY="127.0.0.1"
fi

echo "Starting cc-connect..."
echo "  Work Dir: $WORK_DIR"
echo "  Data Dir: $DATA_DIR"
echo "  Log File: $LOG_FILE"

# 启动服务
cd "$DEPLOY_DIR"
exec "$DEPLOY_DIR/bin/cc-connect"
