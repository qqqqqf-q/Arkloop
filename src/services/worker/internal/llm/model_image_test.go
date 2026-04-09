package llm

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
)

const testBannerHeight = 100

func makeVisionTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 220, G: 30, B: 30, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func decodeModelImage(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	return img
}

func decodeDataURLPayload(t *testing.T, dataURL string) image.Image {
	t.Helper()
	idx := strings.Index(dataURL, ",")
	if idx < 0 {
		t.Fatalf("invalid data url: %q", dataURL)
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[idx+1:])
	if err != nil {
		t.Fatalf("decode data url: %v", err)
	}
	return decodeModelImage(t, raw)
}

func decodeInlineDataPayload(t *testing.T, raw string) image.Image {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode inline data: %v", err)
	}
	return decodeModelImage(t, data)
}

func TestPartDataURLAnnotatesAttachmentKeyImage(t *testing.T) {
	part := ContentPart{
		Type: "image",
		Attachment: &messagecontent.AttachmentRef{
			Key:      "attachments/acc/thread/image.jpg",
			Filename: "image.jpg",
			MimeType: "image/png",
		},
		Data: makeVisionTestPNG(t, 320, 180),
	}

	dataURL, err := partDataURL(part)
	if err != nil {
		t.Fatalf("partDataURL failed: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/jpeg;base64,") {
		t.Fatalf("unexpected data url prefix: %q", dataURL[:24])
	}

	img := decodeDataURLPayload(t, dataURL)
	if got := img.Bounds().Dy(); got != 180+testBannerHeight {
		t.Fatalf("unexpected annotated height: %d", got)
	}
}

func TestToAnthropicMessagesAnnotatesUserImage(t *testing.T) {
	_, messages, err := toAnthropicMessages([]Message{
		{
			Role: "user",
			Content: []ContentPart{
				{Type: "text", Text: "看看这个"},
				{
					Type: "image",
					Attachment: &messagecontent.AttachmentRef{
						Key:      "attachments/acc/thread/image.jpg",
						Filename: "image.jpg",
						MimeType: "image/png",
					},
					Data: makeVisionTestPNG(t, 300, 160),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}

	blocks := messages[0]["content"].([]map[string]any)
	if len(blocks) != 3 {
		t.Fatalf("unexpected block count: %#v", blocks)
	}
	source := blocks[2]["source"].(map[string]any)
	if source["media_type"] != "image/jpeg" {
		t.Fatalf("unexpected media type: %#v", source["media_type"])
	}
	img := decodeInlineDataPayload(t, source["data"].(string))
	if got := img.Bounds().Dy(); got != 160+testBannerHeight {
		t.Fatalf("unexpected annotated height: %d", got)
	}
}

func TestToAnthropicMessagesAnnotatesToolResultImage(t *testing.T) {
	_, messages, err := toAnthropicMessages([]Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ToolCallID:    "call_1",
				ToolName:      "read",
				ArgumentsJSON: map[string]any{"source": map[string]any{"kind": "message_attachment"}},
			}},
		},
		{
			Role: "tool",
			Content: []ContentPart{
				{Type: "text", Text: `{"tool_call_id":"call_1","tool_name":"read","result":{"ok":true}}`},
				{
					Type: "image",
					Attachment: &messagecontent.AttachmentRef{
						Key:      "attachments/acc/thread/image.jpg",
						Filename: "image.jpg",
						MimeType: "image/png",
					},
					Data: makeVisionTestPNG(t, 240, 140),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toAnthropicMessages failed: %v", err)
	}

	wrapper := messages[1]["content"].([]map[string]any)[0]
	content := wrapper["content"].([]map[string]any)
	source := content[1]["source"].(map[string]any)
	img := decodeInlineDataPayload(t, source["data"].(string))
	if got := img.Bounds().Dy(); got != 140+testBannerHeight {
		t.Fatalf("unexpected annotated height: %d", got)
	}
}

func TestGeminiUserPartsAnnotatesAttachmentKeyImage(t *testing.T) {
	parts, err := geminiUserParts([]ContentPart{{
		Type: "image",
		Attachment: &messagecontent.AttachmentRef{
			Key:      "attachments/acc/thread/image.jpg",
			Filename: "image.jpg",
			MimeType: "image/png",
		},
		Data: makeVisionTestPNG(t, 260, 150),
	}})
	if err != nil {
		t.Fatalf("geminiUserParts failed: %v", err)
	}

	inline := parts[0]["inlineData"].(map[string]any)
	if inline["mimeType"] != "image/jpeg" {
		t.Fatalf("unexpected mime type: %#v", inline["mimeType"])
	}
	img := decodeInlineDataPayload(t, inline["data"].(string))
	if got := img.Bounds().Dy(); got != 150+testBannerHeight {
		t.Fatalf("unexpected annotated height: %d", got)
	}
}

func TestToGeminiContentsAnnotatesToolImage(t *testing.T) {
	_, contents, err := toGeminiContents([]Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ToolCallID:    "call_1",
				ToolName:      "read",
				ArgumentsJSON: map[string]any{"source": map[string]any{"kind": "message_attachment"}},
			}},
		},
		{
			Role: "tool",
			Content: []ContentPart{
				{Type: "text", Text: `{"tool_call_id":"call_1","tool_name":"read","result":{"ok":true}}`},
				{
					Type: "image",
					Attachment: &messagecontent.AttachmentRef{
						Key:      "attachments/acc/thread/image.jpg",
						Filename: "image.jpg",
						MimeType: "image/png",
					},
					Data: makeVisionTestPNG(t, 200, 120),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toGeminiContents failed: %v", err)
	}

	toolParts := contents[1]["parts"].([]map[string]any)
	inline := toolParts[1]["inlineData"].(map[string]any)
	img := decodeInlineDataPayload(t, inline["data"].(string))
	if got := img.Bounds().Dy(); got != 120+testBannerHeight {
		t.Fatalf("unexpected annotated height: %d", got)
	}
}
