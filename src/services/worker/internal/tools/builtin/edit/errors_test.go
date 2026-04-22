package edit

import (
	"strings"
	"testing"
)

func TestErrNotFound_WithClosestMatch(t *testing.T) {
	content := "func hello() {\n  return 1\n}\n"
	e := errNotFound("test.go", content, "func hello() {\n  return 2\n}")
	if e.Code != ErrCodeNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeNotFound, e.Code)
	}
	if !strings.Contains(e.Hint, "closest match was found at lines") {
		t.Fatalf("expected closest match hint, got: %s", e.Hint)
	}
}

func TestErrNotFound_NoCloseMatch(t *testing.T) {
	content := "aaa\nbbb\nccc\n"
	e := errNotFound("test.go", content, "completely different text that is very long and has no overlap whatsoever with the content")
	if e.Code != ErrCodeNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeNotFound, e.Code)
	}
	// should not contain closest match since distance is too large
}

func TestLevenshteinLines_Identical(t *testing.T) {
	if d := levenshteinLines("abc", "abc"); d != 0 {
		t.Fatalf("expected 0, got %d", d)
	}
}

func TestLevenshteinLines_Different(t *testing.T) {
	d := levenshteinLines("abc", "abd")
	if d != 1 {
		t.Fatalf("expected 1, got %d", d)
	}
}

func TestFindClosestMatch_Empty(t *testing.T) {
	if m := findClosestMatch("", "needle", 5); m != nil {
		t.Fatalf("expected nil, got %+v", m)
	}
}

func TestErrNoOp(t *testing.T) {
	e := errNoOp("test.go")
	if e.Code != ErrCodeNoOp {
		t.Fatalf("expected %s, got %s", ErrCodeNoOp, e.Code)
	}
}

func TestErrAmbiguous(t *testing.T) {
	e := errAmbiguous("test.go", 3)
	if e.Code != ErrCodeAmbiguous {
		t.Fatalf("expected %s, got %s", ErrCodeAmbiguous, e.Code)
	}
	if !strings.Contains(e.Message, "3") {
		t.Fatalf("expected count in message, got: %s", e.Message)
	}
}

func TestEditError_Format(t *testing.T) {
	e := &editError{Code: "TEST", Message: "msg", Hint: "hint"}
	s := e.Error()
	if !strings.Contains(s, "[TEST]") || !strings.Contains(s, "msg") || !strings.Contains(s, "hint") {
		t.Fatalf("unexpected format: %s", s)
	}

	e2 := &editError{Code: "TEST", Message: "msg"}
	s2 := e2.Error()
	if strings.Contains(s2, "\n") {
		t.Fatalf("should not have newline without hint: %s", s2)
	}
}
