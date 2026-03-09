package runtime

import (
	"testing"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

func TestMemoryProviderFactory_ReusesBySignature(t *testing.T) {
	factory := NewMemoryProviderFactory()
	snapshot := sharedtoolruntime.RuntimeSnapshot{
		MemoryBaseURL:    "http://memory.internal",
		MemoryRootAPIKey: "key-1",
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
		MemoryBaseURL:    "http://memory.internal",
		MemoryRootAPIKey: "key-2",
	})
	if third == nil {
		t.Fatal("expected provider for new signature")
	}
	if third == first {
		t.Fatal("expected provider to switch after signature change")
	}
}
