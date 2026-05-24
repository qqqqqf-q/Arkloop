package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSessionExtractsUserAndFinalAnswer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	payload := `{"type":"session_meta","timestamp":"2026-05-21T01:00:00Z","payload":{"id":"s1","cwd":"/tmp/project"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:01Z","payload":{"type":"task_started","turn_id":"t1"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:02Z","payload":{"type":"user_message","message":"hello"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:03Z","payload":{"type":"agent_message","phase":"thinking","message":"hidden"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:04Z","payload":{"type":"agent_message","phase":"final_answer","message":"done"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:05Z","payload":{"type":"task_complete"}}
`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := parseSession(path, "session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Action != "prompted" || events[0].Title != "hello" {
		t.Fatalf("unexpected user event: %#v", events[0])
	}
	if events[1].Action != "received" || events[1].Title != "done" {
		t.Fatalf("unexpected assistant event: %#v", events[1])
	}
}

func TestParseSessionExtractsResponseItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	payload := `{"type":"session_meta","timestamp":"2026-05-21T01:00:00Z","payload":{"id":"s1","cwd":"/tmp/project"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:01Z","payload":{"type":"task_started","turn_id":"t1"}}
{"type":"response_item","timestamp":"2026-05-21T01:00:02Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\nignore"}]}}
{"type":"response_item","timestamp":"2026-05-21T01:00:03Z","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"dev"}]}}
{"type":"response_item","timestamp":"2026-05-21T01:00:04Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"real prompt"}]}}
{"type":"response_item","timestamp":"2026-05-21T01:00:05Z","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"final"}]}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:06Z","payload":{"type":"task_complete"}}
`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := parseSession(path, "session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Action != "prompted" || events[0].Title != "real prompt" {
		t.Fatalf("unexpected user event: %#v", events[0])
	}
	if events[1].Action != "received" || events[1].Title != "final" {
		t.Fatalf("unexpected assistant event: %#v", events[1])
	}
}

func TestParseSessionDeduplicatesResponseItemAndEventMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	payload := `{"type":"session_meta","timestamp":"2026-05-21T01:00:00Z","payload":{"id":"s1","cwd":"/tmp/project"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:01Z","payload":{"type":"task_started","turn_id":"t1"}}
{"type":"response_item","timestamp":"2026-05-21T01:00:02Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"same prompt"}]}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:03Z","payload":{"type":"user_message","message":"same prompt"}}
{"type":"response_item","timestamp":"2026-05-21T01:00:04Z","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"same final"}]}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:05Z","payload":{"type":"agent_message","phase":"final_answer","message":"same final"}}
{"type":"event_msg","timestamp":"2026-05-21T01:00:06Z","payload":{"type":"task_complete"}}
`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := parseSession(path, "session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Action != "prompted" || events[0].Title != "same prompt" {
		t.Fatalf("unexpected user event: %#v", events[0])
	}
	if events[1].Action != "received" || events[1].Title != "same final" {
		t.Fatalf("unexpected assistant event: %#v", events[1])
	}
}
