# Weibo-WeCode Bridge

微博私信与 WeCode CLI 的双向通信桥接服务，基于 [cc-connect](https://github.com/chenhg5/cc-connect) 实现。

## 功能特性

- ✅ 微博 OpenIM WebSocket 实时通信
- ✅ Token 自动刷新与心跳保活
- ✅ 断线自动重连
- ✅ 消息去重
- ✅ 超长消息分块发送
- ✅ 多 Provider 支持（WeCode Proxy / Anthropic API / 第三方 API）
- ✅ systemd 服务支持

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                         用户                                 │
│                    ↓ 微博私信 ↑                              │
│              ┌──────────────────┐                           │
│              │  微博 OpenIM API  │                           │
│              │   WebSocket 服务  │                           │
│              └────────┬─────────┘                           │
│                       │                                      │
│              ┌────────▼─────────┐                           │
│              │   cc-connect     │                           │
│              │   Weibo Platform │                           │
│              └────────┬─────────┘                           │
│                       │                                      │
│              ┌────────▼─────────┐                           │
│              │   WeCode Proxy   │                           │
│              │   (Claude Code)  │                           │
│              └──────────────────┘                           │
└─────────────────────────────────────────────────────────────┘
```

## 项目结构

```
weibo-wecode-bridge/
├── deploy/                      # 独立部署包
│   ├── bin/
│   │   └── cc-connect          # Linux amd64 二进制文件
│   ├── config/
│   │   └── config.toml         # 配置文件模板
│   ├── scripts/
│   │   ├── install.sh          # 安装脚本
│   │   ├── start.sh            # 启动脚本
│   │   └── cc-connect.service  # systemd 服务文件
│   ├── .env                    # 环境变量配置
│   └── README.md               # 部署文档
├── cc-connect-main/            # cc-connect 源码
│   ├── platform/weibo/         # 微博平台实现
│   ├── cmd/cc-connect/         # 命令入口
│   ├── go.mod
│   └── Makefile
└── README.md                   # 本文件
```

## 快速开始

### 前置要求

- 微博开发者凭证（从 @微博龙虾助手 获取）
- Claude API 密钥 或 WeCode Proxy 服务

### 1. 获取微博凭证

1. 打开微博 App
2. 私信 **@微博龙虾助手**
3. 发送 "连接龙虾"
4. 获取 `app_id` 和 `app_secret`

### 2. 配置

编辑 `deploy/.env`：

```env
# 微博凭证
WEIBO_APP_ID=your_app_id
WEIBO_APP_Secret=your_app_secret

# Claude API（三选一）

# 方式一：直接使用 Anthropic API
CLAUDE_API_KEY=sk-ant-xxx

# 方式二：使用 WeCode Proxy（推荐）
CLAUDE_ROUTER_URL=http://127.0.0.1:3456
CLAUDE_ROUTER_API_KEY=sk-wecode-proxy-claude-code-sk

# 方式三：使用第三方兼容 API
CLAUDE_API_KEY=sk-xxx
CLAUDE_BASE_URL=https://your-api-proxy.com
```

### 3. 部署

```bash
# 打包
cd weibo-wecode-bridge
tar -czf deploy.tar.gz -C deploy .

# 上传到服务器
scp deploy.tar.gz user@server:/home/ubuntu/

# 解压并安装
ssh user@server
cd /home/ubuntu && mkdir -p weibo-wecode-bridge
tar -xzf deploy.tar.gz -C weibo-wecode-bridge
sudo bash weibo-wecode-bridge/scripts/install.sh

# 启动服务
sudo systemctl start cc-connect
```

## 配置说明

### 环境变量

| 变量 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `WEIBO_APP_ID` | ✅ | - | 微博应用 ID |
| `WEIBO_APP_Secret` | ✅ | - | 微博应用密钥 |
| `CLAUDE_API_KEY` | ✅* | - | Claude API 密钥 |
| `CLAUDE_BASE_URL` | ❌ | - | API 端点 |
| `CLAUDE_ROUTER_URL` | ❌ | - | Router 地址 |
| `WORK_DIR` | ❌ | /home/ubuntu/workspace | Agent 工作目录 |
| `DATA_DIR` | ❌ | /home/ubuntu/.cc-connect | 数据目录 |
| `LOG_LEVEL` | ❌ | info | 日志级别 |
| `LANGUAGE` | ❌ | zh | 语言设置 |

*使用 WeCode Proxy 时无需配置 `CLAUDE_API_KEY`

### config.toml

```toml
language = "zh"
data_dir = "/home/ubuntu/.cc-connect"

[[projects]]
name = "AI"

[projects.agent]
type = "claudecode"

[projects.agent.options]
work_dir = "/home/ubuntu/workspace"
mode = "default"
provider = "wecode"

[[projects.agent.providers]]
name = "wecode"
api_key = "sk-wecode-proxy-claude-code-sk"
base_url = "http://127.0.0.1:3456"

[[projects.platforms]]
type = "weibo"

[projects.platforms.options]
app_id = "your_app_id"
app_secret = "your_app_secret"
token_url = "http://open-im.api.weibo.com/open/auth/ws_token"
ws_url = "ws://open-im.api.weibo.com/ws/stream"
allow_from = "*"
account_id = "default"
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
```

## 微博 API 参考

### 获取 Token

```http
POST http://open-im.api.weibo.com/open/auth/ws_token
Content-Type: application/json

{
  "app_id": "your_app_id",
  "app_secret": "your_app_secret"
}
```

### WebSocket 连接

```
ws://open-im.api.weibo.com/ws/stream?app_id={app_id}&token={token}
```

### 心跳

客户端每 30 秒发送：
```json
{"type": "ping"}
```

服务端响应：
```json
{"type": "pong"}
```

## 常见问题

### Q: 微博连接失败？

检查：
1. `app_id` 和 `app_secret` 是否正确
2. 网络是否可访问 `open-im.api.weibo.com`
3. 查看日志中的错误信息

### Q: 消息发送失败？

检查：
1. WebSocket 连接状态（日志显示 `weibo: connected`）
2. 消息长度是否超过 4000 字符（会自动分块）

### Q: Claude 无响应？

检查：
1. Claude Code CLI 是否安装：`claude --version`
2. WeCode Proxy 是否运行：`curl http://127.0.0.1:3456`
3. API Key 是否有效

## 依赖

| 依赖 | 版本 | 说明 |
|------|------|------|
| Go | 1.24+ | 编译环境（仅编译时需要） |
| Claude Code CLI | 2.x | AI 编码助手 |
| WeCode Proxy | - | API 代理服务（可选） |

## 从源码编译

```bash
cd cc-connect-main

# 设置 Go 环境
export GOPROXY=https://goproxy.cn,direct

# 编译 Linux 版本
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o cc-connect ./cmd/cc-connect

# 编译 macOS 版本
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o cc-connect-darwin ./cmd/cc-connect
```

## 参考项目

- [cc-connect](https://github.com/chenhg5/cc-connect) - 多平台消息桥接框架

## 许可证

MIT License

---

**创建时间**: 2026-04-07
**最后更新**: 2026-04-16
