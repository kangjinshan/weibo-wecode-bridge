# Weibo-WeCode Bridge - AI Agent 配置指南

> 本文档为 AI Agent（Claude Code、Gemini CLI 等）提供项目配置和开发指南。

## 项目概述

微博私信与 WeCode CLI 的双向通信桥接服务，基于 cc-connect 实现。

### 核心功能

- 微博 OpenIM WebSocket 实时通信
- 消息收发、去重、分块
- 多 Provider 支持
- systemd 服务管理

## 技术栈

| 组件 | 技术 |
|------|------|
| 核心框架 | Go 1.24+ (cc-connect) |
| 微博平台 | WebSocket, HTTP API |
| AI Agent | Claude Code CLI |
| API 代理 | WeCode Proxy (端口 3456) |

## 项目结构

```
weibo-wecode-bridge/
├── deploy/                      # 独立部署包（生产环境）
│   ├── bin/cc-connect          # 编译好的二进制文件
│   ├── config/config.toml      # 配置文件
│   ├── scripts/                # 安装/启动脚本
│   └── .env                    # 环境变量
│
├── cc-connect-main/            # cc-connect 源码
│   ├── platform/weibo/         # 微博平台实现 ⭐
│   ├── cmd/cc-connect/         # 命令入口
│   ├── core/                   # 核心引擎
│   ├── agent/                  # Agent 适配器
│   └── config/                 # 配置解析
│
├── cc-connect/                 # 微博平台扩展（独立模块）
│   └── platform/weibo/
│
└── src/                        # Node.js 备选实现（已弃用）
```

## 配置文件位置

| 文件 | 路径 | 说明 |
|------|------|------|
| 主配置 | ~/.cc-connect/config.toml | cc-connect 运行配置 |
| 环境变量 | deploy/.env | 敏感信息配置 |
| 会话存储 | ~/.cc-connect/sessions/ | 会话状态 JSON |
| 日志 | ~/.cc-connect/logs/ | 运行日志 |

## 配置模板

### config.toml

```toml
language = "zh"
data_dir = "/home/ubuntu/.cc-connect"

[log]
level = "info"

[[projects]]
name = "AI"

[projects.agent]
type = "claudecode"

[projects.agent.options]
work_dir = "/home/ubuntu/workspace"
mode = "default"
provider = "wecode"  # ⚠️ 必须放在 options 里！

[[projects.agent.providers]]
name = "wecode"
api_key = "sk-wecode-proxy-claude-code-sk"
base_url = "http://127.0.0.1:3456"

[[projects.platforms]]
type = "weibo"

[projects.platforms.options]
app_id = "5288388166418433"
app_secret = "0e2925708f3d09d9f59397737c75432d6683f72edf0d93bf398937254e713a01"
token_url = "http://open-im.api.weibo.com/open/auth/ws_token"
ws_url = "ws://open-im.api.weibo.com/ws/stream"
allow_from = "*"
account_id = "default"
```

### ⚠️ 重要：provider 字段位置

**provider 必须放在 [projects.agent.options] 下面，不能放在 [projects.agent] 下面！**

```toml
# ✅ 正确写法
[projects.agent]
type = "claudecode"

[projects.agent.options]
work_dir = "/path/to/workspace"
provider = "wecode"  # ← 必须在这里

# ❌ 错误写法 - provider 放在 [projects.agent] 下会被忽略
[projects.agent]
type = "claudecode"
provider = "wecode"  # ← 这里无效！
```

**原因**：cc-connect 从 proj.Agent.Options["provider"] 读取 provider。

**症状**：provider 未正确设置时，会收到 "Not logged in · Please run /login" 错误。

## 微博平台实现

核心文件：cc-connect-main/platform/weibo/weibo.go

### 关键方法

| 方法 | 功能 |
|------|------|
| New() | 平台初始化 |
| Start() | 启动服务 |
| Stop() | 停止服务 |
| Reply() / Send() | 发送消息 |
| fetchToken() | 获取访问令牌 |
| connectLoop() | WebSocket 连接循环 |
| readLoop() | 消息读取循环 |
| heartbeatLoop() | 心跳保活 |

### 消息格式

**入站消息（微博 → 服务）：**
```json
{
  "type": "message",
  "payload": {
    "messageId": "xxx",
    "fromUserId": "用户ID",
    "text": "消息内容",
    "timestamp": 1712500000000
  }
}
```

**出站消息（服务 → 微博）：**
```json
{
  "type": "send_message",
  "payload": {
    "toUserId": "用户ID",
    "text": "回复内容",
    "messageId": "msg_xxx",
    "chunkId": 0,
    "done": true
  }
}
```

## 部署流程

### 1. 编译

```bash
cd cc-connect-main
export GOPROXY=https://goproxy.cn,direct
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../deploy/bin/cc-connect ./cmd/cc-connect
```

### 2. 配置

```bash
# 编辑环境变量
nano deploy/.env

# 复制配置到数据目录
cp deploy/config/config.toml ~/.cc-connect/config.toml
```

### 3. 安装服务

```bash
sudo cp deploy/scripts/cc-connect.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable cc-connect
sudo systemctl start cc-connect
```

### 4. 验证

```bash
# 检查服务状态
sudo systemctl status cc-connect

# 查看日志
sudo journalctl -u cc-connect -f

# 或查看文件日志
tail -f ~/.cc-connect/logs/cc-connect.log
```

## 服务管理命令

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
sudo journalctl -u cc-connect -f -n 100
```

## 端口需求

| 端口 | 方向 | 用途 |
|------|------|------|
| 80/443 出站 | TCP | 微博 API |
| 3456 本地 | TCP | WeCode Proxy |

## 常见问题排查

### 1. 微博连接失败

```bash
# 检查凭证
grep app_id ~/.cc-connect/config.toml

# 测试网络
curl http://open-im.api.weibo.com/open/auth/ws_token

# 查看日志
grep weibo ~/.cc-connect/logs/cc-connect.log
```

### 2. Claude 无响应

```bash
# 检查 Claude CLI
which claude && claude --version

# 检查 WeCode Proxy
curl http://127.0.0.1:3456

# 检查 provider 配置
grep provider ~/.cc-connect/config.toml
```

### 3. 服务无法启动

```bash
# 检查配置文件
cat ~/.cc-connect/config.toml

# 检查二进制文件
ls -la /home/ubuntu/weibo-wecode-bridge/bin/cc-connect

# 手动运行测试
/home/ubuntu/weibo-wecode-bridge/bin/cc-connect
```

## 开发指南

### 修改微博平台代码

1. 编辑 cc-connect-main/platform/weibo/weibo.go
2. 重新编译：go build -o cc-connect ./cmd/cc-connect
3. 替换部署包中的二进制文件
4. 重启服务

### 添加新功能

参考 cc-connect 的插件架构：
- 平台插件：platform/xxx/xxx.go + cmd/cc-connect/plugin_platform_xxx.go
- Agent 插件：agent/xxx/xxx.go + cmd/cc-connect/plugin_agent_xxx.go

### 测试

```bash
cd cc-connect-main
go test ./platform/weibo/... -v
```

## 相关文档

- [cc-connect 官方文档](https://github.com/chenhg5/cc-connect)
- [微博 OpenIM API](http://open.weibo.com)

---

**最后更新**: 2026-04-16
