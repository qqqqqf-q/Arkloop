package pluginhook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestCommandHookDeny(t *testing.T) {
	runner := NewCommandHookRunner(HookConfig{
		PluginID:  "demo",
		Event:     EventBeforeRun,
		Type:      HookTypeCommand,
		Command:   []string{os.Args[0]},
		Args:      []string{"-test.run=TestCommandHookHelper", "--", "deny"},
		TimeoutMS: 1000,
	})
	output, err := runner.Run(context.Background(), HookInput{RunID: "run_1"})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if output.Action != ActionDeny || output.Reason != "blocked" {
		t.Fatalf("expected deny, got %#v", output)
	}
}

func TestHookInputMarshalFlattensPayload(t *testing.T) {
	data, err := json.Marshal(HookInput{
		Event:    EventBeforeToolUse,
		PluginID: "demo",
		RunID:    "run_1",
		Payload: map[string]any{
			"tool_name":    "echo",
			"tool_call_id": "call_1",
		},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode input: %v", err)
	}
	if out["event"] != string(EventBeforeToolUse) || out["tool_name"] != "echo" || out["run_id"] != "run_1" {
		t.Fatalf("unexpected marshaled input: %s", data)
	}
	if _, ok := out["payload"]; ok {
		t.Fatalf("payload must be flattened: %s", data)
	}
}

func TestHookOutputSupportsPlanSchema(t *testing.T) {
	output, err := decodeOutput([]byte(`{"action":"modify","args":{"text":"modified"}}`), EventBeforeToolUse)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if output.Action != ActionModify || output.ModifiedArgs == nil {
		t.Fatalf("expected modified args, got %#v", output)
	}
	output, err = decodeOutput([]byte(`{"action":"continue","inject_segments":[{"role":"system","content":"ctx"}]}`), EventBeforeModel)
	if err != nil {
		t.Fatalf("decode inject: %v", err)
	}
	if len(output.InjectSegments) != 1 || output.InjectSegments[0].Content != "ctx" {
		t.Fatalf("unexpected inject segments: %#v", output.InjectSegments)
	}
}

func TestCommandHookTimeoutContinues(t *testing.T) {
	runner := NewCommandHookRunner(HookConfig{
		PluginID:  "demo",
		Event:     EventBeforeRun,
		Type:      HookTypeCommand,
		Command:   []string{os.Args[0], "-test.run=TestCommandHookHelper", "--", "sleep"},
		TimeoutMS: 10,
	})
	output, err := runner.Run(context.Background(), HookInput{RunID: "run_1"})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if output.Action != ActionContinue || output.Error == "" {
		t.Fatalf("expected continue with error, got %#v", output)
	}
}

func TestCommandHookHelper(t *testing.T) {
	args := os.Args
	for i, arg := range args {
		if arg != "--" || i+1 >= len(args) {
			continue
		}
		switch args[i+1] {
		case "deny":
			var input HookInput
			if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			fmt.Println(`{"action":"deny","reason":"blocked"}`)
			os.Exit(0)
		case "sleep":
			time.Sleep(time.Second)
			os.Exit(0)
		}
	}
}
