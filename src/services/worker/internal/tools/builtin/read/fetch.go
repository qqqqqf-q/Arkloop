package read

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

var supportedImageMIMEs = map[string]struct{}{
	"image/gif":  {},
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

type fetchedImage struct {
	SourceURL string
	FinalURL  string
	MimeType  string
	Bytes     []byte
}

type imageTooLargeError struct {
	MaxBytes int
}

func (e imageTooLargeError) Error() string {
	return fmt.Sprintf("image exceeds max_bytes=%d", e.MaxBytes)
}

type unsupportedMediaTypeError struct {
	DetectedMimeType string
}

func (e unsupportedMediaTypeError) Error() string {
	return fmt.Sprintf("unsupported media type: %s", e.DetectedMimeType)
}

type httpStatusError struct {
	StatusCode int
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("unexpected http status: %d", e.StatusCode)
}

func fetchRemoteImage(ctx context.Context, targetURL string, maxBytes int) (fetchedImage, error) {
	client := sharedoutbound.DefaultPolicy().NewHTTPClient(0)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fetchedImage{}, err
	}
	req.Header.Set("User-Agent", "arkloop-read/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fetchedImage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fetchedImage{}, httpStatusError{StatusCode: resp.StatusCode}
	}
	if resp.ContentLength > 0 && resp.ContentLength > int64(maxBytes) {
		return fetchedImage{}, imageTooLargeError{MaxBytes: maxBytes}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return fetchedImage{}, err
	}
	if len(body) > maxBytes {
		return fetchedImage{}, imageTooLargeError{MaxBytes: maxBytes}
	}

	mimeType := detectImageMimeType(resp.Header.Get("Content-Type"), body)
	if mimeType == "" {
		return fetchedImage{}, unsupportedMediaTypeError{
			DetectedMimeType: detectedMimeType(resp.Header.Get("Content-Type"), body),
		}
	}

	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return fetchedImage{
		SourceURL: targetURL,
		FinalURL:  finalURL,
		MimeType:  mimeType,
		Bytes:     body,
	}, nil
}

func detectImageMimeType(contentType string, body []byte) string {
	headerType := normalizeMimeType(contentType)
	if isSupportedImageMime(headerType) {
		return headerType
	}
	sniffedType := sniffMimeType(body)
	if isSupportedImageMime(sniffedType) {
		return sniffedType
	}
	return ""
}

func detectedMimeType(contentType string, body []byte) string {
	headerType := normalizeMimeType(contentType)
	if headerType != "" {
		return headerType
	}
	return sniffMimeType(body)
}

func sniffMimeType(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sniffLen := len(body)
	if sniffLen > 512 {
		sniffLen = 512
	}
	return normalizeMimeType(http.DetectContentType(body[:sniffLen]))
}

func normalizeMimeType(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(cleaned)
	if err != nil {
		return strings.ToLower(cleaned)
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func isSupportedImageMime(mimeType string) bool {
	_, ok := supportedImageMIMEs[strings.ToLower(strings.TrimSpace(mimeType))]
	return ok
}
