//go:build desktop

package acptool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/acp"
	workerCrypto "arkloop/services/worker/internal/crypto"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DesktopExecutorWithInject returns base with InjectDesktopDelegateEnv bound to db.
func DesktopExecutorWithInject(base ToolExecutor, db data.DesktopDB) ToolExecutor {
	if db == nil {
		return base
	}
	base.InjectDesktopDelegateEnv = func(ctx context.Context, execCtx tools.ExecutionContext, invocation acp.ResolvedInvocation, env map[string]string) *tools.ExecutionError {
		return injectDesktopDelegateFromDB(ctx, db, execCtx, invocation, env)
	}
	return base
}

func injectDesktopDelegateFromDB(
	ctx context.Context,
	db data.DesktopDB,
	execCtx tools.ExecutionContext,
	invocation acp.ResolvedInvocation,
	env map[string]string,
) *tools.ExecutionError {
	if invocation.Provider.AuthStrategy != acp.AuthStrategyProviderNative {
		return nil
	}
	acpCfg, ok := execCtx.ActiveToolProviderConfigsByGroup[acp.ProviderGroupACP]
	if !ok {
		return nil
	}
	sel, _ := acpCfg.ConfigJSON["delegate_model_selector"].(string)
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return nil
	}
	credID, modelName, ok := splitDelegateSelector(sel)
	if !ok {
		return &tools.ExecutionError{ErrorClass: "tool.config_invalid", Message: fmt.Sprintf("invalid delegate_model_selector %q", sel)}
	}
	return applyCredentialModelToEnv(ctx, db, credID, modelName, env)
}

func splitDelegateSelector(sel string) (credentialID uuid.UUID, model string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(sel), "^", 2)
	if len(parts) != 2 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(strings.TrimSpace(parts[0]))
	if err != nil {
		return uuid.Nil, "", false
	}
	model = strings.TrimSpace(parts[1])
	if model == "" {
		return uuid.Nil, "", false
	}
	return id, model, true
}

func applyCredentialModelToEnv(ctx context.Context, db data.DesktopDB, credID uuid.UUID, modelName string, env map[string]string) *tools.ExecutionError {
	row := db.QueryRow(ctx, `
		SELECT c.provider, c.base_url, s.encrypted_value, s.key_version
		FROM llm_credentials c
		LEFT JOIN secrets s ON s.id = c.secret_id
		WHERE c.id = $1 AND c.revoked_at IS NULL
		LIMIT 1`, credID.String(),
	)
	var provider, baseURL *string
	var enc *string
	var keyVersion *int
	if err := row.Scan(&provider, &baseURL, &enc, &keyVersion); err != nil {
		if err == pgx.ErrNoRows {
			return &tools.ExecutionError{ErrorClass: "tool.config_invalid", Message: "llm credential not found for delegate_model_selector"}
		}
		return &tools.ExecutionError{ErrorClass: "tool.execution_failed", Message: fmt.Sprintf("load credential: %v", err)}
	}
	if enc == nil || strings.TrimSpace(*enc) == "" || keyVersion == nil {
		return &tools.ExecutionError{ErrorClass: "tool.config_invalid", Message: "llm credential has no secret"}
	}
	plain, err := workerCrypto.DecryptWithKeyVersion(*enc, *keyVersion)
	if err != nil {
		return &tools.ExecutionError{ErrorClass: "tool.execution_failed", Message: fmt.Sprintf("decrypt credential: %v", err)}
	}
	key := string(plain)
	pv := strings.TrimSpace(stringOrEmpty(provider))
	switch pv {
	case "anthropic":
		env["ANTHROPIC_API_KEY"] = key
		if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
			env["ANTHROPIC_BASE_URL"] = sharedoutbound.NormalizeAnthropicCompatibleBaseURL(*baseURL)
		}
	case "openai", "deepseek":
		env["OPENAI_API_KEY"] = key
		if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
			env["OPENAI_BASE_URL"] = strings.TrimRight(strings.TrimSpace(*baseURL), "/")
		}
	case "gemini":
		env["GEMINI_API_KEY"] = key
		env["GOOGLE_API_KEY"] = key
	default:
		env["OPENAI_API_KEY"] = key
		if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
			env["OPENAI_BASE_URL"] = strings.TrimRight(strings.TrimSpace(*baseURL), "/")
		}
	}
	mergeOpenCodeModelEnv(env, modelName)
	return nil
}

func stringOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mergeOpenCodeModelEnv(env map[string]string, model string) {
	if env == nil || strings.TrimSpace(model) == "" {
		return
	}
	raw := env["OPENCODE_CONFIG_CONTENT"]
	var cur map[string]any
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cur)
	}
	if cur == nil {
		cur = map[string]any{}
	}
	cur["model"] = model
	b, err := json.Marshal(cur)
	if err != nil {
		return
	}
	env["OPENCODE_CONFIG_CONTENT"] = string(b)
}
