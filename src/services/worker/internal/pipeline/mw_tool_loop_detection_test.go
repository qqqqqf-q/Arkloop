package pipeline

import (
	"testing"
)

func TestToolLoopDetector_WarnOnRepeat(t *testing.T) {
	d := newToolLoopDetector()
	args := map[string]any{"file": "foo.go", "content": "bar"}

	for i := 0; i < loopWarnThreshold; i++ {
		d.record("edit_file", args, "res1")
	}

	det := d.check("edit_file", args)
	if det == nil {
		t.Fatal("expected warning at threshold")
	}
	if det.Level != "warning" {
		t.Errorf("got level %s, want warning", det.Level)
	}
}

func TestToolLoopDetector_BlockOnRepeat(t *testing.T) {
	d := newToolLoopDetector()
	args := map[string]any{"file": "foo.go"}

	for i := 0; i < loopBlockThreshold; i++ {
		d.record("edit_file", args, "res1")
	}

	det := d.check("edit_file", args)
	if det == nil {
		t.Fatal("expected block at threshold")
	}
	if det.Level != "block" {
		t.Errorf("got level %s, want block", det.Level)
	}
}

func TestToolLoopDetector_PingPong(t *testing.T) {
	d := newToolLoopDetector()
	argsA := map[string]any{"file": "a.go"}
	argsB := map[string]any{"file": "b.go"}

	for i := 0; i < loopPingPongThresh; i++ {
		if i%2 == 0 {
			d.record("read_file", argsA, "resA")
		} else {
			d.record("edit_file", argsB, "resB")
		}
	}

	// next call would be argsA again
	det := d.check("read_file", argsA)
	if det == nil {
		t.Fatal("expected ping-pong detection")
	}
	if det.Level != "warning" {
		t.Errorf("got level %s, want warning", det.Level)
	}
}

func TestToolLoopDetector_NoProgressDetection(t *testing.T) {
	d := newToolLoopDetector()
	for i := 0; i < loopNoProgressThresh; i++ {
		d.record("grep", map[string]any{"q": "x", "i": i}, "same_hash")
	}

	det := d.check("grep", map[string]any{"q": "y"})
	if det == nil {
		t.Fatal("expected no-progress warning")
	}
}

func TestToolLoopDetector_NoFalsePositive(t *testing.T) {
	d := newToolLoopDetector()
	for i := 0; i < 3; i++ {
		d.record("read_file", map[string]any{"file": "a.go"}, "res")
	}
	det := d.check("read_file", map[string]any{"file": "a.go"})
	if det != nil {
		t.Error("should not warn below threshold")
	}
}

func TestComputeCallHash_ExcludesVolatile(t *testing.T) {
	h1 := computeCallHash("edit", map[string]any{"file": "a.go", "timestamp": "123"})
	h2 := computeCallHash("edit", map[string]any{"file": "a.go", "timestamp": "456"})
	if h1 != h2 {
		t.Error("hash should be stable despite volatile fields")
	}
}

func TestComputeCallHash_Deterministic(t *testing.T) {
	args := map[string]any{"b": 2, "a": 1}
	h1 := computeCallHash("tool", args)
	h2 := computeCallHash("tool", args)
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
}

func TestToolLoopDetector_WindowBounded(t *testing.T) {
	d := newToolLoopDetector()
	// fill with unique calls
	for i := 0; i < loopWindowSize+10; i++ {
		d.record("read", map[string]any{"i": i}, "r")
	}
	if len(d.window) > loopWindowSize {
		t.Errorf("window size %d exceeds max %d", len(d.window), loopWindowSize)
	}
}
