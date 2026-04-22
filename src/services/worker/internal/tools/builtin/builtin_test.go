package builtin

import "testing"

func TestHeartbeatDecisionNotInGlobalToolSets(t *testing.T) {
	const toolName = "heartbeat_decision"

	for _, spec := range AgentSpecs() {
		if spec.Name == toolName {
			t.Fatalf("heartbeat tool should not be globally registered in agent specs")
		}
	}

	for _, spec := range LlmSpecs() {
		if spec.Name == toolName {
			t.Fatalf("heartbeat tool should not be globally exposed in llm specs")
		}
	}

	execs, _ := Executors(nil, nil, nil, nil)
	for name := range execs {
		if name == toolName {
			t.Fatalf("heartbeat tool should not be globally bound in executors")
		}
	}
}
