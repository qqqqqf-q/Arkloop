package pipeline

import "testing"

func TestApplyContextCompactPressureUsesRealAnchorPlusDelta(t *testing.T) {
	anchor := ContextCompactPressureAnchor{
		LastRealPromptTokens:             120_000,
		LastRequestContextEstimateTokens: 115_000,
	}
	if pressure := ApplyContextCompactPressure(anchor, 125_000); pressure != 130_000 {
		t.Fatalf("unexpected pressure: %d", pressure)
	}
}

func TestApplyContextCompactPressureAllowsPressureToDrop(t *testing.T) {
	anchor := ContextCompactPressureAnchor{
		LastRealPromptTokens:             120_000,
		LastRequestContextEstimateTokens: 115_000,
	}
	if pressure := ApplyContextCompactPressure(anchor, 90_000); pressure != 95_000 {
		t.Fatalf("unexpected dropped pressure: %d", pressure)
	}
}

func TestApplyContextCompactPressureFallsBackWithoutAnchor(t *testing.T) {
	if pressure := ApplyContextCompactPressure(ContextCompactPressureAnchor{}, 20_000); pressure != 20_000 {
		t.Fatalf("unexpected fallback pressure: %d", pressure)
	}
}
