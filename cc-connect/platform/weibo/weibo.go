package weibo

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/spf13/viper"
)

// Platform 微博平台实现
// 实现 cc-connect 的 Platform 接口
type Platform struct {
	config *Config
	client *Client

	// 消息通道
	messageChan chan Message

	// 会话管理
	sessions sync.Map // sessionID -> *Session

	// 状态
	running bool
	mu      sync.RWMutex

	// 日志
	logger *log.Logger
}

// Session 会话信息
type Session struct {
	ID         string
	UserID     string
	LastActive int64
}

// New 创建微博平台实例
func New(v *viper.Viper, logger *log.Logger) (*Platform, error) {
	cfg, err := ParseConfig(v)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	p := &Platform{
		config:      cfg,
		messageChan: make(chan Message, 100),
		logger:      logger,
	}

	return p, nil
}

// Name 返回平台名称
func (p *Platform) Name() string {
	return PlatformType
}

// Start 启动平台
func (p *Platform) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = true
	p.mu.Unlock()

	// 创建客户端
	p.client = NewClient(
		p.config.AppID,
		p.config.AppSecret,
		p.config.TokenURL,
		p.config.WSURL,
	)

	// 设置回调
	p.client.OnMessage(func(msg WeiboMessage) {
		p.handleMessage(msg)
	})

	p.client.OnConnect(func() {
		p.logger.Printf("[weibo] connected to weibo server")
	})

	p.client.OnDisconnect(func(err error) {
		p.logger.Printf("[weibo] disconnected: %v", err)
	})

	// 连接
	if err := p.client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	p.logger.Printf("[weibo] platform started")

	// 等待上下文取消
	go func() {
		<-ctx.Done()
		p.Stop()
	}()

	return nil
}

// Stop 停止平台
func (p *Platform) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.running = false

	if p.client != nil {
		p.client.Close()
	}

	close(p.messageChan)

	p.logger.Printf("[weibo] platform stopped")

	return nil
}

// Messages 返回消息通道
// cc-connect 通过此通道接收消息
func (p *Platform) Messages() <-chan Message {
	return p.messageChan
}

// Send 发送消息
func (p *Platform) Send(sessionID, content string) error {
	userID := ExtractUserIDFromSession(sessionID)
	if userID == "" {
		return fmt.Errorf("invalid session id: %s", sessionID)
	}

	return p.client.Send(userID, content)
}

// SendChunk 分块发送消息 (流式输出)
func (p *Platform) SendChunk(sessionID, content string, chunkID int, done bool) error {
	userID := ExtractUserIDFromSession(sessionID)
	if userID == "" {
		return fmt.Errorf("invalid session id: %s", sessionID)
	}

	return p.client.SendChunk(userID, content, chunkID, done)
}

// handleMessage 处理接收到的微博消息
func (p *Platform) handleMessage(msg WeiboMessage) {
	// 解析为内部消息格式
	message := ParseWeiboMessage(msg)

	// 更新会话
	sessionKey := message.SessionID
	p.sessions.Store(sessionKey, &Session{
		ID:         sessionKey,
		UserID:     message.UserID,
		LastActive: message.Timestamp.Unix(),
	})

	// 发送到消息通道
	select {
	case p.messageChan <- message:
		p.logger.Printf("[weibo] message received from %s, content len: %d",
			message.UserID, len(message.Content))
	default:
		p.logger.Printf("[weibo] message channel full, dropping message")
	}
}

// GetSession 获取会话
func (p *Platform) GetSession(sessionID string) (*Session, bool) {
	value, ok := p.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	return value.(*Session), true
}

// ListSessions 列出所有会话
func (p *Platform) ListSessions() []*Session {
	var sessions []*Session
	p.sessions.Range(func(key, value interface{}) bool {
		sessions = append(sessions, value.(*Session))
		return true
	})
	return sessions
}

// IsRunning 检查是否运行中
func (p *Platform) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}
