package coerce

import (
	"testing"
)

func TestBool(t *testing.T) {
	tests := []struct {
		input any
		want  bool
		ok    bool
	}{
		{true, true, true},
		{false, false, true},
		{"true", true, true},
		{"false", false, true},
		{"True", true, true},
		{"FALSE", false, true},
		{" true ", true, true},
		{"yes", false, false},
		{1, false, false},
		{nil, false, false},
	}
	for _, tt := range tests {
		got, err := Bool(tt.input)
		if tt.ok {
			if err != nil {
				t.Errorf("Bool(%v): unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Bool(%v) = %v, want %v", tt.input, got, tt.want)
			}
		} else if err == nil {
			t.Errorf("Bool(%v): expected error, got %v", tt.input, got)
		}
	}
}

func TestInt(t *testing.T) {
	tests := []struct {
		input any
		want  int
		ok    bool
	}{
		{42, 42, true},
		{float64(123), 123, true},
		{float64(-5), -5, true},
		{"456", 456, true},
		{" 789 ", 789, true},
		{float64(1.5), 0, false},
		{"abc", 0, false},
		{true, 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, err := Int(tt.input)
		if tt.ok {
			if err != nil {
				t.Errorf("Int(%v): unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Int(%v) = %v, want %v", tt.input, got, tt.want)
			}
		} else if err == nil {
			t.Errorf("Int(%v): expected error, got %v", tt.input, got)
		}
	}
}

func TestFloat(t *testing.T) {
	tests := []struct {
		input any
		want  float64
		ok    bool
	}{
		{float64(1.5), 1.5, true},
		{3, 3.0, true},
		{"2.5", 2.5, true},
		{" 3.14 ", 3.14, true},
		{"abc", 0, false},
		{true, 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, err := Float(tt.input)
		if tt.ok {
			if err != nil {
				t.Errorf("Float(%v): unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Float(%v) = %v, want %v", tt.input, got, tt.want)
			}
		} else if err == nil {
			t.Errorf("Float(%v): expected error, got %v", tt.input, got)
		}
	}
}
