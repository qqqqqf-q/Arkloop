package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompressResult_UnderLimit(t *testing.T) {
	result := ExecutionResult{
		ResultJSON: map[string]any{"output": "hello"},
	}
	got := CompressResult("test_tool", result, 1024)
	if _, ok := got.ResultJSON["_compressed"]; ok {
		t.Fatal("should not compress when under limit")
	}
	if got.ResultJSON["output"] != "hello" {
		t.Fatal("output should be unchanged")
	}
}

func TestCompressResult_StringFieldTruncated(t *testing.T) {
	longStr := strings.Repeat("a\n", 10000)
	result := ExecutionResult{
		ResultJSON: map[string]any{"output": longStr},
	}
	got := CompressResult("test_tool", result, 512)
	if _, ok := got.ResultJSON["_compressed"]; !ok {
		t.Fatal("expected _compressed flag")
	}
	raw, _ := json.Marshal(got.ResultJSON)
	if len(raw) >= len(longStr) {
		t.Fatal("compressed result should be smaller than original")
	}
	if _, ok := got.ResultJSON["output"].(string); !ok {
		t.Fatal("output should still be a string")
	}
}

func TestCompressResult_ArrayFieldTruncated(t *testing.T) {
	items := make([]any, 100)
	for i := range items {
		items[i] = map[string]any{"index": i, "data": strings.Repeat("x", 100)}
	}
	result := ExecutionResult{
		ResultJSON: map[string]any{"artifacts": items},
	}
	got := CompressResult("test_tool", result, 512)
	if _, ok := got.ResultJSON["_compressed"]; !ok {
		t.Fatal("expected _compressed flag")
	}
	arr, ok := got.ResultJSON["artifacts"].([]any)
	if !ok {
		t.Fatal("artifacts should be array")
	}
	if len(arr) >= 100 {
		t.Fatal("array should be truncated")
	}
	// Last two items preserved
	lastItem := arr[len(arr)-1]
	if m, ok := lastItem.(map[string]any); ok {
		if _, hasTruncated := m["_truncated"]; hasTruncated {
			t.Fatal("last item should not be the truncation marker")
		}
	}
}

func TestCompressResult_ErrorFieldPreserved(t *testing.T) {
	longStr := strings.Repeat("x", 100000)
	result := ExecutionResult{
		ResultJSON: map[string]any{
			"output": longStr,
			"error":  "something went wrong",
			"status": "failed",
		},
	}
	got := CompressResult("test_tool", result, 512)
	if got.ResultJSON["error"] != "something went wrong" {
		t.Fatal("error field should be preserved")
	}
	if got.ResultJSON["status"] != "failed" {
		t.Fatal("status field should be preserved")
	}
}

func TestCompressResult_MetadataInjected(t *testing.T) {
	longStr := strings.Repeat("z\n", 5000)
	result := ExecutionResult{
		ResultJSON: map[string]any{"stdout": longStr},
	}
	got := CompressResult("test_tool", result, 256)
	if got.ResultJSON["_compressed"] != true {
		t.Fatal("_compressed should be true")
	}
	origBytes, ok := got.ResultJSON["_original_bytes"].(int)
	if !ok || origBytes <= 0 {
		t.Fatal("_original_bytes should be a positive int")
	}
}

func TestCompressResult_NilResultJSON(t *testing.T) {
	result := ExecutionResult{ResultJSON: nil}
	got := CompressResult("test_tool", result, 256)
	if got.ResultJSON != nil {
		t.Fatal("nil ResultJSON should pass through unchanged")
	}
}

func TestCompressResult_NestedMap(t *testing.T) {
	nested := map[string]any{
		"inner": strings.Repeat("y\n", 2000),
	}
	result := ExecutionResult{
		ResultJSON: map[string]any{"data": nested},
	}
	got := CompressResult("test_tool", result, 512)
	if _, ok := got.ResultJSON["_compressed"]; !ok {
		t.Fatal("expected _compressed flag")
	}
	data, ok := got.ResultJSON["data"].(map[string]any)
	if !ok {
		t.Fatal("data should be a map")
	}
	inner, ok := data["inner"].(string)
	if !ok {
		t.Fatal("inner should be a string")
	}
	if len(inner) >= 2000*3 {
		t.Fatal("inner string should be truncated")
	}
}

func TestShouldBypassResultCompression_GenerativeUIBootstrapTools(t *testing.T) {
	for _, toolName := range []string{"visualize_read_me", "artifact_guidelines"} {
		if !ShouldBypassResultCompression(toolName) {
			t.Fatalf("expected compression bypass for %s", toolName)
		}
		if !ShouldBypassResultSummarization(toolName) {
			t.Fatalf("expected summarization bypass for %s", toolName)
		}
	}
}
