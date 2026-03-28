package promptinjection

import (
	"os"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/plugin"
	"arkloop/services/worker/internal/securitycap"
)

type BuilderDeps struct {
	Resolver sharedconfig.Resolver
	Store    sharedconfig.Store
	Cache    sharedconfig.Cache
	CacheTTL time.Duration
	AuditDB  plugin.DBExecutor
}

func Build(deps BuilderDeps) (securitycap.Runtime, error) {
	return securitycap.BuildRuntime(securitycap.RuntimeDeps{
		Resolver: deps.Resolver,
		Store:    deps.Store,
		Cache:    deps.Cache,
		CacheTTL: deps.CacheTTL,
		AuditDB:  deps.AuditDB,
		ModelDir: strings.TrimSpace(os.Getenv("ARKLOOP_PROMPT_GUARD_MODEL_DIR")),
		OrtLib:   strings.TrimSpace(os.Getenv("ARKLOOP_ONNX_RUNTIME_LIB")),
	})
}
