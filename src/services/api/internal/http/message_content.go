package http

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"arkloop/services/shared/messagecontent"
)

const (
	maxUserMessageTextRunes        = 20000
	maxUserMessageProjectionRunes  = 20000
	maxMessageAttachmentCount      = 8
	maxMessageAttachmentTotalBytes = 20 << 20
	maxImageAttachmentBytes        = 10 << 20
	maxTextAttachmentBytes         = 1 << 20
	maxTextAttachmentRunes         = 12000
	maxMessageAttachmentTextRunes  = 40000
)

var supportedImageMIMEs = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
}

var supportedTextExtensions = map[string]struct{}{
	".txt": {}, ".md": {}, ".json": {}, ".csv": {}, ".xml": {},
	".yaml": {}, ".yml": {}, ".js": {}, ".ts": {}, ".tsx": {},
	".jsx": {}, ".py": {}, ".go": {}, ".java": {}, ".rs": {},
	".sh": {}, ".sql": {},
}

type createMessageRequest struct {
	Content     string          `json:"content"`
	ContentJSON json.RawMessage `json:"content_json"`
}

type messageResponse struct {
	ID              string          `json:"id"`
	OrgID           string          `json:"org_id"`
	ThreadID        string          `json:"thread_id"`
	CreatedByUserID *string         `json:"created_by_user_id"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	ContentJSON     json.RawMessage `json:"content_json,omitempty"`
	CreatedAt       string          `json:"created_at"`
}

type messageAttachmentUploadResponse struct {
	Key           string `json:"key"`
	Filename      string `json:"filename"`
	MimeType      string `json:"mime_type"`
	Size          int64  `json:"size"`
	Kind          string `json:"kind"`
	ExtractedText string `json:"extracted_text,omitempty"`
}

func normalizeCreateMessagePayload(body createMessageRequest) (messagecontent.Content, string, json.RawMessage, error) {
	if len(body.ContentJSON) == 0 {
		if strings.TrimSpace(body.Content) == "" {
			return messagecontent.Content{}, "", nil, fmt.Errorf("content or content_json is required")
		}
		content, err := messagecontent.Normalize(messagecontent.FromText(body.Content).Parts)
		if err != nil {
			return messagecontent.Content{}, "", nil, err
		}
		return finalizeMessageContent(content)
	}

	parsed, err := messagecontent.Parse(body.ContentJSON)
	if err != nil {
		return messagecontent.Content{}, "", nil, err
	}
	content, err := messagecontent.Normalize(parsed.Parts)
	if err != nil {
		return messagecontent.Content{}, "", nil, err
	}
	return finalizeMessageContent(content)
}

func normalizeEditedMessagePayload(existingContentJSON json.RawMessage, body createMessageRequest) (messagecontent.Content, string, json.RawMessage, error) {
	if len(body.ContentJSON) > 0 {
		return normalizeCreateMessagePayload(body)
	}
	if strings.TrimSpace(body.Content) == "" {
		return messagecontent.Content{}, "", nil, fmt.Errorf("content or content_json is required")
	}
	if len(existingContentJSON) == 0 {
		content, err := messagecontent.Normalize(messagecontent.FromText(body.Content).Parts)
		if err != nil {
			return messagecontent.Content{}, "", nil, err
		}
		return finalizeMessageContent(content)
	}
	parsed, err := messagecontent.Parse(existingContentJSON)
	if err != nil {
		content, nErr := messagecontent.Normalize(messagecontent.FromText(body.Content).Parts)
		if nErr != nil {
			return messagecontent.Content{}, "", nil, nErr
		}
		return finalizeMessageContent(content)
	}
	updated, err := messagecontent.ReplaceText(parsed, body.Content)
	if err != nil {
		return messagecontent.Content{}, "", nil, err
	}
	return finalizeMessageContent(updated)
}

func finalizeMessageContent(content messagecontent.Content) (messagecontent.Content, string, json.RawMessage, error) {
	if err := validateMessageContent(content); err != nil {
		return messagecontent.Content{}, "", nil, err
	}
	projection := messagecontent.Projection(content, maxUserMessageProjectionRunes)
	if strings.TrimSpace(projection) == "" {
		return messagecontent.Content{}, "", nil, fmt.Errorf("message content must not be empty")
	}
	raw, err := content.JSON()
	if err != nil {
		return messagecontent.Content{}, "", nil, err
	}
	return content, projection, raw, nil
}

func validateMessageContent(content messagecontent.Content) error {
	if len(content.Parts) == 0 {
		return fmt.Errorf("message content must not be empty")
	}
	attachmentCount := 0
	totalBytes := int64(0)
	totalExtractedRunes := 0
	textRunes := 0
	for _, part := range content.Parts {
		switch strings.TrimSpace(part.Type) {
		case messagecontent.PartTypeText:
			textRunes += utf8.RuneCountInString(part.Text)
		case messagecontent.PartTypeImage:
			attachmentCount++
			if part.Attachment == nil {
				return fmt.Errorf("image attachment is required")
			}
			if _, ok := supportedImageMIMEs[strings.TrimSpace(part.Attachment.MimeType)]; !ok {
				return fmt.Errorf("unsupported image mime type")
			}
			if part.Attachment.Size > maxImageAttachmentBytes {
				return fmt.Errorf("image attachment too large")
			}
			totalBytes += part.Attachment.Size
		case messagecontent.PartTypeFile:
			attachmentCount++
			if part.Attachment == nil {
				return fmt.Errorf("file attachment is required")
			}
			if !isSupportedTextAttachment(part.Attachment.Filename, part.Attachment.MimeType) {
				return fmt.Errorf("unsupported file attachment type")
			}
			if part.Attachment.Size > maxTextAttachmentBytes {
				return fmt.Errorf("text attachment too large")
			}
			totalBytes += part.Attachment.Size
			totalExtractedRunes += utf8.RuneCountInString(part.ExtractedText)
		default:
			return fmt.Errorf("unsupported content part type")
		}
	}
	if textRunes > maxUserMessageTextRunes {
		return fmt.Errorf("message text too long")
	}
	if attachmentCount > maxMessageAttachmentCount {
		return fmt.Errorf("too many attachments")
	}
	if totalBytes > maxMessageAttachmentTotalBytes {
		return fmt.Errorf("attachments too large")
	}
	if totalExtractedRunes > maxMessageAttachmentTextRunes {
		return fmt.Errorf("attachment text too long")
	}
	return nil
}

func trimToRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func isSupportedTextAttachment(filename string, mimeType string) bool {
	if strings.HasPrefix(strings.TrimSpace(mimeType), "text/") {
		return true
	}
	_, ok := supportedTextExtensions[strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))]
	return ok
}
