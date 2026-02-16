package queue

import (
	"fmt"
	"testing"
)

func TestDefaultRetryDelaySeconds(t *testing.T) {
	cases := []struct {
		attempts int
		want     int
	}{
		{attempts: 0, want: 1},
		{attempts: 1, want: 1},
		{attempts: 2, want: 2},
		{attempts: 3, want: 4},
		{attempts: 4, want: 8},
		{attempts: 5, want: 16},
		{attempts: 6, want: 30},
		{attempts: 30, want: 30},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("attempts_%d", tc.attempts), func(t *testing.T) {
			got := DefaultRetryDelaySeconds(tc.attempts)
			if got != tc.want {
				t.Fatalf("attempts=%d got=%d want=%d", tc.attempts, got, tc.want)
			}
		})
	}
}
