package understandimage

import "context"

const (
	GroupName           = "image_understanding"
	ProviderNameMiniMax = "image_understanding.minimax"
	DefaultMiniMaxModel = "MiniMax-VL-01"
)

type DescribeImageRequest struct {
	Prompt    string
	SourceURL string
	MimeType  string
	Bytes     []byte
}

type DescribeImageResponse struct {
	Text     string
	Provider string
	Model    string
}

type Provider interface {
	DescribeImage(ctx context.Context, req DescribeImageRequest) (DescribeImageResponse, error)
	Name() string
}

type ProviderError struct {
	Message    string
	StatusCode int
	TraceID    string
	Provider   string
}

func (e ProviderError) Error() string {
	return e.Message
}
