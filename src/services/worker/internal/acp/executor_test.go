package acp

import (
	"context"
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
)

func TestNewACPExecutor(t *testing.T) {
	ex, err := NewACPExecutor(nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}
	if ex == nil {
		t.Fatal("factory returned nil")
	}
	// default command
	acpEx := ex.(*Executor)
	if len(acpEx.command) != 2 || acpEx.command[0] != "opencode" {
		t.Errorf("default command = %v, want [opencode acp]", acpEx.command)
	}

	// custom command via config
	ex2, err := NewACPExecutor(map[string]any{
		"command": []any{"aider", "acp", "--model", "gpt-4"},
	})
	if err != nil {
		t.Fatalf("factory with command config returned error: %v", err)
	}
	acpEx2 := ex2.(*Executor)
	if len(acpEx2.command) != 4 || acpEx2.command[0] != "aider" {
		t.Errorf("custom command = %v, want [aider acp --model gpt-4]", acpEx2.command)
	}
}

func TestExecutor_NoSandbox(t *testing.T) {
	tests := []struct {
		name    string
		runtime *sharedtoolruntime.RuntimeSnapshot
	}{
		{"nil runtime", nil},
		{"empty sandbox URL", &sharedtoolruntime.RuntimeSnapshot{SandboxBaseURL: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &Executor{command: defaultACPCommand}
			emitter := events.NewEmitter("trace-test")
			rc := &pipeline.RunContext{
				Run:      data.Run{ID: uuid.New(), AccountID: uuid.New(), ThreadID: uuid.New()},
				Runtime:  tt.runtime,
				Messages: []llm.Message{{Role: "user", Content: []llm.ContentPart{{Text: "hello"}}}},
			}

			var got []events.RunEvent
			err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
				got = append(got, ev)
				return nil
			})
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 event, got %d", len(got))
			}
			if got[0].Type != "run.failed" {
				t.Errorf("event type = %q, want %q", got[0].Type, "run.failed")
			}
			if got[0].ErrorClass == nil || *got[0].ErrorClass != "acp.sandbox_unavailable" {
				t.Errorf("error_class = %v, want %q", got[0].ErrorClass, "acp.sandbox_unavailable")
			}
		})
	}
}

func TestExecutor_NoPrompt(t *testing.T) {
	ex := &Executor{command: defaultACPCommand}
	emitter := events.NewEmitter("trace-test")
	rc := &pipeline.RunContext{
		Run: data.Run{ID: uuid.New(), AccountID: uuid.New(), ThreadID: uuid.New()},
		Runtime: &sharedtoolruntime.RuntimeSnapshot{
			SandboxBaseURL:   "http://sandbox:8080",
			SandboxAuthToken: "tok",
		},
		Messages: []llm.Message{
			{Role: "assistant", Content: []llm.ContentPart{{Text: "previous reply"}}},
		},
	}

	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != "run.failed" {
		t.Errorf("event type = %q, want %q", got[0].Type, "run.failed")
	}
	if got[0].ErrorClass == nil || *got[0].ErrorClass != "acp.empty_prompt" {
		t.Errorf("error_class = %v, want %q", got[0].ErrorClass, "acp.empty_prompt")
	}
}

func TestExtractPrompt(t *testing.T) {
	tests := []struct {
		name     string
		messages []llm.Message
		want     string
	}{
		{
			name:     "empty messages",
			messages: nil,
			want:     "",
		},
		{
			name: "single user message",
			messages: []llm.Message{
				{Role: "user", Content: []llm.ContentPart{{Text: "build a web server"}}},
			},
			want: "build a web server",
		},
		{
			name: "picks last user message",
			messages: []llm.Message{
				{Role: "user", Content: []llm.ContentPart{{Text: "first prompt"}}},
				{Role: "assistant", Content: []llm.ContentPart{{Text: "ok"}}},
				{Role: "user", Content: []llm.ContentPart{{Text: "second prompt"}}},
			},
			want: "second prompt",
		},
		{
			name: "joins multiple content parts",
			messages: []llm.Message{
				{Role: "user", Content: []llm.ContentPart{
					{Text: "part one"},
					{Text: "part two"},
				}},
			},
			want: "part one\npart two",
		},
		{
			name: "skips blank parts",
			messages: []llm.Message{
				{Role: "user", Content: []llm.ContentPart{
					{Text: "   "},
					{Text: "real content"},
					{Text: ""},
				}},
			},
			want: "real content",
		},
		{
			name: "no user messages",
			messages: []llm.Message{
				{Role: "system", Content: []llm.ContentPart{{Text: "system prompt"}}},
				{Role: "assistant", Content: []llm.ContentPart{{Text: "reply"}}},
			},
			want: "",
		},
		{
			name: "user message with empty content",
			messages: []llm.Message{
				{Role: "user", Content: []llm.ContentPart{}},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPrompt(tt.messages)
			if got != tt.want {
				t.Errorf("extractPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}
