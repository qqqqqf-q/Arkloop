//go:build !desktop

package http

import (
	"encoding/json"
	"testing"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/personas"

	"github.com/google/uuid"
)

func TestToLiteAgentFromDBUsesDenylistPolicy(t *testing.T) {
	t.Parallel()

	persona := data.Persona{
		ID:           uuid.New(),
		PersonaKey:   "support",
		DisplayName:  "Support",
		PromptMD:     "prompt",
		ToolDenylist: []string{"document_write"},
		BudgetsJSON:  json.RawMessage("{}"),
		IsActive:     true,
		CreatedAt:    time.Unix(0, 0),
	}

	resp := toLiteAgentFromDB(persona)
	if resp.ToolPolicy != "denylist" {
		t.Fatalf("unexpected tool policy: %q", resp.ToolPolicy)
	}
	if len(resp.ToolDenylist) != 1 || resp.ToolDenylist[0] != "document_write" {
		t.Fatalf("unexpected tool_denylist: %#v", resp.ToolDenylist)
	}
	if len(resp.ToolAllowlist) != 0 {
		t.Fatalf("unexpected tool_allowlist: %#v", resp.ToolAllowlist)
	}
}

func TestToLiteAgentFromRepoIncludesDenylist(t *testing.T) {
	t.Parallel()

	repoPersona := personas.RepoPersona{
		ID:           "researcher",
		Title:        "Researcher",
		PromptMD:     "prompt",
		ToolDenylist: []string{"document_write"},
	}

	resp := toLiteAgentFromRepo(repoPersona)
	if resp.ToolPolicy != "denylist" {
		t.Fatalf("unexpected tool policy: %q", resp.ToolPolicy)
	}
	if len(resp.ToolDenylist) != 1 || resp.ToolDenylist[0] != "document_write" {
		t.Fatalf("unexpected tool_denylist: %#v", resp.ToolDenylist)
	}
}
