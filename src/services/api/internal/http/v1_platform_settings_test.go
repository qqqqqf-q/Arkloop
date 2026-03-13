//go:build !desktop

package http

import (
	"testing"

	sharedconfig "arkloop/services/shared/config"
)

func TestMaskIfSensitiveMasksNonEmptyValue(t *testing.T) {
	reg := sharedconfig.NewRegistry()
	if err := reg.Register(sharedconfig.Entry{
		Key:       "secret.k",
		Type:      sharedconfig.TypeString,
		Default:   "",
		Scope:     sharedconfig.ScopePlatform,
		Sensitive: true,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	got := maskIfSensitive("secret.k", "abc", reg)
	if got != maskedSensitiveValue {
		t.Fatalf("expected masked value, got %q", got)
	}
}

func TestMaskIfSensitiveKeepsEmptyValue(t *testing.T) {
	reg := sharedconfig.NewRegistry()
	if err := reg.Register(sharedconfig.Entry{
		Key:       "secret.k",
		Type:      sharedconfig.TypeString,
		Default:   "",
		Scope:     sharedconfig.ScopePlatform,
		Sensitive: true,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	got := maskIfSensitive("secret.k", "", reg)
	if got != "" {
		t.Fatalf("expected empty value, got %q", got)
	}
}

func TestMaskIfSensitiveUnknownKeyUnchanged(t *testing.T) {
	reg := sharedconfig.NewRegistry()
	got := maskIfSensitive("unknown", "v", reg)
	if got != "v" {
		t.Fatalf("expected original value, got %q", got)
	}
}
