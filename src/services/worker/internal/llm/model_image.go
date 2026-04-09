package llm

import (
	"encoding/base64"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/imageutil"
)

func modelInputImage(part ContentPart) (string, []byte, error) {
	if part.Attachment == nil {
		return "", nil, fmt.Errorf("image attachment is required")
	}
	if len(part.Data) == 0 {
		return "", nil, fmt.Errorf("image attachment data is required")
	}

	mimeType := strings.TrimSpace(part.Attachment.MimeType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	data := part.Data
	if key := strings.TrimSpace(part.Attachment.Key); key != "" {
		data, mimeType = imageutil.PrepareModelInputImage(data, mimeType, key)
	}
	return mimeType, data, nil
}

func modelInputImageDataURL(part ContentPart) (string, error) {
	mimeType, data, err := modelInputImage(part)
	if err != nil {
		return "", err
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
