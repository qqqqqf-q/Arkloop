package llm

import (
	"context"
	"testing"
)

func TestAuxGatewayConfigFromEnv_DefaultDisabled(t *testing.T) {
	cfg, err := AuxGatewayConfigFromEnv()
	if err != nil {
		t.Fatalf("AuxGatewayConfigFromEnv failed: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("stub gateway must be disabled unless explicitly enabled")
	}
}

func TestAuxGatewayConfigFromEnv_ExplicitEnable(t *testing.T) {
	t.Setenv(stubEnabledEnv, "true")

	cfg, err := AuxGatewayConfigFromEnv()
	if err != nil {
		t.Fatalf("AuxGatewayConfigFromEnv failed: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected explicit env to enable stub gateway")
	}
}

func TestAuxGatewayDisabledDoesNotEmitStubDeltas(t *testing.T) {
	gateway := NewAuxGateway(AuxGatewayConfig{Enabled: false, DeltaCount: 3})

	var deltas int
	var failed bool
	err := gateway.Stream(context.Background(), Request{}, func(event StreamEvent) error {
		switch event.(type) {
		case StreamMessageDelta:
			deltas++
		case StreamRunFailed:
			failed = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if deltas != 0 {
		t.Fatalf("expected no stub deltas, got %d", deltas)
	}
	if !failed {
		t.Fatal("expected disabled stub gateway to emit run failure")
	}
}
