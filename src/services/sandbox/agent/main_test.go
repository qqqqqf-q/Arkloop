package main

import (
	"strings"
	"testing"
)

func TestLimitedBuffer_Truncates(t *testing.T) {
	buf := newLimitedBuffer(10)
	n, err := buf.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len("hello world") {
		t.Fatalf("expected n=%d, got %d", len("hello world"), n)
	}
	if got := buf.String(); got != "hello worl" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestExecuteJob_CodeTooLarge(t *testing.T) {
	job := ExecJob{
		Language:  "python",
		Code:      strings.Repeat("a", maxCodeBytes+1),
		TimeoutMs: 1000,
	}
	result := executeJob(job)
	if result.ExitCode != 1 {
		t.Fatalf("expected ExitCode=1, got %d", result.ExitCode)
	}
	if strings.TrimSpace(result.Stderr) == "" {
		t.Fatalf("expected stderr not empty")
	}
}
