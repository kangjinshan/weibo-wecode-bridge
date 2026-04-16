package weibo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/gorilla/websocket"
)

func init() {
	core.RegisterPlatform("weibo", New)
}

const (
	sessionKeyPrefix = "weibo:dm:"
	maxWeiboChunk    = 4000 // 微博私信字符限制

	defaultTokenURL = "http://open-im.api.weibo.com/open/auth/ws_token"
	defaultWSURL    = "ws://open-im.api.weibo.com/ws/stream"
	pingInterval    = 30 * time.Second
	pongTimeout     = 120 * time.Second
)

// replyContext 用于存储回复所需的上下文
type replyContext struct {
	peerUserID string
}

// Platform 实现 core.Platform 接口
type Platform struct {
	appID          string
	appSecret string
	tokenURL       string
	wsURL          string
	allowFrom      string
	stateDir       string
	accountLabel   string

	// HTTP 客户端
	httpClient *http.Client

	// WebSocket 连接
	conn      *websocket.Conn
	connMutex sync.Mutex

	// Token 缓存
	token       string
	tokenExpire time.Time
	tokenMutex  sync.Mutex

	// 状态
	mu       sync.RWMutex
	handler  core.MessageHandler
	cancel   context.CancelFunc
	stopping bool

	// 消息去重
	dedupMu sync.Mutex
	dedup   map[string]time.Time

	// 会话 token 存储 (用于 Reply)
	tokensMu   sync.RWMutex
	tokens     map[string]string
	tokensPath string
}

// New 创建微博平台实例
func New(opts map[string]any) (core.Platform, error) {
	appID, _ := opts["app_id"].(string)
	appSecret, _ := opts["app_secret"].(string)

	if strings.TrimSpace(appID) == "" {
		return nil, fmt.Errorf("weibo: app_id is required")
	}
	if strings.TrimSpace(appSecret) == "" {
		return nil, fmt.Errorf("weibo: app_secret is required")
	}

	allowFrom, _ := opts["allow_from"].(string)
	core.CheckAllowFrom("weibo", allowFrom)

	tokenURL, _ := opts["token_url"].(string)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}

	wsURL, _ := opts["ws_url"].(string)
	if wsURL == "" {
		wsURL = defaultWSURL
	}

	accountLabel, _ := opts["account_id"].(string)
	if accountLabel == "" {
		accountLabel = "default"
	}

	// 状态目录
	dataDir, _ := opts["cc_data_dir"].(string)
	project, _ := opts["cc_project"].(string)
	stateDir := ""
	if dataDir != "" && project != "" {
		stateDir = filepath.Join(dataDir, "weibo", project, accountLabel)
	}
	if override, _ := opts["state_dir"].(string); override != "" {
		stateDir = override
	}

	p := &Platform{
		appID:          strings.TrimSpace(appID),
		appSecret: strings.TrimSpace(appSecret),
		tokenURL:       strings.TrimSpace(tokenURL),
		wsURL:          strings.TrimSpace(wsURL),
		allowFrom:      strings.TrimSpace(allowFrom),
		stateDir:       stateDir,
		accountLabel:   accountLabel,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		tokens:         make(map[string]string),
		dedup:          make(map[string]time.Time),
	}

	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return nil, fmt.Errorf("weibo: create state dir: %w", err)
		}
		p.tokensPath = filepath.Join(stateDir, "context_tokens.json")
		p.loadTokens()
	}

	return p, nil
}

// loadTokens 加载保存的上下文 tokens
func (p *Platform) loadTokens() {
	if p.tokensPath == "" {
		return
	}
	b, err := os.ReadFile(p.tokensPath)
	if err != nil {
		return
	}
	var m map[string]string
	if json.Unmarshal(b, &m) == nil {
		p.tokensMu.Lock()
		p.tokens = m
		p.tokensMu.Unlock()
	}
}

// persistTokens 持久化 tokens
func (p *Platform) persistTokens() {
	if p.tokensPath == "" {
		return
	}
	p.tokensMu.RLock()
	out, err := json.MarshalIndent(p.tokens, "", "  ")
	p.tokensMu.RUnlock()
	if err != nil {
		return
	}
	os.WriteFile(p.tokensPath, out, 0o600)
}

// Name 返回平台名称
func (p *Platform) Name() string { return "weibo" }

// tokenResponse 微博 Token 响应
type tokenResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Token    string `json:"token"`
		ExpireIn int    `json:"expire_in"`
	} `json:"data"`
}

// fetchToken 获取 Token
func (p *Platform) fetchToken() error {
	payload := map[string]string{
		"app_id":         p.appID,
		"app_secret": p.appSecret,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal token request: %w", err)
	}

	resp, err := p.httpClient.Post(p.tokenURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("parse token response: %w", err)
	}

	if tokenResp.Code != 0 {
		return fmt.Errorf("token error: %s (code: %d)", tokenResp.Message, tokenResp.Code)
	}

	p.tokenMutex.Lock()
	p.token = tokenResp.Data.Token
	p.tokenExpire = time.Now().Add(time.Duration(tokenResp.Data.ExpireIn-60) * time.Second)
	p.tokenMutex.Unlock()

	return nil
}

// getValidToken 获取有效的 Token
func (p *Platform) getValidToken() (string, error) {
	p.tokenMutex.Lock()
	defer p.tokenMutex.Unlock()

	if p.token != "" && time.Now().Before(p.tokenExpire) {
		return p.token, nil
	}

	// 临时解锁以调用 fetchToken
	p.tokenMutex.Unlock()
	err := p.fetchToken()
	p.tokenMutex.Lock()

	if err != nil {
		return "", err
	}

	return p.token, nil
}

// weiboMessage 微博消息格式
type weiboMessage struct {
	Type    string `json:"type"`
	Payload struct {
		MessageID  string `json:"messageId"`
		FromUserID string `json:"fromUserId"`
		Text       string `json:"text"`
		Timestamp  int64  `json:"timestamp"`
		Input      []any  `json:"input,omitempty"`
	} `json:"payload"`
}

// wsMessage WebSocket 消息
type wsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// sendMessagePayload 发送消息载荷
type sendMessagePayload struct {
	ToUserID  string `json:"toUserId"`
	Text      string `json:"text"`
	MessageID string `json:"messageId"`
	ChunkID   int    `json:"chunkId"`
	Done      bool   `json:"done"`
}

// Start 启动平台
func (p *Platform) Start(handler core.MessageHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopping {
		return fmt.Errorf("weibo: platform stopped")
	}

	p.handler = handler
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// 启动连接循环
	go p.connectLoop(ctx)

	slog.Info("weibo: platform started", "account", p.accountLabel)
	return nil
}

// connectLoop 连接循环
func (p *Platform) connectLoop(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		// 获取 Token
		token, err := p.getValidToken()
		if err != nil {
			slog.Warn("weibo: fetch token failed", "error", err, "backoff", backoff)
			time.Sleep(backoff)
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		// 构建 WebSocket URL
		wsURL := fmt.Sprintf("%s?app_id=%s&token=%s", p.wsURL, p.appID, token)

		// 连接 WebSocket
		p.connMutex.Lock()
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			p.connMutex.Unlock()
			slog.Warn("weibo: connect failed", "error", err, "backoff", backoff)
			time.Sleep(backoff)
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		p.conn = conn
		p.connMutex.Unlock()

		slog.Info("weibo: connected", "account", p.accountLabel)

		// 重置 backoff
		backoff = time.Second

		// 启动心跳
		heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
		go p.heartbeatLoop(heartbeatCtx)

		// 读取消息
		p.readLoop(ctx)

		// 停止心跳
		heartbeatCancel()

		// 清理连接
		p.connMutex.Lock()
		if p.conn != nil {
			p.conn.Close()
			p.conn = nil
		}
		p.connMutex.Unlock()

		slog.Warn("weibo: disconnected, reconnecting...", "account", p.accountLabel)
	}
}

// readLoop 读取消息循环
func (p *Platform) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			p.connMutex.Lock()
			conn := p.conn
			p.connMutex.Unlock()

			if conn == nil {
				return
			}

			_, data, err := conn.ReadMessage()
			if err != nil {
				slog.Debug("weibo: read error", "error", err)
				return
			}

			// 处理 pong
			if string(data) == "pong" || string(data) == `{"type":"pong"}` {
				continue
			}

			var msg weiboMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.Type == "pong" {
				continue
			}

			if msg.Type == "message" {
				p.dispatchInbound(ctx, &msg)
			}
		}
	}
}

// heartbeatLoop 心跳循环
func (p *Platform) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.connMutex.Lock()
			conn := p.conn
			p.connMutex.Unlock()

			if conn != nil {
				msg := wsMessage{Type: "ping"}
				if err := conn.WriteJSON(msg); err != nil {
					slog.Debug("weibo: send ping failed", "error", err)
				}
			}
		}
	}
}

// dispatchInbound 处理入站消息
func (p *Platform) dispatchInbound(ctx context.Context, msg *weiboMessage) {
	if msg == nil {
		return
	}

	from := strings.TrimSpace(msg.Payload.FromUserID)
	if from == "" {
		return
	}

	// 检查允许列表
	if !core.AllowList(p.allowFrom, from) {
		slog.Debug("weibo: sender not in allow_from", "from", from)
		return
	}

	// 消息去重
	dedupKey := fmt.Sprintf("%s|%s|%d", from, msg.Payload.MessageID, msg.Payload.Timestamp)
	p.dedupMu.Lock()
	now := time.Now()
	for k, ts := range p.dedup {
		if now.Sub(ts) > 5*time.Minute {
			delete(p.dedup, k)
		}
	}
	if _, ok := p.dedup[dedupKey]; ok {
		p.dedupMu.Unlock()
		return
	}
	p.dedup[dedupKey] = now
	p.dedupMu.Unlock()

	// 解析消息内容
	content := strings.TrimSpace(msg.Payload.Text)

	// 解析多媒体 (如果有)
	var images []core.ImageAttachment
	var files []core.FileAttachment
	if len(msg.Payload.Input) > 0 {
		content, images, files = p.parseInput(msg.Payload.Input)
	}

	if content == "" && len(images) == 0 && len(files) == 0 {
		return
	}

	// 获取 handler
	p.mu.RLock()
	h := p.handler
	p.mu.RUnlock()

	if h == nil {
		return
	}

	// 构造消息
	messageID := msg.Payload.MessageID
	if messageID == "" {
		messageID = randomHex(8)
	}

	rc := &replyContext{peerUserID: from}

	h(p, &core.Message{
		SessionKey: sessionKeyPrefix + from,
		Platform:   p.Name(),
		MessageID:  messageID,
		UserID:     from,
		UserName:   shortWeiboUser(from),
		Content:    content,
		Images:     images,
		Files:      files,
		ReplyCtx:   rc,
	})
}

// parseInput 解析 input 字段
func (p *Platform) parseInput(input []any) (text string, images []core.ImageAttachment, files []core.FileAttachment) {
	for _, item := range input {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemType, _ := itemMap["type"].(string)
		if itemType != "message" {
			continue
		}

		role, _ := itemMap["role"].(string)
		if role != "user" {
			continue
		}

		content, _ := itemMap["content"].([]interface{})
		for _, part := range content {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}

			partType, _ := partMap["type"].(string)
			switch partType {
			case "input_text":
				if t, ok := partMap["text"].(string); ok {
					text = t
				}

			case "input_image":
				source, _ := partMap["source"].(map[string]interface{})
				if source != nil {
					dataType, _ := source["type"].(string)
					if dataType == "base64" {
						dataStr, _ := source["data"].(string)
						mediaType, _ := source["media_type"].(string)
						if dataStr != "" {
							// 解码 base64
							decoded := make([]byte, len(dataStr))
							n, _ := hex.Decode(decoded, []byte(dataStr))
							images = append(images, core.ImageAttachment{
								MimeType: mediaType,
								Data:     decoded[:n],
								FileName: "",
							})
						}
					}
				}

			case "input_file":
				source, _ := partMap["source"].(map[string]interface{})
				if source != nil {
					dataType, _ := source["type"].(string)
					if dataType == "base64" {
						dataStr, _ := source["data"].(string)
						mediaType, _ := source["media_type"].(string)
						filename, _ := partMap["filename"].(string)
						if dataStr != "" {
							decoded := make([]byte, len(dataStr))
							n, _ := hex.Decode(decoded, []byte(dataStr))
							files = append(files, core.FileAttachment{
								MimeType: mediaType,
								Data:     decoded[:n],
								FileName: filename,
							})
						}
					}
				}
			}
		}
	}

	return
}

// Reply 回复消息
func (p *Platform) Reply(ctx context.Context, replyCtx any, content string) error {
	return p.sendChunks(ctx, replyCtx, content)
}

// Send 发送消息
func (p *Platform) Send(ctx context.Context, replyCtx any, content string) error {
	return p.sendChunks(ctx, replyCtx, content)
}

// sendChunks 分块发送消息
func (p *Platform) sendChunks(ctx context.Context, replyCtx any, content string) error {
	rc, ok := replyCtx.(*replyContext)
	if !ok || rc == nil {
		return fmt.Errorf("weibo: invalid reply context")
	}

	if strings.TrimSpace(content) == "" {
		return nil
	}

	// 分块发送
	chunks := splitByRune(content, maxWeiboChunk)
	for i, chunk := range chunks {
		done := i == len(chunks)-1
		if err := p.sendChunk(rc.peerUserID, chunk, i, done); err != nil {
			return fmt.Errorf("weibo: send chunk %d: %w", i, err)
		}
	}

	return nil
}

// sendChunk 发送单个分块
func (p *Platform) sendChunk(toUserID, text string, chunkID int, done bool) error {
	p.connMutex.Lock()
	conn := p.conn
	p.connMutex.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := wsMessage{
		Type: "send_message",
		Payload: sendMessagePayload{
			ToUserID:  toUserID,
			Text:      text,
			MessageID: generateMessageID(),
			ChunkID:   chunkID,
			Done:      done,
		},
	}

	return conn.WriteJSON(msg)
}

// Stop 停止平台
func (p *Platform) Stop() error {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.stopping = true
	p.mu.Unlock()

	p.connMutex.Lock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	p.connMutex.Unlock()

	slog.Info("weibo: platform stopped", "account", p.accountLabel)
	return nil
}

// ReconstructReplyCtx 重建回复上下文 (用于 cron 任务)
func (p *Platform) ReconstructReplyCtx(sessionKey string) (any, error) {
	if !strings.HasPrefix(sessionKey, sessionKeyPrefix) {
		return nil, fmt.Errorf("weibo: not a weibo session key")
	}
	peer := strings.TrimPrefix(sessionKey, sessionKeyPrefix)
	return &replyContext{peerUserID: peer}, nil
}

// FormattingInstructions 返回格式化说明
func (p *Platform) FormattingInstructions() string {
	return "Replies are delivered as plain text to Weibo DM. Avoid markdown; use short paragraphs."
}

// 辅助函数

func shortWeiboUser(id string) string {
	if len(id) > 32 {
		return id[:32] + "…"
	}
	return id
}

func randomHex(n int) string {
	b := make([]byte, n)
	h := sha1.New()
	h.Write([]byte(time.Now().String()))
	copy(b, h.Sum(nil)[:n])
	return hex.EncodeToString(b)[:n*2]
}

func generateMessageID() string {
	return fmt.Sprintf("msg_%d_%s", time.Now().UnixNano(), randomHex(4))
}

func splitByRune(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{s}
	}

	runes := []rune(s)
	if len(runes) <= maxRunes {
		return []string{s}
	}

	var chunks []string
	for len(runes) > 0 {
		n := maxRunes
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

// 确保实现接口
var (
	_ core.Platform                  = (*Platform)(nil)
	_ core.ReplyContextReconstructor = (*Platform)(nil)
	_ core.FormattingInstructionProvider = (*Platform)(nil)
)
