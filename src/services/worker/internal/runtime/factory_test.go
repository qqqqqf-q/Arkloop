//go:build !desktop

package runtime

import (
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

func TestMemoryProviderFactory_ReusesBySignature(t *testing.T) {
	factory := NewMemoryProviderFactory()
	snapshot := sharedtoolruntime.RuntimeSnapshot{
		MemoryProvider: "nowledge",
		MemoryBaseURL:  "http://memory.internal",
		MemoryAPIKey:   "key-1",
	}
	first := factory.Resolve(snapshot)
	second := factory.Resolve(snapshot)
	if first == nil || second == nil {
		t.Fatal("expected provider to be created")
	}
	if first != second {
		t.Fatal("expected provider to be reused for identical signature")
	}
	third := factory.Resolve(sharedtoolruntime.RuntimeSnapshot{
		MemoryProvider: "nowledge",
		MemoryBaseURL:  "http://memory.internal",
		MemoryAPIKey:   "key-2",
	})
	if third == nil {
		t.Fatal("expected provider for new signature")
	}
	if third == first {
		t.Fatal("expected provider to switch after signature change")
	}
}

func TestMemoryProviderFactory_URLWithoutKey(t *testing.T) {
	factory := NewMemoryProviderFactory()
	snapshot := sharedtoolruntime.RuntimeSnapshot{
		MemoryProvider: "nowledge",
		MemoryBaseURL:  "http://memory.internal",
		MemoryAPIKey:   "",
	}
	p := factory.Resolve(snapshot)
	if p == nil {
		t.Fatal("expected provider with base URL only")
	}
	again := factory.Resolve(snapshot)
	if again != p {
		t.Fatal("expected same cached provider")
	}
}

func TestMemoryProviderFactory_UnknownProviderReturnsNil(t *testing.T) {
	factory := NewMemoryProviderFactory()
	if p := factory.Resolve(sharedtoolruntime.RuntimeSnapshot{
		MemoryProvider: "unknown",
		MemoryBaseURL:  "http://memory.internal",
	}); p != nil {
		t.Fatal("expected nil provider for unknown backend")
	}
}
