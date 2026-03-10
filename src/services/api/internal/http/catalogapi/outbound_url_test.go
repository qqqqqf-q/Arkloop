package catalogapi

import "testing"

func TestNormalizeOptionalInternalBaseURL(t *testing.T) {
	raw := " http://openviking:1933/api/ "
	normalized, err := normalizeOptionalInternalBaseURL(&raw)
	if err != nil {
		t.Fatalf("normalizeOptionalInternalBaseURL() error = %v", err)
	}
	if normalized == nil {
		t.Fatal("expected normalized base URL")
	}
	if *normalized != "http://openviking:1933/api" {
		t.Fatalf("unexpected normalized base URL: %q", *normalized)
	}
}

func TestNormalizeOptionalBaseURLRejectsInsecureHTTP(t *testing.T) {
	raw := " http://openviking:1933/api/ "
	if _, err := normalizeOptionalBaseURL(&raw); err == nil {
		t.Fatal("expected insecure HTTP to be rejected by outbound policy")
	}
}
