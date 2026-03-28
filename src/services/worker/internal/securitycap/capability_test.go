package securitycap

import "testing"

func TestBuildRuntimeProvidesResolverAndScanner(t *testing.T) {
	runtime, err := BuildRuntime(RuntimeDeps{})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	if runtime.Resolver == nil {
		t.Fatal("BuildRuntime() resolver is nil")
	}
	if runtime.CompositeScanner == nil {
		t.Fatal("BuildRuntime() composite scanner is nil")
	}
}

func TestRuntimeMiddlewaresBundle(t *testing.T) {
	middlewares := Runtime{}.Middlewares(nil)
	if len(middlewares) != 2 {
		t.Fatalf("len(Middlewares()) = %d, want 2", len(middlewares))
	}
	for i, middleware := range middlewares {
		if middleware == nil {
			t.Fatalf("Middlewares()[%d] is nil", i)
		}
	}
}
