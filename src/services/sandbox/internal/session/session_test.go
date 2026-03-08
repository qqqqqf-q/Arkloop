package session

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

func TestEnsureEnvironmentProtocolRejectsOutdatedAgent(t *testing.T) {
	s := &Session{AgentImage: "arkloop/sandbox-agent:old", Dial: singleResponseDialer(agentResponse{Action: "agent_capabilities", Error: "unknown action: agent_capabilities"})}
	err := s.EnsureEnvironmentProtocol(context.Background())
	if err == nil {
		t.Fatal("expected protocol error")
	}
	if !strings.Contains(err.Error(), "arkloop/sandbox-agent:old") || !strings.Contains(err.Error(), "outdated") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureEnvironmentProtocolAcceptsRequiredActions(t *testing.T) {
	s := &Session{AgentImage: "arkloop/sandbox-agent:dev", Dial: singleResponseDialer(agentResponse{Action: "agent_capabilities", Capabilities: &AgentCapabilities{ProtocolVersion: 1, EnvironmentActions: []string{"environment_manifest_build", "environment_files_collect", "environment_apply"}}})}
	if err := s.EnsureEnvironmentProtocol(context.Background()); err != nil {
		t.Fatalf("ensure environment protocol: %v", err)
	}
}

func singleResponseDialer(resp agentResponse) Dialer {
	return func(ctx context.Context) (net.Conn, error) {
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			var req agentRequest
			_ = json.NewDecoder(server).Decode(&req)
			_ = json.NewEncoder(server).Encode(resp)
		}()
		return client, nil
	}
}
