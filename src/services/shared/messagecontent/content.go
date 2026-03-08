package messagecontent

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	PartTypeText  = "text"
	PartTypeImage = "image"
	PartTypeFile  = "file"
)

type AttachmentRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

type Part struct {
	Type          string         `json:"type"`
	Text          string         `json:"text,omitempty"`
	Attachment    *AttachmentRef `json:"attachment,omitempty"`
	ExtractedText string         `json:"extracted_text,omitempty"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

func Parse(raw []byte) (Content, error) {
	if len(raw) == 0 {
		return Content{}, nil
	}
	var content Content
	if err := json.Unmarshal(raw, &content); err != nil {
		return Content{}, err
	}
	return content, nil
}

func (c Content) JSON() ([]byte, error) {
	if len(c.Parts) == 0 {
		return nil, nil
	}
	return json.Marshal(c)
}

func Normalize(parts []Part) (Content, error) {
	if len(parts) == 0 {
		return Content{}, nil
	}

	normalized := make([]Part, 0, len(parts))
	texts := make([]string, 0, len(parts))
	firstTextIndex := -1

	appendText := func(text string) {
		if firstTextIndex == -1 {
			firstTextIndex = len(normalized)
		}
		texts = append(texts, text)
	}

	for _, part := range parts {
		switch cleanType(part.Type, part) {
		case PartTypeText:
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			appendText(part.Text)
		case PartTypeImage:
			cleanAttachment, err := normalizeAttachment(part.Attachment)
			if err != nil {
				return Content{}, err
			}
			normalized = append(normalized, Part{Type: PartTypeImage, Attachment: cleanAttachment})
		case PartTypeFile:
			cleanAttachment, err := normalizeAttachment(part.Attachment)
			if err != nil {
				return Content{}, err
			}
			normalized = append(normalized, Part{
				Type:          PartTypeFile,
				Attachment:    cleanAttachment,
				ExtractedText: strings.TrimSpace(part.ExtractedText),
			})
		default:
			return Content{}, fmt.Errorf("unsupported content part type")
		}
	}

	if len(texts) > 0 {
		textPart := Part{Type: PartTypeText, Text: strings.Join(texts, "\n\n")}
		if firstTextIndex < 0 || firstTextIndex >= len(normalized) {
			normalized = append(normalized, textPart)
		} else {
			normalized = append(normalized, Part{})
			copy(normalized[firstTextIndex+1:], normalized[firstTextIndex:])
			normalized[firstTextIndex] = textPart
		}
	}

	if len(normalized) == 0 {
		return Content{}, nil
	}
	return Content{Parts: normalized}, nil
}

func FromText(text string) Content {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Content{}
	}
	return Content{Parts: []Part{{Type: PartTypeText, Text: text}}}
}

func ReplaceText(content Content, newText string) (Content, error) {
	normalized, err := Normalize(content.Parts)
	if err != nil {
		return Content{}, err
	}

	parts := make([]Part, 0, len(normalized.Parts)+1)
	inserted := false
	for _, part := range normalized.Parts {
		if part.Type == PartTypeText {
			if strings.TrimSpace(newText) == "" {
				continue
			}
			parts = append(parts, Part{Type: PartTypeText, Text: newText})
			inserted = true
			continue
		}
		parts = append(parts, part)
	}
	if !inserted && strings.TrimSpace(newText) != "" {
		parts = append([]Part{{Type: PartTypeText, Text: newText}}, parts...)
	}
	return Normalize(parts)
}

func Projection(content Content, limit int) string {
	if len(content.Parts) == 0 {
		return ""
	}
	blocks := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		switch cleanType(part.Type, part) {
		case PartTypeText:
			if strings.TrimSpace(part.Text) != "" {
				blocks = append(blocks, part.Text)
			}
		case PartTypeImage:
			name := "image"
			if part.Attachment != nil && strings.TrimSpace(part.Attachment.Filename) != "" {
				name = strings.TrimSpace(part.Attachment.Filename)
			}
			blocks = append(blocks, fmt.Sprintf("[图片: %s]", name))
		case PartTypeFile:
			name := "file"
			if part.Attachment != nil && strings.TrimSpace(part.Attachment.Filename) != "" {
				name = strings.TrimSpace(part.Attachment.Filename)
			}
			block := fmt.Sprintf("[附件: %s]", name)
			if strings.TrimSpace(part.ExtractedText) != "" {
				block += "\n" + part.ExtractedText
			}
			blocks = append(blocks, block)
		}
	}
	out := strings.Join(blocks, "\n\n")
	if limit <= 0 || utf8.RuneCountInString(out) <= limit {
		return out
	}
	runes := []rune(out)
	return string(runes[:limit])
}

func PromptText(part Part) string {
	switch cleanType(part.Type, part) {
	case PartTypeText:
		return part.Text
	case PartTypeFile:
		name := "file"
		if part.Attachment != nil && strings.TrimSpace(part.Attachment.Filename) != "" {
			name = strings.TrimSpace(part.Attachment.Filename)
		}
		if strings.TrimSpace(part.ExtractedText) == "" {
			return fmt.Sprintf("附件 %s", name)
		}
		return fmt.Sprintf("附件 %s:\n%s", name, part.ExtractedText)
	default:
		return ""
	}
}

func cleanType(raw string, part Part) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		return trimmed
	}
	if part.Attachment == nil {
		return PartTypeText
	}
	if strings.TrimSpace(part.ExtractedText) != "" {
		return PartTypeFile
	}
	return PartTypeImage
}

func normalizeAttachment(attachment *AttachmentRef) (*AttachmentRef, error) {
	if attachment == nil {
		return nil, fmt.Errorf("attachment is required")
	}
	clean := &AttachmentRef{
		Key:      strings.TrimSpace(attachment.Key),
		Filename: strings.TrimSpace(attachment.Filename),
		MimeType: strings.TrimSpace(attachment.MimeType),
		Size:     attachment.Size,
	}
	if clean.Key == "" || clean.Filename == "" || clean.MimeType == "" {
		return nil, fmt.Errorf("attachment fields are required")
	}
	if clean.Size < 0 {
		clean.Size = 0
	}
	return clean, nil
}
