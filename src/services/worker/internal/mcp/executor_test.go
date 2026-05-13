package mcp

import (
	"encoding/base64"
	"testing"
)

func TestSplitMCPContentExtractsImages(t *testing.T) {
	imageData := []byte("png-bytes")
	content := []map[string]any{
		{"type": "text", "text": "snapshot"},
		{
			"type":     "image",
			"mimeType": "image/png",
			"data":     base64.StdEncoding.EncodeToString(imageData),
		},
	}

	cleaned, attachments := splitMCPContent(content)
	if len(attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(attachments))
	}
	if attachments[0].MimeType != "image/png" || string(attachments[0].Data) != string(imageData) {
		t.Fatalf("unexpected attachment: %#v", attachments[0])
	}
	if cleaned[1]["data"] != nil {
		t.Fatalf("image data should not be replayed in JSON: %#v", cleaned[1])
	}
	if cleaned[1]["attached"] != true {
		t.Fatalf("expected attached marker: %#v", cleaned[1])
	}
}

func TestSplitMCPContentKeepsInvalidImageOutOfAttachments(t *testing.T) {
	cleaned, attachments := splitMCPContent([]map[string]any{
		{"type": "image", "mime_type": "image/png", "data": "not-base64"},
	})
	if len(attachments) != 0 {
		t.Fatalf("expected no attachments, got %d", len(attachments))
	}
	if cleaned[0]["error"] != "invalid_image_data" {
		t.Fatalf("expected invalid image marker: %#v", cleaned[0])
	}
}
