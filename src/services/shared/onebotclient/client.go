package onebotclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client 是 OneBot11 HTTP API 客户端
type Client struct {
	baseURL    string
	token      string
	httpClient HTTPClient
}

func NewClient(baseURL string, token string, httpClient HTTPClient) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    base,
		token:      strings.TrimSpace(token),
		httpClient: httpClient,
	}
}

// callJSON 通用 POST 请求
func (c *Client) callJSON(ctx context.Context, action string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("onebot marshal %s: %w", action, err)
	}
	url := c.baseURL + "/" + action
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("onebot request %s: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("onebot call %s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("onebot read %s: %w", action, err)
	}

	var envelope struct {
		Status  string          `json:"status"`
		RetCode int             `json:"retcode"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message,omitempty"`
		Wording string          `json:"wording,omitempty"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("onebot decode %s: %w (body: %s)", action, err, string(respBody))
	}
	if envelope.RetCode != 0 {
		msg := envelope.Message
		if msg == "" {
			msg = envelope.Wording
		}
		return fmt.Errorf("onebot %s failed: retcode=%d msg=%s", action, envelope.RetCode, msg)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("onebot decode %s data: %w", action, err)
		}
	}
	return nil
}

// SendPrivateMsg 发送私聊消息
func (c *Client) SendPrivateMsg(ctx context.Context, userID string, message []MessageSegment) (*SendMsgResponse, error) {
	req := map[string]any{
		"user_id": userID,
		"message": message,
	}
	var resp SendMsgResponse
	if err := c.callJSON(ctx, "send_private_msg", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendGroupMsg 发送群聊消息
func (c *Client) SendGroupMsg(ctx context.Context, groupID string, message []MessageSegment) (*SendMsgResponse, error) {
	req := map[string]any{
		"group_id": groupID,
		"message":  message,
	}
	var resp SendMsgResponse
	if err := c.callJSON(ctx, "send_group_msg", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendMsg 通用发送（自动区分私聊/群聊）
func (c *Client) SendMsg(ctx context.Context, messageType, userID, groupID string, message []MessageSegment) (*SendMsgResponse, error) {
	req := map[string]any{
		"message_type": messageType,
		"message":      message,
	}
	if userID != "" {
		req["user_id"] = userID
	}
	if groupID != "" {
		req["group_id"] = groupID
	}
	var resp SendMsgResponse
	if err := c.callJSON(ctx, "send_msg", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetLoginInfo 获取当前登录的 QQ 号和昵称
func (c *Client) GetLoginInfo(ctx context.Context) (*LoginInfo, error) {
	var info LoginInfo
	if err := c.callJSON(ctx, "get_login_info", struct{}{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetMsg 获取消息详情
func (c *Client) GetMsg(ctx context.Context, messageID string) (*GetMsgResponse, error) {
	req := map[string]any{"message_id": messageID}
	var resp GetMsgResponse
	if err := c.callJSON(ctx, "get_msg", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetGroupMemberInfo 获取群成员信息（含 role: owner/admin/member）
func (c *Client) GetGroupMemberInfo(ctx context.Context, groupID, userID string) (*GroupMemberInfo, error) {
	req := map[string]any{
		"group_id": groupID,
		"user_id":  userID,
		"no_cache": false,
	}
	var info GroupMemberInfo
	if err := c.callJSON(ctx, "get_group_member_info", req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// DownloadURL 通过 HTTP GET 下载指定 URL 的内容（用于获取图片字节）。
// maxBytes 限制最大读取量，防止过大响应。
func (c *Client) DownloadURL(ctx context.Context, url string, maxBytes int64) (data []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("onebot download request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("onebot download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("onebot download: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("onebot download read: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("onebot download: response too large")
	}
	ct := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	return body, ct, nil
}

// SetMsgEmojiLike 对消息添加表情反应（NapCat 扩展 API）
func (c *Client) SetMsgEmojiLike(ctx context.Context, messageID string, emojiID string) error {
	req := map[string]any{
		"message_id": messageID,
		"emoji_id":   emojiID,
	}
	return c.callJSON(ctx, "set_msg_emoji_like", req, nil)
}

// DeleteMsg 撤回消息
func (c *Client) DeleteMsg(ctx context.Context, messageID string) error {
	req := map[string]any{"message_id": messageID}
	return c.callJSON(ctx, "delete_msg", req, nil)
}

// UploadPrivateFile 私聊上传文件（NapCat 扩展 API）
func (c *Client) UploadPrivateFile(ctx context.Context, userID, file, name string) error {
	req := map[string]any{
		"user_id": userID,
		"file":    file,
		"name":    name,
	}
	return c.callJSON(ctx, "upload_private_file", req, nil)
}

// UploadGroupFile 群聊上传文件（NapCat 扩展 API）
func (c *Client) UploadGroupFile(ctx context.Context, groupID, file, name string) error {
	req := map[string]any{
		"group_id": groupID,
		"file":     file,
		"name":     name,
	}
	return c.callJSON(ctx, "upload_group_file", req, nil)
}

// GetGroupInfo 获取群信息
func (c *Client) GetGroupInfo(ctx context.Context, groupID string) (*GroupInfo, error) {
	req := map[string]any{"group_id": groupID, "no_cache": false}
	var info GroupInfo
	if err := c.callJSON(ctx, "get_group_info", req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
