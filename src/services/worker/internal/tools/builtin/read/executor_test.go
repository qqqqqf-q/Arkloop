package read

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/shared/messagecontent"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"

	"github.com/google/uuid"
)

type fakeProvider struct {
	resp DescribeImageResponse
	err  error
	req  DescribeImageRequest
}

func (p *fakeProvider) DescribeImage(_ context.Context, req DescribeImageRequest) (DescribeImageResponse, error) {
	p.req = req
	if p.err != nil {
		return DescribeImageResponse{}, p.err
	}
	return p.resp, nil
}

func (p *fakeProvider) Name() string {
	return "fake"
}

type fakePipelineRunContext struct {
	messages []llm.Message
}

func (f *fakePipelineRunContext) ReadToolMessages() []llm.Message {
	if f == nil || len(f.messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(f.messages))
	copy(out, f.messages)
	return out
}

type legacyPipelineShape struct {
	Messages []llm.Message
}

func TestLlmSpecIncludesUnifiedSourceKinds(t *testing.T) {
	props, ok := LlmSpec.JSONSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected schema properties: %#v", LlmSpec.JSONSchema["properties"])
	}
	source, ok := props["source"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected source schema: %#v", props["source"])
	}
	sourceProps, ok := source["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected source.properties: %#v", source["properties"])
	}
	kind, ok := sourceProps["kind"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected source.kind schema: %#v", sourceProps["kind"])
	}
	enum, ok := kind["enum"].([]string)
	if !ok {
		t.Fatalf("unexpected source.kind enum: %#v", kind["enum"])
	}
	want := []string{"file_path", "message_attachment", "remote_url"}
	if len(enum) != len(want) {
		t.Fatalf("unexpected kind enum length: got %d want %d", len(enum), len(want))
	}
	for i := range want {
		if enum[i] != want[i] {
			t.Fatalf("unexpected enum[%d]: got %q want %q", i, enum[i], want[i])
		}
	}
}

func TestReadFilePathSource(t *testing.T) {
	tracker := fileops.NewFileTracker()
	executor := NewToolExecutorWithTracker(tracker)

	workDir := t.TempDir()
	path := filepath.Join(workDir, "sample.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	runID := uuid.New()
	result := executor.Execute(context.Background(), "read", map[string]any{
		"source": map[string]any{
			"kind":      "file_path",
			"file_path": "sample.txt",
		},
		"offset": 2,
		"limit":  1,
	}, tools.ExecutionContext{RunID: runID, WorkDir: workDir}, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	content, _ := result.ResultJSON["content"].(string)
	if !strings.Contains(content, "2|line2") {
		t.Fatalf("unexpected content: %q", content)
	}
	if !tracker.HasBeenRead("sample.txt") {
		t.Fatal("expected file tracker to record read")
	}
}

func TestReadRemoteURLSource(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	provider := &fakeProvider{
		resp: DescribeImageResponse{
			Text:     "this is a screenshot",
			Provider: "minimax",
			Model:    "MiniMax-VL-01",
		},
	}
	executor := NewToolExecutorWithProvider(provider)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNGBytes(t))
	}))
	defer server.Close()

	result := executor.Execute(context.Background(), "read", map[string]any{
		"source": map[string]any{
			"kind": "remote_url",
			"url":  server.URL + "/shot.png",
		},
		"prompt": "describe this image",
	}, tools.ExecutionContext{}, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := provider.req.MimeType; got != "image/png" {
		t.Fatalf("unexpected mime type: %q", got)
	}
	if got := result.ResultJSON["source_kind"]; got != "remote_url" {
		t.Fatalf("unexpected source kind: %#v", got)
	}
}

func TestReadMessageAttachmentSource(t *testing.T) {
	provider := &fakeProvider{
		resp: DescribeImageResponse{
			Text:     "attachment image text",
			Provider: "minimax",
			Model:    "MiniMax-VL-01",
		},
	}
	executor := NewToolExecutorWithProvider(provider)
	key := "threads/thread-a/attachments/1/cat.png"

	rc := &fakePipelineRunContext{
		messages: []llm.Message{
			{
				Role: "user",
				Content: []llm.ContentPart{
					{
						Type: "image",
						Attachment: &messagecontent.AttachmentRef{
							Key:      key,
							Filename: "cat.png",
							MimeType: "image/png",
						},
						Data: testPNGBytes(t),
					},
				},
			},
		},
	}

	result := executor.Execute(context.Background(), "read", map[string]any{
		"source": map[string]any{
			"kind":           "message_attachment",
			"attachment_key": key,
		},
		"prompt": "what is in this attachment",
	}, tools.ExecutionContext{PipelineRC: rc}, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %+v", result.Error)
	}
	if got := result.ResultJSON["source_kind"]; got != "message_attachment" {
		t.Fatalf("unexpected source kind: %#v", got)
	}
	if got := result.ResultJSON["attachment_key"]; got != key {
		t.Fatalf("unexpected attachment key: %#v", got)
	}
	if len(provider.req.Bytes) == 0 {
		t.Fatal("expected provider to receive image bytes")
	}
}

func TestReadMessageAttachmentSourceRejectsLegacyPipelineShape(t *testing.T) {
	provider := &fakeProvider{
		resp: DescribeImageResponse{
			Text:     "attachment image text",
			Provider: "minimax",
			Model:    "MiniMax-VL-01",
		},
	}
	executor := NewToolExecutorWithProvider(provider)
	key := "threads/thread-a/attachments/1/cat.png"

	legacy := &legacyPipelineShape{
		Messages: []llm.Message{
			{
				Role: "user",
				Content: []llm.ContentPart{
					{
						Type: "image",
						Attachment: &messagecontent.AttachmentRef{
							Key:      key,
							Filename: "cat.png",
							MimeType: "image/png",
						},
						Data: testPNGBytes(t),
					},
				},
			},
		},
	}

	result := executor.Execute(context.Background(), "read", map[string]any{
		"source": map[string]any{
			"kind":           "message_attachment",
			"attachment_key": key,
		},
		"prompt": "what is in this attachment",
	}, tools.ExecutionContext{PipelineRC: legacy}, "")

	if result.Error == nil {
		t.Fatal("expected error for pipeline context without ReadToolMessages")
	}
	if result.Error.ErrorClass != errorFetchFailed {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func TestReadImageSourceRequiresPrompt(t *testing.T) {
	executor := NewToolExecutorWithProvider(&fakeProvider{})
	result := executor.Execute(context.Background(), "read", map[string]any{
		"source": map[string]any{
			"kind": "remote_url",
			"url":  "https://example.com/image.png",
		},
	}, tools.ExecutionContext{}, "")
	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
}

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	raw := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF9sAAAAASUVORK5CYII="
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return decoded
}
