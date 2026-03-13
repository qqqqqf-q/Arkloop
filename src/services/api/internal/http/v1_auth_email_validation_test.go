//go:build !desktop

package http

import "testing"

func TestIsValidEmail(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		{email: "alice@example.com", want: true},
		{email: "a+b@example.co.uk", want: true},
		{email: "@", want: false},
		{email: "a@", want: false},
		{email: "a", want: false},
		{email: "Alice <alice@example.com>", want: false},
		{email: "alice@example.com\r\nBcc: evil@example.com", want: false},
		{email: "alice@example.com\n", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.email, func(t *testing.T) {
			if got := isValidEmail(tc.email); got != tc.want {
				t.Fatalf("isValidEmail(%q)=%v want=%v", tc.email, got, tc.want)
			}
		})
	}
}
