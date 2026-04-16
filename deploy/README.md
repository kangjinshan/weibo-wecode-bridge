# 微博-WeCode 桥接服务 部署指南

## 目录结构

```
deploy/
├── bin/
│   └── cc-connect          # 编译好的 Linux amd64 二进制文件
├── config/
│   └── config.toml         # 配置文件模板
├── scripts/
│   ├── install.sh          # 安装脚本
│   ├── start.sh            # 启动脚本
│   └── cc-connect.service  # systemd 服务文件
└── .env                    # 环境变量配置
```

## 快速部署

### 1. 上传到服务器

```bash
# 打包
cd /Users/kanayama/Desktop/AI/weibo-wecode-bridge
tar -czf deploy.tar.gz -C deploy .

# 上传
scp deploy.tar.gz ubuntu@10.201.0.166:/home/ubuntu/

# 解压
ssh ubuntu@10.201.0.166 "cd /home/ubuntu && mkdir -p weibo-wecode-bridge && tar -xzf deploy.tar.gz -C weibo-wecode-bridge"
```

### 2. 配置环境变量

编辑 `.env` 文件：

```bash
nano /home/ubuntu/weibo-wecode-bridge/.env
```

**必须配置的项目：**

| 变量 | 说明 | 获取方式 |
|------|------|----------|
| `WEIBO_APP_ID` | 微博应用 ID | 联系 @微博龙虾助手 |
| `WEIBO_APP_Secret` | 微博应用密钥 | 联系 @微博龙虾助手 |
| `CLAUDE_API_KEY` | Claude API 密钥 | Anthropic 官网 |

### 3. 安装服务

```bash
cd /home/ubuntu/weibo-wecode-bridge
sudo bash scripts/install.sh
```

### 4. 启动服务

```bash
sudo systemctl start cc-connect
sudo systemctl status cc-connect
```

## 配置说明

### 环境变量 (.env)

| 变量 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `WEIBO_APP_ID` | ✅ | - | 微博应用 ID |
| `WEIBO_APP_Secret` | ✅ | - | 微博应用密钥 |
| `CLAUDE_API_KEY` | ✅ | - | Claude API 密钥 |
| `CLAUDE_BASE_URL` | ❌ | api.anthropic.com | API 端点 |
| `WORK_DIR` | ❌ | /home/ubuntu/workspace | Agent 工作目录 |
| `DATA_DIR` | ❌ | /home/ubuntu/.cc-connect | 数据存储目录 |
| `LOG_LEVEL` | ❌ | info | 日志级别 |
| `LANGUAGE` | ❌ | zh | 语言设置 |

### Claude API 配置方式

**方式一：直接使用 Anthropic API**

```env
CLAUDE_API_KEY=sk-ant-xxx
CLAUDE_BASE_URL=https://api.anthropic.com
```

**方式二：使用 Claude Code Router / WeCode Proxy**

```env
CLAUDE_ROUTER_URL=http://127.0.0.1:3456
CLAUDE_ROUTER_API_KEY=sk-wecode-proxy-claude-code-sk
```

**方式三：使用第三方兼容 API**

```env
CLAUDE_API_KEY=sk-xxx
CLAUDE_BASE_URL=https://your-api-proxy.com
```

### config.toml 配置

主要配置项：

```toml
# Agent 工作目录
[projects.agent.options]
work_dir = "/home/ubuntu/workspace"
mode = "default"  # default | acceptEdits | plan | auto | bypassPermissions

# 微博平台
[projects.platforms.options]
app_id = "your_app_id"
app_secret = "your_app_secret"
```

## 服务管理

```bash
# 启动
sudo systemctl start cc-connect

# 停止
sudo systemctl stop cc-connect

# 重启
sudo systemctl restart cc-connect

# 查看状态
sudo systemctl status cc-connect

# 查看日志
sudo journalctl -u cc-connect -f

# 查看最近 100 行日志
sudo journalctl -u cc-connect -n 100
```

## 常见问题

### 1. 微博连接失败

检查：
- `WEIBO_APP_ID` 和 `WEIBO_APP_Secret` 是否正确
- 网络是否可访问 `open-im.api.weibo.com`

### 2. Claude API 调用失败

检查：
- `CLAUDE_API_KEY` 是否有效
- `CLAUDE_BASE_URL` 是否正确
- 网络是否可访问 API 端点

### 3. 服务无法启动

检查日志：
```bash
sudo journalctl -u cc-connect -n 50
```

## 端口需求

| 端口 | 协议 | 说明 |
|------|------|------|
| 出站 80/443 | TCP | 微博 API / Claude API |

## 更新部署

```bash
# 停止服务
sudo systemctl stop cc-connect

# 备份配置
cp /home/ubuntu/weibo-wecode-bridge/.env /tmp/.env.bak
cp /home/ubuntu/.cc-connect/config.toml /tmp/config.toml.bak

# 上传新版本
scp deploy.tar.gz ubuntu@10.201.0.166:/home/ubuntu/
ssh ubuntu@10.201.0.166 "cd /home/ubuntu && tar -xzf deploy.tar.gz -C weibo-wecode-bridge"

# 恢复配置
cp /tmp/.env.bak /home/ubuntu/weibo-wecode-bridge/.env

# 启动服务
sudo systemctl start cc-connect
```

---

**创建时间**: 2026-04-16
