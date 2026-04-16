package weibo

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	DefaultTokenURL = "http://open-im.api.weibo.com/open/auth/ws_token"
	DefaultWSURL    = "ws://open-im.api.weibo.com/ws/stream"
	PingInterval    = 30 * time.Second
	PongTimeout     = 120 * time.Second
)

// TokenResponse 微博 Token 响应
type TokenResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Token    string `json:"token"`
		ExpireIn int    `json:"expire_in"`
	} `json:"data"`
}

// WeiboMessage 微博消息
type WeiboMessage struct {
	Type    string `json:"type"`
	Payload struct {
		MessageID  string `json:"messageId"`
		FromUserID string `json:"fromUserId"`
		Text       string `json:"text"`
		Timestamp  int64  `json:"timestamp"`
		Input      []any  `json:"input,omitempty"`
	} `json:"payload"`
}

// SendMessagePayload 发送消息载荷
type SendMessagePayload struct {
	ToUserID  string `json:"toUserId"`
	Text      string `json:"text"`
	MessageID string `json:"messageId"`
	ChunkID   int    `json:"chunkId"`
	Done      bool   `json:"done"`
}

// WebSocketMessage WebSocket 消息
type WebSocketMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// Client 微博客户端
type Client struct {
	appID        string
	appSecret string
	tokenURL     string
	wsURL        string

	conn         *websocket.Conn
	connMutex    sync.Mutex
	token        string
	tokenExpire  time.Time

	messageChan  chan WeiboMessage
	stopChan     chan struct{}

	onMessage    func(msg WeiboMessage)
	onConnect    func()
	onDisconnect func(err error)
}

// NewClient 创建微博客户端
func NewClient(appID, appSecret, tokenURL, wsURL string) *Client {
	if tokenURL == "" {
		tokenURL = DefaultTokenURL
	}
	if wsURL == "" {
		wsURL = DefaultWSURL
	}

	return &Client{
		appID:          appID,
		appSecret: appSecret,
		tokenURL:       tokenURL,
		wsURL:          wsURL,
		messageChan:    make(chan WeiboMessage, 100),
		stopChan:       make(chan struct{}),
	}
}

// OnMessage 设置消息回调
func (c *Client) OnMessage(handler func(msg WeiboMessage)) {
	c.onMessage = handler
}

// OnConnect 设置连接成功回调
func (c *Client) OnConnect(handler func()) {
	c.onConnect = handler
}

// OnDisconnect 设置断开连接回调
func (c *Client) OnDisconnect(handler func(err error)) {
	c.onDisconnect = handler
}

// fetchToken 获取 Token
func (c *Client) fetchToken() error {
	payload := map[string]string{
		"app_id":         c.appID,
		"app_secret": c.appSecret,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal token request: %w", err)
	}

	resp, err := http.Post(c.tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return fmt.Errorf("parse token response: %w", err)
	}

	if tokenResp.Code != 0 {
		return fmt.Errorf("token error: %s", tokenResp.Message)
	}

	c.token = tokenResp.Data.Token
	c.tokenExpire = time.Now().Add(time.Duration(tokenResp.Data.ExpireIn-60) * time.Second)

	return nil
}

// getValidToken 获取有效的 Token
func (c *Client) getValidToken() (string, error) {
	if c.token != "" && time.Now().Before(c.tokenExpire) {
		return c.token, nil
	}

	if err := c.fetchToken(); err != nil {
		return "", err
	}

	return c.token, nil
}

// Connect 连接到微博服务器
func (c *Client) Connect() error {
	token, err := c.getValidToken()
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}

	// 构建 WebSocket URL
	url := fmt.Sprintf("%s?app_id=%s&token=%s", c.wsURL, c.appID, token)

	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	c.conn = conn

	// 启动读取协程
	go c.readLoop()

	// 启动心跳协程
	go c.heartbeatLoop()

	// 触发连接回调
	if c.onConnect != nil {
		c.onConnect()
	}

	return nil
}

// readLoop 读取消息循环
func (c *Client) readLoop() {
	for {
		select {
		case <-c.stopChan:
			return
		default:
			c.connMutex.Lock()
			conn := c.conn
			c.connMutex.Unlock()

			if conn == nil {
				time.Sleep(1 * time.Second)
				continue
			}

			_, data, err := conn.ReadMessage()
			if err != nil {
				if c.onDisconnect != nil {
					c.onDisconnect(err)
				}
				// 尝试重连
				go c.reconnect()
				return
			}

			// 处理 pong
			if string(data) == "pong" || string(data) == `{"type":"pong"}` {
				continue
			}

			var msg WeiboMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			// 处理 pong 消息
			if msg.Type == "pong" {
				continue
			}

			// 处理消息
			if msg.Type == "message" && c.onMessage != nil {
				c.onMessage(msg)
			}
		}
	}
}

// heartbeatLoop 心跳循环
func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.connMutex.Lock()
			conn := c.conn
			c.connMutex.Unlock()

			if conn != nil {
				ping := WebSocketMessage{Type: "ping"}
				conn.WriteJSON(ping)
			}
		}
	}
}

// reconnect 重连
func (c *Client) reconnect() {
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-c.stopChan:
			return
		default:
			time.Sleep(backoff)

			if err := c.Connect(); err != nil {
				backoff = backoff * 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			return
		}
	}
}

// generateMessageID 生成消息 ID
func generateMessageID() string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return "msg_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// Send 发送消息
func (c *Client) Send(toUserID, text string) error {
	return c.SendChunk(toUserID, text, 0, true)
}

// SendChunk 分块发送消息
func (c *Client) SendChunk(toUserID, text string, chunkID int, done bool) error {
	c.connMutex.Lock()
	conn := c.conn
	c.connMutex.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := WebSocketMessage{
		Type: "send_message",
		Payload: SendMessagePayload{
			ToUserID:  toUserID,
			Text:      text,
			MessageID: generateMessageID(),
			ChunkID:   chunkID,
			Done:      done,
		},
	}

	return conn.WriteJSON(msg)
}

// Close 关闭连接
func (c *Client) Close() {
	close(c.stopChan)

	c.connMutex.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMutex.Unlock()
}
