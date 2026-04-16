#!/bin/bash
# =============================================================================
# 微博-WeCode 桥接服务 安装脚本
# =============================================================================

set -e

echo "=============================================="
echo "  微博-WeCode 桥接服务 安装脚本"
echo "=============================================="

# 检查 root 权限
if [ "$EUID" -ne 0 ]; then
    echo "请使用 sudo 运行此脚本"
    exit 1
fi

# 获取部署目录
DEPLOY_DIR="/home/ubuntu/weibo-wecode-bridge"
SERVICE_NAME="cc-connect"

echo ""
echo "部署目录: $DEPLOY_DIR"
echo ""

# 创建目录
echo "创建目录..."
mkdir -p "$DEPLOY_DIR"
mkdir -p /home/ubuntu/workspace
mkdir -p /home/ubuntu/.cc-connect/logs
mkdir -p /home/ubuntu/.cc-connect/sessions

# 复制文件
echo "复制文件..."
cp -r bin "$DEPLOY_DIR/"
cp -r config "$DEPLOY_DIR/"
cp -r scripts "$DEPLOY_DIR/"
cp .env "$DEPLOY_DIR/"

# 设置权限
echo "设置权限..."
chown -R ubuntu:ubuntu "$DEPLOY_DIR"
chown -R ubuntu:ubuntu /home/ubuntu/workspace
chown -R ubuntu:ubuntu /home/ubuntu/.cc-connect
chmod +x "$DEPLOY_DIR/bin/cc-connect"
chmod +x "$DEPLOY_DIR/scripts/start.sh"

# 安装 systemd 服务
echo "安装 systemd 服务..."
cp scripts/cc-connect.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable cc-connect

echo ""
echo "=============================================="
echo "  安装完成!"
echo "=============================================="
echo ""
echo "下一步:"
echo ""
echo "1. 编辑配置文件:"
echo "   nano $DEPLOY_DIR/.env"
echo "   nano $DEPLOY_DIR/config/config.toml"
echo ""
echo "   需要配置:"
echo "   - WEIBO_APP_ID (从 @微博龙虾助手 获取)"
echo "   - WEIBO_APP_Secret (从 @微博龙虾助手 获取)"
echo "   - CLAUDE_API_KEY (Claude API 密钥)"
echo ""
echo "2. 启动服务:"
echo "   systemctl start cc-connect"
echo ""
echo "3. 查看状态:"
echo "   systemctl status cc-connect"
echo ""
echo "4. 查看日志:"
echo "   journalctl -u cc-connect -f"
echo ""
