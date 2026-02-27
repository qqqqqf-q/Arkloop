package memory

import (
	"strings"
	"testing"
)

func TestBuildURI_UserScope(t *testing.T) {
	uri := BuildURI(MemoryScopeUser, MemoryCategoryPreference, "language")
	want := "viking://user/memories/preferences/language"
	if uri != want {
		t.Fatalf("got %q, want %q", uri, want)
	}
}

func TestBuildURI_AgentScope(t *testing.T) {
	uri := BuildURI(MemoryScopeAgent, MemoryCategoryPattern, "retry")
	want := "viking://agent/memories/patterns/retry"
	if uri != want {
		t.Fatalf("got %q, want %q", uri, want)
	}
}

func TestBuildURI_SanitizesKey(t *testing.T) {
	uri := BuildURI(MemoryScopeUser, MemoryCategoryEntity, "my project/name?v=1")
	// 非法字符（空格、斜杠、问号、等号）应替换为 _
	if strings.Contains(uri, "/my ") || strings.Contains(uri, "?") || strings.Contains(uri, "=") {
		t.Fatalf("key not sanitized: %q", uri)
	}
	if !strings.HasPrefix(uri, "viking://user/memories/entities/") {
		t.Fatalf("unexpected prefix: %q", uri)
	}
}

func TestBuildURI_TrimSpace(t *testing.T) {
	uri := BuildURI(MemoryScopeUser, MemoryCategoryProfile, "  name  ")
	if !strings.HasSuffix(uri, "/name") {
		t.Fatalf("expected trimmed key, got: %q", uri)
	}
}

func TestBuildURI_AllCategories(t *testing.T) {
	cases := []struct {
		cat  MemoryCategory
		frag string
	}{
		{MemoryCategoryProfile, "profile"},
		{MemoryCategoryPreference, "preferences"},
		{MemoryCategoryEntity, "entities"},
		{MemoryCategoryEvent, "events"},
		{MemoryCategoryCase, "cases"},
		{MemoryCategoryPattern, "patterns"},
	}
	for _, tc := range cases {
		uri := BuildURI(MemoryScopeUser, tc.cat, "k")
		expected := "viking://user/memories/" + tc.frag + "/k"
		if uri != expected {
			t.Errorf("category %q: got %q, want %q", tc.cat, uri, expected)
		}
	}
}
