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
	OrgMembershipRepo            *data.OrgMembershipRepository
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
	mux.HandleFunc("/v1/llm-providers", llmProvidersEntry(deps.AuthService, deps.OrgMembershipRepo, deps.LlmCredentialsRepo, deps.LlmRoutesRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/llm-providers/", llmProviderEntry(deps.AuthService, deps.OrgMembershipRepo, deps.LlmCredentialsRepo, deps.LlmRoutesRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/asr-credentials", asrCredentialsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.AsrCredentialsRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/asr-credentials/", asrCredentialEntry(deps.AuthService, deps.OrgMembershipRepo, deps.AsrCredentialsRepo))
	mux.HandleFunc("/v1/asr/transcribe", asrTranscribeEntry(deps.AuthService, deps.OrgMembershipRepo, deps.AsrCredentialsRepo, deps.SecretsRepo))
	mux.HandleFunc("/v1/mcp-configs", mcpConfigsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.MCPConfigsRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/mcp-configs/", mcpConfigEntry(deps.AuthService, deps.OrgMembershipRepo, deps.MCPConfigsRepo, deps.SecretsRepo, deps.Pool))
	mux.HandleFunc("/v1/tool-catalog/effective", toolCatalogEffectiveEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ToolDescriptionOverridesRepo, deps.Pool, deps.EffectiveToolCatalogCache, deps.ArtifactStoreAvailable))
	mux.HandleFunc("/v1/tool-catalog", toolCatalogEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ToolDescriptionOverridesRepo))
	mux.HandleFunc("/v1/tool-catalog/", toolCatalogItemEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ToolDescriptionOverridesRepo))
	mux.HandleFunc("/v1/tool-providers", toolProvidersEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ToolProviderConfigsRepo, deps.SecretsRepo, deps.Pool, deps.DirectPool))
	mux.HandleFunc("/v1/tool-providers/", toolProviderEntry(deps.AuthService, deps.OrgMembershipRepo, deps.ToolProviderConfigsRepo, deps.SecretsRepo, deps.Pool, deps.DirectPool))
	mux.HandleFunc("/v1/skill-packages", skillPackagesEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/", skillPackageEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo))
	mux.HandleFunc("/v1/skill-packages/import/github", githubSkillImportEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/import/upload", uploadSkillImportEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/market/skills", marketSkillsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.PlatformSettingsRepo, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo, deps.WorkspaceSkillEnableRepo))
	mux.HandleFunc("/v1/market/skills/import", marketSkillsImportEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/skill-packages/import/skillsmp", marketSkillsImportEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/profiles/me/skills", profileSkillsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo))
	mux.HandleFunc("/v1/profiles/me/skills/", profileSkillEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo))
	mux.HandleFunc("/v1/profiles/me/default-skills", profileDefaultSkillsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.WorkspaceSkillEnableRepo, deps.ProfileRegistriesRepo, deps.WorkspaceRegistriesRepo, deps.Pool))
	mux.HandleFunc("/v1/me/selectable-personas", selectablePersonasEntry(deps.AuthService, deps.OrgMembershipRepo, deps.PersonasRepo, deps.RepoPersonas))
	mux.HandleFunc("/v1/workspaces/", workspaceSkillsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.WorkspaceSkillEnableRepo, deps.WorkspaceRegistriesRepo, deps.ProfileRegistriesRepo, deps.Pool))
	mux.HandleFunc("/v1/personas", personasEntry(deps.AuthService, deps.OrgMembershipRepo, deps.PersonasRepo, deps.RepoPersonas, deps.PersonaSyncTrigger))
	mux.HandleFunc("/v1/personas/", personaEntry(deps.AuthService, deps.OrgMembershipRepo, deps.PersonasRepo, deps.PersonaSyncTrigger))
	mux.HandleFunc("/v1/admin/skill-packages", adminSkillPackagesEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.SkillStore))
	mux.HandleFunc("/v1/profiles/me/skills/install", profileSkillsInstallEntry(deps.AuthService, deps.OrgMembershipRepo, deps.APIKeysRepo, deps.AuditWriter, deps.SkillPackagesRepo, deps.ProfileSkillInstallsRepo, deps.ProfileRegistriesRepo))
	mux.HandleFunc("/v1/lite/agents", liteAgentsEntry(deps.AuthService, deps.OrgMembershipRepo, deps.PersonasRepo, deps.RepoPersonas, deps.PersonaSyncTrigger))
	mux.HandleFunc("/v1/lite/agents/", liteAgentEntry(deps.AuthService, deps.OrgMembershipRepo, deps.PersonasRepo, deps.PersonaSyncTrigger))
}
