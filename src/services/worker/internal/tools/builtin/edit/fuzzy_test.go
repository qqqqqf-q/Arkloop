package edit

import (
	"strings"
	"testing"
)

func TestMatchFuzzy_WhitespaceDiff(t *testing.T) {
	content := "func foo() {\n\treturn   bar\n}\n"
	search := "func foo() {\n\treturn bar\n}\n"

	r := matchFuzzy(content, search)
	if r == nil {
		t.Fatal("expected fuzzy match for whitespace diff")
	}
	if r.strategy != "fuzzy" {
		t.Fatalf("expected strategy fuzzy, got %s", r.strategy)
	}
}

func TestMatchFuzzy_IndentDiff(t *testing.T) {
	content := "  if true {\n    doSomething()\n  }"
	search := "if true {\n  doSomething()\n}"

	r := matchFuzzy(content, search)
	if r == nil {
		t.Fatal("expected fuzzy match for indent diff")
	}
}

func TestMatchFuzzy_MinorTypo(t *testing.T) {
	content := "func procesData(input string) error {\n\treturn nil\n}"
	search := "func processData(input string) error {\n\treturn nil\n}"

	r := matchFuzzy(content, search)
	if r == nil {
		t.Fatal("expected fuzzy match for minor typo")
	}
}

func TestMatchFuzzy_TooDistant(t *testing.T) {
	content := "completely different text here"
	search := "func foo() { return bar }"

	r := matchFuzzy(content, search)
	if r != nil {
		t.Fatal("expected nil for very different content")
	}
}

func TestMatchFuzzy_ComplexityGuard(t *testing.T) {
	// large source * large search^2 > 4e8
	source := strings.Repeat("x\n", 10000)
	search := strings.Repeat("y", 300)

	r := matchFuzzy(source, search)
	if r != nil {
		t.Fatal("expected nil due to complexity guard")
	}
}

func TestMatchFuzzy_PicksBestMatch(t *testing.T) {
	content := "func aaa() {}\nfunc foo() {\n\treturn bar\n}\nfunc bbb() {}"
	search := "func foo() {\n\treturn  bar\n}"

	r := matchFuzzy(content, search)
	if r == nil {
		t.Fatal("expected fuzzy match")
	}
	if !strings.Contains(r.actuals[0], "func foo") {
		t.Fatalf("expected match on foo block, got: %s", r.actuals[0])
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
	}
	for _, tc := range tests {
		got := levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
