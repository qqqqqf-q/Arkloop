package read

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

const defaultMiniMaxBaseURL = "https://api.minimaxi.com"

type MiniMaxProvider struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string
}

func NewMiniMaxProvider(apiKey string, baseURL string, model string) (*MiniMaxProvider, error) {
	cleanedKey := strings.TrimSpace(apiKey)
	if cleanedKey == "" {
		return nil, fmt.Errorf("minimax api_key not configured")
	}

	cleanedBaseURL := strings.TrimSpace(baseURL)
	if cleanedBaseURL == "" {
		cleanedBaseURL = defaultMiniMaxBaseURL
	}
	normalizedBaseURL, err := sharedoutbound.DefaultPolicy().NormalizeBaseURL(cleanedBaseURL)
	if err != nil {
		return nil, err
	}

	cleanedModel := strings.TrimSpace(model)
	if cleanedModel == "" {
		cleanedModel = DefaultMiniMaxModel
	}

	return &MiniMaxProvider{
		http:    sharedoutbound.DefaultPolicy().NewHTTPClient(0),
		baseURL: normalizedBaseURL,
		apiKey:  cleanedKey,
		model:   cleanedModel,
	}, nil
}

func (p *MiniMaxProvider) DescribeImage(ctx context.Context, req DescribeImageRequest) (DescribeImageResponse, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return DescribeImageResponse{}, fmt.Errorf("minimax prompt is required")
	}
	imageDataURL, err := imageDataURL(req.MimeType, req.Bytes)
	if err != nil {
		return DescribeImageResponse{}, err
	}

	body, err := json.Marshal(map[string]any{
		"model":     p.model,
		"prompt":    prompt,
		"image_url": imageDataURL,
	})
	if err != nil {
		return DescribeImageResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+"/v1/coding_plan/vlm", bytes.NewReader(body))
	if err != nil {
		return DescribeImageResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("MM-API-Source", "Arkloop")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return DescribeImageResponse{}, err
	}
	defer resp.Body.Close()

	traceID := strings.TrimSpace(resp.Header.Get("Trace-Id"))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DescribeImageResponse{}, ProviderError{
			Message:    buildMiniMaxHTTPError(resp, traceID),
			StatusCode: resp.StatusCode,
			TraceID:    traceID,
			Provider:   p.Name(),
		}
	}

	var payload struct {
		Content  string `json:"content"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return DescribeImageResponse{}, ProviderError{
			Message:  buildMiniMaxTraceMessage("minimax image understanding returned invalid JSON", traceID),
			TraceID:  traceID,
			Provider: p.Name(),
		}
	}
	if payload.BaseResp.StatusCode != 0 {
		message := "minimax image understanding failed"
		if statusMsg := strings.TrimSpace(payload.BaseResp.StatusMsg); statusMsg != "" {
			message = message + ": " + statusMsg
		}
		return DescribeImageResponse{}, ProviderError{
			Message:  messageWithTrace(message, traceID),
			TraceID:  traceID,
			Provider: p.Name(),
		}
	}

	content := strings.TrimSpace(payload.Content)
	if content == "" {
		return DescribeImageResponse{}, ProviderError{
			Message:  buildMiniMaxTraceMessage("minimax image understanding returned empty content", traceID),
			TraceID:  traceID,
			Provider: p.Name(),
		}
	}

	return DescribeImageResponse{
		Text:     content,
		Provider: p.Name(),
		Model:    p.model,
	}, nil
}

func imageDataURL(mimeType string, body []byte) (string, error) {
	cleanedMimeType := normalizeMimeType(mimeType)
	if !isSupportedImageMime(cleanedMimeType) {
		return "", fmt.Errorf("unsupported image mime type: %s", cleanedMimeType)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("image body is empty")
	}
	encoded := base64.StdEncoding.EncodeToString(body)
	return "data:" + cleanedMimeType + ";base64," + encoded, nil
}

func buildMiniMaxHTTPError(resp *http.Response, traceID string) string {
	message := fmt.Sprintf("minimax image understanding request failed (%d %s)", resp.StatusCode, strings.TrimSpace(resp.Status))
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if snippet := strings.TrimSpace(string(body)); snippet != "" {
		message = message + ": " + snippet
	}
	return messageWithTrace(message, traceID)
}

func buildMiniMaxTraceMessage(message string, traceID string) string {
	return messageWithTrace(strings.TrimSpace(message), traceID)
}

func (p *MiniMaxProvider) Name() string {
	return ProviderNameMiniMax
}

func messageWithTrace(message string, traceID string) string {
	if strings.TrimSpace(traceID) == "" {
		return message
	}
	return message + " (trace_id=" + traceID + ")"
}
