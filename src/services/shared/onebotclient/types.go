package onebotclient

import (
	"encoding/json"
	"strings"
)

// OneBot11 消息段
type MessageSegment struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// 文本消息段 data
type TextData struct {
	Text string `json:"text"`
}

// At 消息段 data
type AtData struct {
	QQ string `json:"qq"`
}

// Reply 消息段 data
type ReplyData struct {
	ID string `json:"id"`
}

// Image 消息段 data
type ImageData struct {
	File string `json:"file,omitempty"`
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

// 事件基础字段
type Event struct {
	Time        int64           `json:"time"`
	SelfID      json.Number     `json:"self_id"`
	PostType    string          `json:"post_type"`
	MessageType string          `json:"message_type,omitempty"`
	SubType     string          `json:"sub_type,omitempty"`
	MessageID   json.Number     `json:"message_id,omitempty"`
	UserID      json.Number     `json:"user_id,omitempty"`
	GroupID     json.Number     `json:"group_id,omitempty"`
	RawMessage  string          `json:"raw_message,omitempty"`
	Message     json.RawMessage `json:"message,omitempty"`
	Sender      *Sender         `json:"sender,omitempty"`
	NoticeType  string          `json:"notice_type,omitempty"`
	RequestType string          `json:"request_type,omitempty"`
	MetaEvent   string          `json:"meta_event_type,omitempty"`

	// 群通知字段
	OperatorID json.Number `json:"operator_id,omitempty"`
	Comment    string      `json:"comment,omitempty"`
	Flag       string      `json:"flag,omitempty"`
}

type Sender struct {
	UserID   json.Number `json:"user_id,omitempty"`
	Nickname string      `json:"nickname,omitempty"`
	Card     string      `json:"card,omitempty"`
	Role     string      `json:"role,omitempty"`
}

// IsMessageEvent 判断是否为消息事件
func (e *Event) IsMessageEvent() bool {
	return e.PostType == "message"
}

// IsPrivateMessage 私聊消息
func (e *Event) IsPrivateMessage() bool {
	return e.PostType == "message" && e.MessageType == "private"
}

// IsGroupMessage 群聊消息
func (e *Event) IsGroupMessage() bool {
	return e.PostType == "message" && e.MessageType == "group"
}

// IsHeartbeat 心跳事件
func (e *Event) IsHeartbeat() bool {
	return e.PostType == "meta_event" && e.MetaEvent == "heartbeat"
}

// IsLifecycle 生命周期事件
func (e *Event) IsLifecycle() bool {
	return e.PostType == "meta_event" && e.MetaEvent == "lifecycle"
}

// IsNoticeEvent 通知事件（群撤回、群禁言等）
func (e *Event) IsNoticeEvent() bool {
	return e.PostType == "notice"
}

// IsGroupRecall 群消息撤回通知
func (e *Event) IsGroupRecall() bool {
	return e.PostType == "notice" && e.NoticeType == "group_recall"
}

// ForwardMessages 提取 forward 消息段中的 ID（NapCat 合并转发）
func (e *Event) ForwardMessages() []string {
	var ids []string
	for _, seg := range e.ParsedSegments() {
		if seg.Type != "forward" {
			continue
		}
		var fd ForwardData
		if err := json.Unmarshal(seg.Data, &fd); err != nil {
			continue
		}
		if id := strings.TrimSpace(fd.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// PlainText 从 message 字段提取纯文本（过滤 CQ 码，只保留 text 段）
func (e *Event) PlainText() string {
	// 优先解析结构化 message 数组
	if len(e.Message) > 0 {
		var segments []MessageSegment
		if err := json.Unmarshal(e.Message, &segments); err == nil && len(segments) > 0 {
			var buf []byte
			for _, seg := range segments {
				if seg.Type != "text" {
					continue
				}
				var td TextData
				if err := json.Unmarshal(seg.Data, &td); err != nil {
					continue
				}
				buf = append(buf, td.Text...)
			}
			return string(buf)
		}
	}
	return e.RawMessage
}

// SenderDisplayName 返回发送者展示名（优先群名片）
func (e *Event) SenderDisplayName() string {
	if e.Sender == nil {
		return ""
	}
	if e.Sender.Card != "" {
		return e.Sender.Card
	}
	return e.Sender.Nickname
}

// --- send_msg 请求/响应 ---

type SendMsgRequest struct {
	MessageType string           `json:"message_type"`
	UserID      string           `json:"user_id,omitempty"`
	GroupID     string           `json:"group_id,omitempty"`
	Message     []MessageSegment `json:"message"`
}

type SendMsgResponse struct {
	MessageID json.Number `json:"message_id"`
}

// --- get_login_info 响应 ---

type LoginInfo struct {
	UserID   json.Number `json:"user_id"`
	Nickname string      `json:"nickname"`
}

// --- get_msg 响应 ---

type GetMsgResponse struct {
	MessageID   json.Number      `json:"message_id"`
	RealID      json.Number      `json:"real_id,omitempty"`
	MessageType string           `json:"message_type"`
	Sender      *Sender          `json:"sender,omitempty"`
	Time        int64            `json:"time"`
	Message     []MessageSegment `json:"message,omitempty"`
	RawMessage  string           `json:"raw_message,omitempty"`
}

// --- get_group_member_info 响应 ---

type GroupMemberInfo struct {
	GroupID  json.Number `json:"group_id"`
	UserID   json.Number `json:"user_id"`
	Nickname string      `json:"nickname"`
	Card     string      `json:"card,omitempty"`
	Role     string      `json:"role"` // owner / admin / member
	Title    string      `json:"title,omitempty"`
}

// --- get_group_info 响应 ---

type GroupInfo struct {
	GroupID   json.Number `json:"group_id"`
	GroupName string      `json:"group_name"`
}

// RecordData 语音消息段 data
type RecordData struct {
	File string `json:"file"`
}

// VideoData 视频消息段 data
type VideoData struct {
	File string `json:"file"`
}

// ForwardData 合并转发消息段 data
type ForwardData struct {
	ID string `json:"id,omitempty"`
}

// TextSegments 将纯文本构造为消息段数组
func TextSegments(text string) []MessageSegment {
	data, _ := json.Marshal(TextData{Text: text})
	return []MessageSegment{{Type: "text", Data: data}}
}

// ReplySegment 构造引用回复消息段
func ReplySegment(messageID string) MessageSegment {
	data, _ := json.Marshal(ReplyData{ID: messageID})
	return MessageSegment{Type: "reply", Data: data}
}

// ImageSegment 构造图片消息段（file 支持 http:// / file:/// / base64:// ）
func ImageSegment(file string) MessageSegment {
	data, _ := json.Marshal(ImageData{File: file})
	return MessageSegment{Type: "image", Data: data}
}

// RecordSegment 构造语音消息段
func RecordSegment(file string) MessageSegment {
	data, _ := json.Marshal(RecordData{File: file})
	return MessageSegment{Type: "record", Data: data}
}

// VideoSegment 构造视频消息段
func VideoSegment(file string) MessageSegment {
	data, _ := json.Marshal(VideoData{File: file})
	return MessageSegment{Type: "video", Data: data}
}

// ParsedSegments 解析 message 字段为消息段数组
func (e *Event) ParsedSegments() []MessageSegment {
	if len(e.Message) == 0 {
		return nil
	}
	var segments []MessageSegment
	if err := json.Unmarshal(e.Message, &segments); err != nil {
		return nil
	}
	return segments
}

// MentionsQQ 检测消息中是否 @ 了指定 QQ 号
func (e *Event) MentionsQQ(selfID string) bool {
	if selfID == "" {
		return false
	}
	for _, seg := range e.ParsedSegments() {
		if seg.Type != "at" {
			continue
		}
		var ad AtData
		if err := json.Unmarshal(seg.Data, &ad); err != nil {
			continue
		}
		if ad.QQ == selfID || ad.QQ == "all" {
			return true
		}
	}
	return false
}

// IsReplyToMessage 检测消息中是否包含 reply 段
func (e *Event) IsReplyToMessage() bool {
	for _, seg := range e.ParsedSegments() {
		if seg.Type == "reply" {
			return true
		}
	}
	return false
}

// ReplyMessageID 从 reply 段提取被回复消息的 ID
func (e *Event) ReplyMessageID() string {
	for _, seg := range e.ParsedSegments() {
		if seg.Type != "reply" {
			continue
		}
		var rd ReplyData
		if err := json.Unmarshal(seg.Data, &rd); err != nil {
			continue
		}
		return rd.ID
	}
	return ""
}

// ImageURLs 从 message 字段提取所有 image 段的下载 URL
func (e *Event) ImageURLs() []string {
	var urls []string
	for _, seg := range e.ParsedSegments() {
		if seg.Type != "image" {
			continue
		}
		var id ImageData
		if err := json.Unmarshal(seg.Data, &id); err != nil {
			continue
		}
		if u := strings.TrimSpace(id.URL); u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}
