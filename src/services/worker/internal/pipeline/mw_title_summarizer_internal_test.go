package pipeline

import "testing"

func TestNormalizeGeneratedTitle(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain", raw: "改这段代码", want: "改这段代码"},
		{name: "drops label", raw: "Title: Fix login bug", want: "Fix login bug"},
		{name: "first non-empty line", raw: "\n\n标题：优化首页\n补充说明", want: "优化首页"},
		{name: "drops quotes and punctuation", raw: "“写登录页。”", want: "写登录页"},
		{name: "empty after trim", raw: " \n ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeGeneratedTitle(tt.raw)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
