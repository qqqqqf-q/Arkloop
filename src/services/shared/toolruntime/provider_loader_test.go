package toolruntime

import "testing"

func TestEvaluateProviderRuntimeStatusNowledge(t *testing.T) {
	state, reason := evaluateProviderRuntimeStatus(ProviderRuntimeStatus{
		GroupName:    "memory",
		ProviderName: "memory.nowledge",
		BaseURL:      strPtr("http://nowledge.internal"),
		APIKeyValue:  strPtr("nowledge-key"),
	})
	if state != ProviderRuntimeStateReady || reason != "" {
		t.Fatalf("expected ready status, got %s %q", state, reason)
	}

	state, reason = evaluateProviderRuntimeStatus(ProviderRuntimeStatus{
		GroupName:    "memory",
		ProviderName: "memory.nowledge",
		BaseURL:      strPtr("http://nowledge.internal"),
	})
	if state != ProviderRuntimeStateMissingConfig || reason != "missing_api_key" {
		t.Fatalf("expected missing api key, got %s %q", state, reason)
	}
}

func TestEvaluateProviderRuntimeStatusExa(t *testing.T) {
	state, reason := evaluateProviderRuntimeStatus(ProviderRuntimeStatus{
		GroupName:    "web_search",
		ProviderName: "web_search.exa",
	})
	if state != ProviderRuntimeStateReady || reason != "" {
		t.Fatalf("expected ready status, got %s %q", state, reason)
	}

	state, reason = evaluateProviderRuntimeStatus(ProviderRuntimeStatus{
		GroupName:    "web_search",
		ProviderName: "web_search.exa",
		APIKeyValue:  strPtr("legacy-exa-key"),
		BaseURL:      strPtr("ftp://legacy.exa.local"),
	})
	if state != ProviderRuntimeStateReady || reason != "" {
		t.Fatalf("expected legacy credential ignored, got %s %q", state, reason)
	}
}
