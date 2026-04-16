package weibo

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// MessageType 消息类型
type MessageType int

const (
	MessageTypeText  MessageType = iota // 文本消息
	MessageTypeImage                    // 图片消息
	MessageTypeFile                     // 文件消息
)

// Message 内部消息格式 (与 cc-connect 交互)
type Message struct {
	Platform   string    // 平台名称: weibo
	SessionID  string    // 会话ID: weibo:dm:{userId}
	UserID     string    // 用户ID
	Content    string    // 消息内容
	MsgType    MessageType // 消息类型
	MediaURL   string    // 媒体URL (图片/文件)
	MediaData  []byte    // 媒体数据 (base64解码后)
	Filename   string    // 文件名
	Timestamp  time.Time // 时间戳
	MsgID      string    // 消息ID
}

// ParseWeiboMessage 解析微博消息为内部格式
func ParseWeiboMessage(msg WeiboMessage) Message {
	m := Message{
		Platform:  PlatformType,
		SessionID: fmt.Sprintf("weibo:dm:%s", msg.Payload.FromUserID),
		UserID:    msg.Payload.FromUserID,
		Content:   msg.Payload.Text,
		MsgType:   MessageTypeText,
		Timestamp: time.Now(),
		MsgID:     msg.Payload.MessageID,
	}

	if msg.Payload.Timestamp > 0 {
		m.Timestamp = time.Unix(msg.Payload.Timestamp/1000, 0)
	}

	// 解析多媒体内容
	if len(msg.Payload.Input) > 0 {
		m.Content, m.MediaData, m.Filename = parseInput(msg.Payload.Input)
		if len(m.MediaData) > 0 {
			m.MsgType = MessageTypeImage // 默认当作图片处理
		}
	}

	return m
}

// parseInput 解析 input 字段中的多媒体内容
func parseInput(input []any) (text string, mediaData []byte, filename string) {
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

			case "input_image", "input_file":
				source, _ := partMap["source"].(map[string]interface{})
				if source != nil {
					dataType, _ := source["type"].(string)
					if dataType == "base64" {
						dataStr, _ := source["data"].(string)
						if dataStr != "" {
							mediaData, _ = base64.StdEncoding.DecodeString(dataStr)
						}
					}
				}
				filename, _ = partMap["filename"].(string)
			}
		}
	}

	return
}

// BuildSendPayload 构建发送消息载荷
func BuildSendPayload(toUserID, text string, chunkID int, done bool) WebSocketMessage {
	return WebSocketMessage{
		Type: "send_message",
		Payload: SendMessagePayload{
			ToUserID:  toUserID,
			Text:      text,
			MessageID: generateMessageID(),
			ChunkID:   chunkID,
			Done:      done,
		},
	}
}

// ExtractUserIDFromSession 从 SessionID 提取用户ID
func ExtractUserIDFromSession(sessionID string) string {
	return strings.TrimPrefix(sessionID, "weibo:dm:")
}

// IsWeiboSession 判断是否为微博会话
func IsWeiboSession(sessionID string) bool {
	return strings.HasPrefix(sessionID, "weibo:dm:")
}
