package catalogapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	repopersonas "arkloop/services/api/internal/personas"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Deps struct {
	AuthService                  *auth.Service
	AccountMembershipRepo            *data.AccountMembershipRepository
	LlmCredentialsRepo           *data.LlmCredentialsRepository
	LlmRoutesRepo                *data.LlmRoutesRepository
	SecretsRepo                  *data.SecretsRepository
	Pool                         *pgxpool.Pool
	DirectPool                   *pgxpool.Pool
	AsrCredentialsRepo           *data.AsrCredentialsRepository
	MCPConfigsRepo               *data.MCPConfigsRepository
	ToolProviderConfigsRepo      *data.ToolProviderConfigsRepository
	ToolDescriptionOverridesRepo *data.ToolDescriptionOverridesRepository
	PersonasRepo                 *data.PersonasRepository
	SkillPackagesRepo            *data.SkillPackagesRepository
	ProfileSkillInstallsRepo     *data.ProfileSkillInstallsRepository
	WorkspaceSkillEnableRepo     *data.WorkspaceSkillEnablementsRepository
	ProfileRegistriesRepo        *data.ProfileRegistriesRepository
	WorkspaceRegistriesRepo      *data.WorkspaceRegistriesRepository
	PlatformSettingsRepo         *data.PlatformSettingsRepository
	APIKeysRepo                  *data.APIKeysRepository
	ProjectRepo                  *data.ProjectRepository
	AuditWriter                  *audit.Writer
	SkillStore                   skillStore
	RepoPersonas                 []repopersonas.RepoPersona
	PersonaSyncTrigger           personaSyncTrigger
	EffectiveToolCatalogCache    *EffectiveToolCatalogCache
	ArtifactStoreAvailable       bool
}

type personaSyncTrigger interface {
	Trigger()
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/llm-providers", llmProvidersEntry(deps.AuthService, deps.AccountMembershipRepo, deps.LlmCredentialsRepo, deps.LlmRoutesRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/llm-providers/", llmProviderEntry(deps.AuthService, deps.AccountMembershipRepo, deps.LlmCredentialsRepo, deps.LlmRoutesRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/asr-credentials", asrCredentialsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AsrCredentialsRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/asr-credentials/", asrCredentialEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AsrCredentialsRepo))
	mux.HandleFunc("/v1/asr/transcribe", asrTranscribeEntry(deps.AuthService, deps.AccountMembershipRepo, deps.AsrCredentialsRepo, deps.SecretsRepo))
	mux.HandleFunc("/v1/mcp-configs", mcpConfigsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.MCPConfigsRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/mcp-configs/", mcpConfigEntry(deps.AuthService, deps.AccountMembershipRepo, deps.MCPConfigsRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/tool-catalog/effective", toolCatalogEffectiveEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ToolDescriptionOverridesRepo, deps.Pool, deps.EffectiveToolCatalogCache, deps.ArtifactStoreAvailable, deps.ProjectRepo))
	mux.HandleFunc("/v1/tool-catalog", toolCatalogEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ToolDescriptionOverridesRepo, deps.ProjectRepo))
	mux.HandleFunc("/v1/tool-catalog/", toolCatalogItemEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ToolDescriptionOverridesRepo, deps.ProjectRepo))
	mux.HandleFunc("/v1/tool-providers", toolProvidersEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ToolProviderConfigsRepo, deps.SecretsRepo, deps.Pool, deps.DirectPool, deps.ProjectRepo))
	mux.HandleFunc("/v1/tool-providers/", toolProviderEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ToolProviderConfigsRepo, deps.SecretsRepo, deps.Pool, deps.DirectPool, deps.ProjectRepo))
	mux.HandleFunc("/v1/skill-packages", skillPackagesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/", skillPackageEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo))
	mux.HandleFunc("/v1/skill-packages/import/github", githubSkillImportEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/import/upload", uploadSkillImportEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/market/skills", marketSkillsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.PlatformSettingsRepo, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo, deps.WorkspaceSkillEnableRepo))
	mux.HandleFunc("/v1/market/skills/import", marketSkillsImportEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.PlatformSettingsRepo, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/import/registry", marketSkillsImportEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.PlatformSettingsRepo, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/import/skillsmp", marketSkillsImportEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.PlatformSettingsRepo, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/profiles/me/skills", profileSkillsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo))
	mux.HandleFunc("/v1/profiles/me/skills/", profileSkillEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo))
	mux.HandleFunc("/v1/profiles/me/default-skills", profileDefaultSkillsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.WorkspaceSkillEnableRepo, deps.ProfileRegistriesRepo, deps.WorkspaceRegistriesRepo, deps.Pool))
	mux.HandleFunc("/v1/me/selectable-personas", selectablePersonasEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PersonasRepo, deps.RepoPersonas, deps.ProjectRepo))
	mux.HandleFunc("/v1/workspaces/", workspaceSkillsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.WorkspaceSkillEnableRepo, deps.WorkspaceRegistriesRepo, deps.ProfileRegistriesRepo, deps.Pool))
	mux.HandleFunc("/v1/personas", personasEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PersonasRepo, deps.RepoPersonas, deps.PersonaSyncTrigger, deps.ProjectRepo))
	mux.HandleFunc("/v1/personas/", personaEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PersonasRepo, deps.PersonaSyncTrigger, deps.ProjectRepo))
	mux.HandleFunc("/v1/admin/skill-packages", adminSkillPackagesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/profiles/me/skills/install", profileSkillsInstallEntry(deps.AuthService, deps.AccountMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo))
	mux.HandleFunc("/v1/lite/agents", liteAgentsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PersonasRepo, deps.RepoPersonas, deps.PersonaSyncTrigger))
	mux.HandleFunc("/v1/lite/agents/", liteAgentEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PersonasRepo, deps.PersonaSyncTrigger))
}
