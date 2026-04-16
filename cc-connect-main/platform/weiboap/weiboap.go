package weiboap

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
)

func init() {
	core.RegisterPlatform("weiboap", New)
}

const (
	sessionKeyPrefix = "weiboap:dm:"
	maxWeiboAPChunk  = 4000

	defaultAPIBaseURL   = "http://127.0.0.1:9527"
	defaultPollInterval = 2 * time.Second
)

type replyContext struct {
	peerUserID string
}

type Platform struct {
	apiBaseURL   string
	apiToken     string
	allowFrom    string
	stateDir     string
	accountLabel string
	pollInterval time.Duration
	httpClient   *http.Client
	mu           sync.RWMutex
	handler      core.MessageHandler
	cancel       context.CancelFunc
	stopping     bool
	dedupMu      sync.Mutex
	dedup        map[string]time.Time
	tokensMu     sync.RWMutex
	tokens       map[string]string
	tokensPath   string
	lastMessageID int64
}

func New(opts map[string]any) (core.Platform, error) {
	apiBaseURL, _ := opts["api_base_url"].(string)
	if strings.TrimSpace(apiBaseURL) == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	apiToken, _ := opts["api_token"].(string)
	allowFrom, _ := opts["allow_from"].(string)
	core.CheckAllowFrom("weiboap", allowFrom)
	accountLabel, _ := opts["account_id"].(string)
	if accountLabel == "" {
		accountLabel = "default"
	}
	dataDir, _ := opts["cc_data_dir"].(string)
	project, _ := opts["cc_project"].(string)
	stateDir := ""
	if dataDir != "" && project != "" {
		stateDir = filepath.Join(dataDir, "weiboap", project, accountLabel)
	}
	if override, _ := opts["state_dir"].(string); override != "" {
		stateDir = override
	}
	pollInterval := defaultPollInterval
	if pi, ok := opts["poll_interval"].(string); ok && pi != "" {
		if d, err := time.ParseDuration(pi); err == nil {
			pollInterval = d
		}
	}
	p := &Platform{
		apiBaseURL:   strings.TrimRight(strings.TrimSpace(apiBaseURL), "/"),
		apiToken:     strings.TrimSpace(apiToken),
		allowFrom:    strings.TrimSpace(allowFrom),
		stateDir:     stateDir,
		accountLabel: accountLabel,
		pollInterval: pollInterval,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		tokens:       make(map[string]string),
		dedup:        make(map[string]time.Time),
	}
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return nil, fmt.Errorf("weiboap: create state dir: %w", err)
		}
		p.tokensPath = filepath.Join(stateDir, "context_tokens.json")
		p.loadTokens()
	}
	return p, nil
}

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

func (p *Platform) Name() string { return "weiboap" }

func (p *Platform) Start(handler core.MessageHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping {
		return fmt.Errorf("weiboap: platform stopped")
	}
	p.handler = handler
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.pollLoop(ctx)
	slog.Info("weiboap: platform started", "account", p.accountLabel, "api_base_url", p.apiBaseURL)
	return nil
}

func (p *Platform) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.fetchMessages(ctx)
		}
	}
}

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type messageItem struct {
	MessageID  string `json:"message_id"`
	FromUserID string `json:"from_user_id"`
	ToUserID   string `json:"to_user_id"`
	Text       string `json:"text"`
	Timestamp  int64  `json:"timestamp"`
	Type       string `json:"type"`
}

type messagesResponse struct {
	Messages []messageItem `json:"messages"`
	HasMore  bool          `json:"has_more"`
}

func (p *Platform) fetchMessages(ctx context.Context) {
	url := fmt.Sprintf("%s/api/v1/messages?since_id=%d", p.apiBaseURL, p.lastMessageID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		slog.Debug("weiboap: create request failed", "error", err)
		return
	}
	if p.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiToken)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		slog.Debug("weiboap: fetch messages failed", "error", err)
		return
	}
	defer resp.Body.Close()
	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		slog.Debug("weiboap: parse response failed", "error", err)
		return
	}
	if apiResp.Code != 0 {
		slog.Debug("weiboap: api error", "code", apiResp.Code, "message", apiResp.Message)
		return
	}
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return
	}
	var msgResp messagesResponse
	if err := json.Unmarshal(dataBytes, &msgResp); err != nil {
		return
	}
	for i := range msgResp.Messages {
		p.dispatchInbound(ctx, &msgResp.Messages[i])
		if msgResp.Messages[i].Timestamp > p.lastMessageID {
			p.lastMessageID = msgResp.Messages[i].Timestamp
		}
	}
}

func (p *Platform) dispatchInbound(ctx context.Context, msg *messageItem) {
	if msg == nil {
		return
	}
	from := strings.TrimSpace(msg.FromUserID)
	if from == "" {
		return
	}
	if !core.AllowList(p.allowFrom, from) {
		slog.Debug("weiboap: sender not in allow_from", "from", from)
		return
	}
	dedupKey := fmt.Sprintf("%s|%s|%d", from, msg.MessageID, msg.Timestamp)
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
	content := strings.TrimSpace(msg.Text)
	if content == "" {
		return
	}
	p.mu.RLock()
	h := p.handler
	p.mu.RUnlock()
	if h == nil {
		return
	}
	messageID := msg.MessageID
	if messageID == "" {
		messageID = randomHex(8)
	}
	rc := &replyContext{peerUserID: from}
	h(p, &core.Message{
		SessionKey: sessionKeyPrefix + from,
		Platform:   p.Name(),
		MessageID:  messageID,
		UserID:     from,
		UserName:   shortUser(from),
		Content:    content,
		ReplyCtx:   rc,
	})
}

func (p *Platform) Reply(ctx context.Context, replyCtx any, content string) error {
	return p.sendChunks(ctx, replyCtx, content)
}

func (p *Platform) Send(ctx context.Context, replyCtx any, content string) error {
	return p.sendChunks(ctx, replyCtx, content)
}

func (p *Platform) sendChunks(ctx context.Context, replyCtx any, content string) error {
	rc, ok := replyCtx.(*replyContext)
	if !ok || rc == nil {
		return fmt.Errorf("weiboap: invalid reply context")
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	chunks := splitByRune(content, maxWeiboAPChunk)
	for i, chunk := range chunks {
		if err := p.sendMessage(rc.peerUserID, chunk); err != nil {
			return fmt.Errorf("weiboap: send chunk %d: %w", i, err)
		}
	}
	return nil
}

func (p *Platform) sendMessage(toUserID, text string) error {
	payload := map[string]string{
		"to_user_id": toUserID,
		"text":       text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	url := fmt.Sprintf("%s/api/v1/send", p.apiBaseURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiToken)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if apiResp.Code != 0 {
		return fmt.Errorf("api error: %s (code: %d)", apiResp.Message, apiResp.Code)
	}
	return nil
}

func (p *Platform) Stop() error {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.stopping = true
	p.mu.Unlock()
	slog.Info("weiboap: platform stopped", "account", p.accountLabel)
	return nil
}

func (p *Platform) ReconstructReplyCtx(sessionKey string) (any, error) {
	if !strings.HasPrefix(sessionKey, sessionKeyPrefix) {
		return nil, fmt.Errorf("weiboap: not a weiboap session key")
	}
	peer := strings.TrimPrefix(sessionKey, sessionKeyPrefix)
	return &replyContext{peerUserID: peer}, nil
}

func (p *Platform) FormattingInstructions() string {
	return "Replies are delivered as plain text to WeiboAP. Avoid markdown; use short paragraphs."
}

func shortUser(id string) string {
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

var (
	_ core.Platform                     = (*Platform)(nil)
	_ core.ReplyContextReconstructor    = (*Platform)(nil)
	_ core.FormattingInstructionProvider = (*Platform)(nil)
)
