package edit

import (
	"strings"
	"testing"
)

func TestMatchExact_Single(t *testing.T) {
	content := "hello world\nfoo bar\n"
	r := match(content, "foo bar")
	if r == nil {
		t.Fatal("expected match")
	}
	if r.strategy != "exact" {
		t.Fatalf("expected exact, got %s", r.strategy)
	}
	if len(r.indices) != 1 {
		t.Fatalf("expected 1 match, got %d", len(r.indices))
	}
}

func TestMatchExact_Multiple(t *testing.T) {
	content := "aaa\nbbb\naaa\n"
	r := match(content, "aaa")
	if r == nil {
		t.Fatal("expected match")
	}
	if len(r.indices) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(r.indices))
	}
}

func TestMatchNormalized_Quotes(t *testing.T) {
	content := "say “hello” to the world"
	r := match(content, `say "hello" to the world`)
	if r == nil {
		t.Fatal("expected match")
	}
	if r.strategy != "normalized" {
		t.Fatalf("expected normalized, got %s", r.strategy)
	}
}

func TestMatchNormalized_Whitespace(t *testing.T) {
	content := "  func foo() {\n    return 1\n  }\n"
	r := match(content, "func foo() {\nreturn 1\n}")
	if r == nil {
		t.Fatal("expected match")
	}
	if r.strategy != "normalized" {
		t.Fatalf("expected normalized, got %s", r.strategy)
	}
	// actual should preserve original indentation
	if !strings.Contains(r.actuals[0], "  func foo()") {
		t.Fatalf("actual should preserve indentation, got: %s", r.actuals[0])
	}
}

func TestMatchRegex_TokenBased(t *testing.T) {
	// extra spaces between tokens
	content := "if  (x  ==  1)  {\n  return\n}"
	r := match(content, "if (x == 1) {\nreturn\n}")
	if r == nil {
		t.Fatal("expected match")
	}
	// should match via normalized or regex
	if r.strategy != "normalized" && r.strategy != "regex" {
		t.Fatalf("expected normalized or regex, got %s", r.strategy)
	}
}

func TestMatchExact_NoMatch(t *testing.T) {
	content := "hello world"
	r := match(content, "goodbye world")
	if r != nil {
		t.Fatal("expected no match")
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("func foo(x int) {")
	expected := []string{"func", "foo", "(", "x", "int", ")", "{"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i := range expected {
		if tokens[i] != expected[i] {
			t.Fatalf("token[%d]: expected %q, got %q", i, expected[i], tokens[i])
		}
	}
}

func TestNormalizeQuotes(t *testing.T) {
	input := "“hello” ‘world’"
	got := normalizeQuotes(input)
	want := `"hello" 'world'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPreserveQuoteStyle_NoChange(t *testing.T) {
	got := preserveQuoteStyle("hello", "hello", "world")
	if got != "world" {
		t.Fatalf("expected world, got %s", got)
	}
}

func TestPreserveQuoteStyle_CurlyDoubleQuotes(t *testing.T) {
	actual := "“hello”"
	got := preserveQuoteStyle(`"hello"`, actual, `"world"`)
	if !strings.Contains(got, "“") || !strings.Contains(got, "”") {
		t.Fatalf("expected curly quotes in result, got %q", got)
	}
}

func TestApplyIndentation(t *testing.T) {
	lines := []string{"func foo() {", "  return 1", "}"}
	result := applyIndentation(lines, "    ")
	if result[0] != "    func foo() {" {
		t.Fatalf("line 0: %q", result[0])
	}
	if result[1] != "      return 1" {
		t.Fatalf("line 1: %q", result[1])
	}
	if result[2] != "    }" {
		t.Fatalf("line 2: %q", result[2])
	}
}

func TestDiffSnippet(t *testing.T) {
	content := "line1\nline2\nline3\nNEW\nline4\nline5\n"
	s := diffSnippet(content, 18, 0, 3, 2)
	if s == "" {
		t.Fatal("expected non-empty snippet")
	}
	if !strings.Contains(s, "NEW") {
		t.Fatalf("snippet should contain NEW: %s", s)
	}
}

func TestAllIndices(t *testing.T) {
	indices := allIndices("abcabcabc", "abc")
	if len(indices) != 3 {
		t.Fatalf("expected 3, got %d", len(indices))
	}
}
