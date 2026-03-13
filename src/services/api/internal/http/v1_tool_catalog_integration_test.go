//go:build !desktop

package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	apiCrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedtoolmeta "arkloop/services/shared/toolmeta"

	"github.com/google/uuid"
)

func TestToolCatalogSupportsPlatformAndOrgOverrides(t *testing.T) {
	db := setupTestDatabase(t, "api_go_tool_catalog")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("cred repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh repo: %v", err)
	}
	orgRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("org repo: %v", err)
	}
	overridesRepo, err := data.NewToolDescriptionOverridesRepository(pool)
	if err != nil {
		t.Fatalf("tool description repo: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 5)
	}
	ring, err := apiCrypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	_ = ring
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	org, err := orgRepo.Create(ctx, "tool-catalog-org", "Tool Catalog Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	platformAdmin, err := userRepo.Create(ctx, "tool-admin", "tool-admin@test.com", "en")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, org.ID, platformAdmin.ID, auth.RolePlatformAdmin); err != nil {
		t.Fatalf("create admin membership: %v", err)
	}
	adminToken, err := tokenService.Issue(platformAdmin.ID, org.ID, auth.RolePlatformAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	listenerCtx, cancelListener := context.WithCancel(ctx)
	t.Cleanup(cancelListener)

	handler := NewHandler(HandlerConfig{
		Pool:                         pool,
		DirectPool:                   pool,
		InvalidationListenerCtx:      listenerCtx,
		Logger:                       logger,
		AuthService:                  authService,
		AccountMembershipRepo:            membershipRepo,
		ToolDescriptionOverridesRepo: overridesRepo,
	})

	listResp := doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog", nil, authHeader(adminToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list: %d %s", listResp.Code, listResp.Body.String())
	}
	catalog := decodeJSONBody[toolCatalogResponse](t, listResp.Body.Bytes())
	for _, groupName := range []string{"web_search", "web_fetch", "sandbox", "memory", "document", "orchestration"} {
		if _, ok := findCatalogGroup(catalog, groupName); !ok {
			t.Fatalf("missing group %s", groupName)
		}
	}
	if _, ok := findCatalogGroup(catalog, "internal"); ok {
		t.Fatal("internal group should be absent")
	}

	webSearch, ok := findCatalogTool(catalog, "web_search", "web_search")
	if !ok {
		t.Fatal("web_search tool missing")
	}
	if webSearch.Label != "Web search" {
		t.Fatalf("unexpected label: %s", webSearch.Label)
	}
	if webSearch.DescriptionSource != toolDescriptionSourceDefault {
		t.Fatalf("expected default source, got %s", webSearch.DescriptionSource)
	}
	if webSearch.HasOverride {
		t.Fatal("default tool should not be marked overridden")
	}
	if webSearch.LLMDescription != sharedtoolmeta.Must("web_search").LLMDescription {
		t.Fatal("unexpected default llm description")
	}

	platformOverride := map[string]any{"description": "platform override for web search"}
	putPlatform := doJSON(handler, nethttp.MethodPut, "/v1/tool-catalog/web_search/description", platformOverride, authHeader(adminToken))
	if putPlatform.Code != nethttp.StatusNoContent {
		t.Fatalf("put platform override: %d %s", putPlatform.Code, putPlatform.Body.String())
	}

	listPlatform := doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog", nil, authHeader(adminToken))
	platformCatalog := decodeJSONBody[toolCatalogResponse](t, listPlatform.Body.Bytes())
	webSearch, _ = findCatalogTool(platformCatalog, "web_search", "web_search")
	if webSearch.LLMDescription != "platform override for web search" {
		t.Fatalf("unexpected platform description: %s", webSearch.LLMDescription)
	}
	if !webSearch.HasOverride {
		t.Fatal("platform override should set has_override")
	}
	if webSearch.DescriptionSource != toolDescriptionSourcePlatform {
		t.Fatalf("expected platform source, got %s", webSearch.DescriptionSource)
	}

	listOrg := doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog?scope=project", nil, authHeader(adminToken))
	if listOrg.Code != nethttp.StatusOK {
		t.Fatalf("list project: %d %s", listOrg.Code, listOrg.Body.String())
	}
	orgCatalog := decodeJSONBody[toolCatalogResponse](t, listOrg.Body.Bytes())
	webSearch, _ = findCatalogTool(orgCatalog, "web_search", "web_search")
	if webSearch.LLMDescription != "platform override for web search" {
		t.Fatalf("project view should inherit platform override, got %s", webSearch.LLMDescription)
	}
	if webSearch.HasOverride {
		t.Fatal("project inherited description should not set has_override")
	}
	if webSearch.DescriptionSource != toolDescriptionSourcePlatform {
		t.Fatalf("expected platform source in project scope, got %s", webSearch.DescriptionSource)
	}

	orgOverride := map[string]any{"description": "project override for web search"}
	putOrg := doJSON(handler, nethttp.MethodPut, "/v1/tool-catalog/web_search/description?scope=project", orgOverride, authHeader(adminToken))
	if putOrg.Code != nethttp.StatusNoContent {
		t.Fatalf("put project override: %d %s", putOrg.Code, putOrg.Body.String())
	}

	listOrg = doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog?scope=project", nil, authHeader(adminToken))
	orgCatalog = decodeJSONBody[toolCatalogResponse](t, listOrg.Body.Bytes())
	webSearch, _ = findCatalogTool(orgCatalog, "web_search", "web_search")
	if webSearch.LLMDescription != "project override for web search" {
		t.Fatalf("unexpected org description: %s", webSearch.LLMDescription)
	}
	if !webSearch.HasOverride {
		t.Fatal("org override should set has_override")
	}
	if webSearch.DescriptionSource != toolDescriptionSourceProject {
		t.Fatalf("expected org source, got %s", webSearch.DescriptionSource)
	}

	deleteOrg := doJSON(handler, nethttp.MethodDelete, "/v1/tool-catalog/web_search/description?scope=project", nil, authHeader(adminToken))
	if deleteOrg.Code != nethttp.StatusNoContent {
		t.Fatalf("delete project override: %d %s", deleteOrg.Code, deleteOrg.Body.String())
	}

	listOrg = doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog?scope=project", nil, authHeader(adminToken))
	orgCatalog = decodeJSONBody[toolCatalogResponse](t, listOrg.Body.Bytes())
	webSearch, _ = findCatalogTool(orgCatalog, "web_search", "web_search")
	if webSearch.LLMDescription != "platform override for web search" {
		t.Fatalf("project reset should fall back to platform, got %s", webSearch.LLMDescription)
	}
	if webSearch.HasOverride {
		t.Fatal("project reset should clear has_override")
	}
	if webSearch.DescriptionSource != toolDescriptionSourcePlatform {
		t.Fatalf("expected platform source after org reset, got %s", webSearch.DescriptionSource)
	}

	unknown := doJSON(handler, nethttp.MethodPut, "/v1/tool-catalog/not_real/description", platformOverride, authHeader(adminToken))
	if unknown.Code != nethttp.StatusNotFound {
		t.Fatalf("unknown tool should be 404, got %d", unknown.Code)
	}

	disableDoc := doJSON(handler, nethttp.MethodPut, "/v1/tool-catalog/document_write/disabled", map[string]any{"disabled": true}, authHeader(adminToken))
	if disableDoc.Code != nethttp.StatusNoContent {
		t.Fatalf("disable document_write: %d %s", disableDoc.Code, disableDoc.Body.String())
	}

	listPlatform = doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog", nil, authHeader(adminToken))
	platformCatalog = decodeJSONBody[toolCatalogResponse](t, listPlatform.Body.Bytes())
	documentWrite, ok := findCatalogTool(platformCatalog, "document", "document_write")
	if !ok {
		t.Fatal("document_write should still be visible in management catalog")
	}
	if !documentWrite.IsDisabled {
		t.Fatal("document_write should be marked disabled")
	}
}

func TestToolCatalogScopePermissions(t *testing.T) {
	db := setupTestDatabase(t, "api_go_tool_catalog_perms")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)
	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("cred repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("refresh repo: %v", err)
	}
	orgRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("org repo: %v", err)
	}
	overridesRepo, err := data.NewToolDescriptionOverridesRepository(pool)
	if err != nil {
		t.Fatalf("tool description repo: %v", err)
	}
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	org, err := orgRepo.Create(ctx, "tool-catalog-org-member", "Tool Catalog Org Member", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	member, err := userRepo.Create(ctx, "tool-member", "tool-member@test.com", "en")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, org.ID, member.ID, auth.RoleAccountMember); err != nil {
		t.Fatalf("create member membership: %v", err)
	}
	memberToken, err := tokenService.Issue(member.ID, org.ID, auth.RoleAccountMember, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue member token: %v", err)
	}
	listenerCtx, cancelListener := context.WithCancel(ctx)
	t.Cleanup(cancelListener)

	handler := NewHandler(HandlerConfig{
		Pool:                         pool,
		DirectPool:                   pool,
		InvalidationListenerCtx:      listenerCtx,
		Logger:                       logger,
		AuthService:                  authService,
		AccountMembershipRepo:            membershipRepo,
		ToolDescriptionOverridesRepo: overridesRepo,
	})

	platformResp := doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog", nil, authHeader(memberToken))
	if platformResp.Code != nethttp.StatusForbidden {
		t.Fatalf("account member platform scope should be 403, got %d", platformResp.Code)
	}

	orgResp := doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog?scope=project", nil, authHeader(memberToken))
	if orgResp.Code != nethttp.StatusForbidden {
		t.Fatalf("account member project scope should be 403, got %d", orgResp.Code)
	}
}

func TestEffectiveToolCatalogIncludesConditionalAndMCPTools(t *testing.T) {
	db := setupTestDatabase(t, "api_go_tool_catalog_effective")
	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)
	userRepo, _ := data.NewUserRepository(pool)
	credRepo, _ := data.NewUserCredentialRepository(pool)
	membershipRepo, _ := data.NewAccountMembershipRepository(pool)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(pool)
	mcpRepo, _ := data.NewMCPConfigsRepository(pool)
	toolProvidersRepo, _ := data.NewToolProviderConfigsRepository(pool)
	overridesRepo, _ := data.NewToolDescriptionOverridesRepository(pool)
	orgRepo, _ := data.NewAccountRepository(pool)
	passwordHasher, _ := auth.NewBcryptPasswordHasher(0)
	tokenService, _ := auth.NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 2592000)
	authService, _ := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)

	mcpServer := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method, _ := body["method"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result": map[string]any{
				"tools": []map[string]any{{
					"name":        method + "_tool",
					"title":       "Docs Lookup",
					"description": "lookup docs",
				}},
			},
		})
	}))
	defer mcpServer.Close()

	t.Setenv("ARKLOOP_SANDBOX_BASE_URL", "http://sandbox.internal")
	t.Setenv("ARKLOOP_OPENVIKING_BASE_URL", "http://memory.internal")
	t.Setenv("ARKLOOP_OPENVIKING_ROOT_API_KEY", "memory-root-key")
	t.Setenv("ARKLOOP_S3_ENDPOINT", "http://seaweedfs.internal")

	envCfgDir := t.TempDir()
	envCfgPath := filepath.Join(envCfgDir, "mcp.config.json")
	if err := os.WriteFile(envCfgPath, []byte(`{"mcpServers":{"env-demo":{"transport":"streamable_http","url":"`+mcpServer.URL+`"}}}`), 0o644); err != nil {
		t.Fatalf("write env mcp config: %v", err)
	}
	t.Setenv("ARKLOOP_MCP_CONFIG_FILE", envCfgPath)

	org, err := orgRepo.Create(ctx, "effective-tool-catalog-org", "Effective Tool Catalog Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	accountID := org.ID
	user, err := userRepo.Create(ctx, "effective-user", "effective@test.com", "en")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := membershipRepo.Create(ctx, accountID, user.ID, auth.RoleAccountAdmin); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	token, err := tokenService.Issue(user.ID, accountID, auth.RoleAccountAdmin, time.Now().UTC())
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	if _, err := mcpRepo.Create(ctx, accountID, "org-demo", "streamable_http", strPtrCatalogTest(mcpServer.URL), nil, nil, nil, nil, nil, false, 3000); err != nil {
		t.Fatalf("create org mcp config: %v", err)
	}
	listenerCtx, cancelListener := context.WithCancel(ctx)
	t.Cleanup(cancelListener)

	handler := NewHandler(HandlerConfig{
		Pool:                         pool,
		DirectPool:                   pool,
		InvalidationListenerCtx:      listenerCtx,
		Logger:                       logger,
		AuthService:                  authService,
		AccountMembershipRepo:            membershipRepo,
		ToolProviderConfigsRepo:      toolProvidersRepo,
		ToolDescriptionOverridesRepo: overridesRepo,
		ArtifactStore:                newFakeHTTPArtifactStore(),
	})

	resp := doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog/effective", nil, authHeader(token))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("effective catalog: %d %s", resp.Code, resp.Body.String())
	}
	catalog := decodeJSONBody[toolCatalogResponse](t, resp.Body.Bytes())
	for _, toolName := range []struct{ group, name string }{
		{group: "sandbox", name: "exec_command"},
		{group: "memory", name: "memory_search"},
		{group: "document", name: "document_write"},
		{group: "mcp", name: "mcp__env_demo__tools_list_tool"},
		{group: "mcp", name: "mcp__org_demo__tools_list_tool"},
	} {
		if _, ok := findCatalogTool(catalog, toolName.group, toolName.name); !ok {
			t.Fatalf("missing effective tool %s/%s", toolName.group, toolName.name)
		}
	}
	item, ok := findCatalogTool(catalog, "mcp", "mcp__env_demo__tools_list_tool")
	if !ok {
		t.Fatal("expected env mcp tool")
	}
	if item.Label != "Docs Lookup" {
		t.Fatalf("unexpected mcp label: %s", item.Label)
	}

	if err := overridesRepo.SetDisabled(ctx, "document_write", true); err != nil {
		t.Fatalf("disable document_write: %v", err)
	}

	resp = doJSON(handler, nethttp.MethodGet, "/v1/tool-catalog/effective", nil, authHeader(token))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("effective catalog after disable: %d %s", resp.Code, resp.Body.String())
	}
	catalog = decodeJSONBody[toolCatalogResponse](t, resp.Body.Bytes())
	if _, ok := findCatalogTool(catalog, "document", "document_write"); ok {
		t.Fatal("document_write should be hidden after disable")
	}
}

func TestBuildEffectiveToolCatalogOmitsDocumentWriteWithoutArtifactStore(t *testing.T) {
	t.Setenv("ARKLOOP_S3_ENDPOINT", "http://seaweedfs.internal")

	catalog, err := buildEffectiveToolCatalog(context.Background(), uuid.Nil, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("build effective tool catalog: %v", err)
	}
	if _, ok := findCatalogTool(catalog, "document", "document_write"); ok {
		t.Fatal("document_write should be absent without artifact store")
	}
}

func findCatalogGroup(resp toolCatalogResponse, groupName string) (toolCatalogGroup, bool) {
	for _, group := range resp.Groups {
		if group.Group == groupName {
			return group, true
		}
	}
	return toolCatalogGroup{}, false
}

func findCatalogTool(resp toolCatalogResponse, groupName string, toolName string) (toolCatalogItem, bool) {
	group, ok := findCatalogGroup(resp, groupName)
	if !ok {
		return toolCatalogItem{}, false
	}
	for _, tool := range group.Tools {
		if tool.Name == toolName {
			return tool, true
		}
	}
	return toolCatalogItem{}, false
}

func strPtrCatalogTest(value string) *string {
	return &value
}
