package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLogAction(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	logger.LogAction("install", "openviking", map[string]string{"key": "val"})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for _, field := range []string{"timestamp", "action", "module_id", "result"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("expected field %q in JSON output", field)
		}
	}

	if entry["action"] != "install" {
		t.Errorf("action = %v, want %q", entry["action"], "install")
	}
	if entry["module_id"] != "openviking" {
		t.Errorf("module_id = %v, want %q", entry["module_id"], "openviking")
	}
	if entry["result"] != "started" {
		t.Errorf("result = %v, want %q", entry["result"], "started")
	}
}

func TestLogActionComplete(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	logger.LogActionComplete("install", "openviking", 2*time.Second, nil)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if entry["result"] != "completed" {
		t.Errorf("result = %v, want %q", entry["result"], "completed")
	}
	if _, ok := entry["duration"]; !ok {
		t.Error("expected duration field")
	}
}

func TestLogActionCompleteFailed(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	logger.LogActionComplete("install", "openviking", time.Second, errors.New("connection refused"))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if entry["result"] != "failed" {
		t.Errorf("result = %v, want %q", entry["result"], "failed")
	}
	if entry["error"] != "connection refused" {
		t.Errorf("error = %v, want %q", entry["error"], "connection refused")
	}
}

func TestLogActionTruncatesParams(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	logger.LogAction("install", "openviking", map[string]string{
		"body": strings.Repeat("x", 2048),
	})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	params := entry["params"].(map[string]any)
	got := params["body"].(string)
	if len(got) >= 2048 {
		t.Fatalf("expected truncated param, len=%d", len(got))
	}
}
