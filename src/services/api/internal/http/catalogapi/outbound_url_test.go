package catalogapi

import "testing"

func TestNormalizeOptionalInternalBaseURL(t *testing.T) {
	raw := " http://internal-svc:8080/api/ "
	normalized, err := normalizeOptionalInternalBaseURL(&raw)
	if err != nil {
		t.Fatalf("normalizeOptionalInternalBaseURL() error = %v", err)
	}
	if normalized == nil {
		t.Fatal("expected normalized base URL")
	}
	if *normalized != "http://internal-svc:8080/api" {
		t.Fatalf("unexpected normalized base URL: %q", *normalized)
	}
}

func TestNormalizeOptionalBaseURLAllowsInsecureHTTP(t *testing.T) {
	raw := " http://internal-svc:8080/api/ "
	normalized, err := normalizeOptionalBaseURL(&raw)
	if err != nil {
		t.Fatalf("normalizeOptionalBaseURL() error = %v", err)
	}
	if normalized == nil {
		t.Fatal("expected normalized base URL")
	}
	if *normalized != "http://internal-svc:8080/api" {
		t.Fatalf("unexpected normalized base URL: %q", *normalized)
	}
}
