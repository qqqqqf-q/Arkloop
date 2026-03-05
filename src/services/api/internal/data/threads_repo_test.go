package data

import "testing"

func TestEscapeILikePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "abc", want: "abc"},
		{name: "percent", input: "a%b", want: "a!%b"},
		{name: "underscore", input: "a_b", want: "a!_b"},
		{name: "escape_char", input: "a!b", want: "a!!b"},
		{name: "mixed", input: "a%!_b!", want: "a!%!!!_b!!"},
		{name: "only_specials", input: "%_!", want: "!%!_!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := escapeILikePattern(tt.input); got != tt.want {
				t.Fatalf("escapeILikePattern(%q)=%q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
