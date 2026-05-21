package speak

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/tools"
)

type bindingStub struct {
	discuss bool
	visible bool
	replyTo string
}

func (b *bindingStub) SetDiscussSpeak(replyToMessageID string) {
	b.visible = true
	b.replyTo = replyToMessageID
}

func (b *bindingStub) IsDiscussRun() bool {
	return b.discuss
}

func TestSpeakSetsDiscussState(t *testing.T) {
	binding := &bindingStub{discuss: true}
	res := New().Execute(context.Background(), ToolName, map[string]any{"reply_to_message_id": "42"}, tools.ExecutionContext{
		PipelineRC: binding,
	}, "")
	if res.Error != nil {
		t.Fatalf("speak failed: %v", res.Error)
	}
	if !binding.visible || binding.replyTo != "42" {
		t.Fatalf("unexpected binding state: visible=%v reply=%q", binding.visible, binding.replyTo)
	}
	if res.ResultJSON["reply_to_message_id"] != "42" {
		t.Fatalf("unexpected result: %#v", res.ResultJSON)
	}
}

func TestSpeakRejectsNonDiscussRun(t *testing.T) {
	binding := &bindingStub{}
	res := New().Execute(context.Background(), ToolName, nil, tools.ExecutionContext{
		PipelineRC: binding,
	}, "")
	if res.Error == nil {
		t.Fatal("expected error")
	}
}
