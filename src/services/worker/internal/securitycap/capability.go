package securitycap

import (
	"log/slog"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/plugin"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/security"
)

// Runtime 收口 prompt injection 运行时依赖，供 cloud 和 desktop 共用。
type Runtime struct {
	Resolver         sharedconfig.Resolver
	CompositeScanner *security.CompositeScanner
	InjectionAuditor *security.SecurityAuditor
}

// RuntimeDeps 描述 runtime 装配所需的最小依赖。
type RuntimeDeps struct {
	Resolver sharedconfig.Resolver
	Store    sharedconfig.Store
	Cache    sharedconfig.Cache
	CacheTTL time.Duration
	AuditDB  plugin.DBExecutor
	ModelDir string
	OrtLib   string
}

// BuildRuntime 统一构建 resolver、scanner 和 auditor。
func BuildRuntime(deps RuntimeDeps) (Runtime, error) {
	resolver := deps.Resolver
	if resolver == nil {
		var err error
		resolver, err = sharedconfig.NewResolver(sharedconfig.DefaultRegistry(), deps.Store, deps.Cache, deps.CacheTTL)
		if err != nil {
			return Runtime{}, err
		}
	}

	var regexScanner *security.RegexScanner
	if scanner, err := security.NewRegexScanner(security.DefaultPatterns()); err == nil {
		regexScanner = scanner
	} else {
		slog.Error("failed to initialize injection scanner", "error", err)
	}

	semanticScanner := security.NewRuntimeSemanticScanner(resolver, deps.ModelDir, deps.OrtLib)
	compositeScanner := security.NewCompositeScanner(regexScanner, semanticScanner)

	var injectionAuditor *security.SecurityAuditor
	if deps.AuditDB != nil {
		if dbSink, err := plugin.NewDBSink(deps.AuditDB); err == nil {
			injectionAuditor = security.NewSecurityAuditor(dbSink)
		} else {
			slog.Error("failed to initialize security auditor", "error", err)
		}
	}

	return Runtime{
		Resolver:         resolver,
		CompositeScanner: compositeScanner,
		InjectionAuditor: injectionAuditor,
	}, nil
}

// Middlewares 返回 prompt injection 所需的 middleware 组合，顺序不可打乱。
func (r Runtime) Middlewares(eventsRepo data.RunEventStore) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewTrustSourceMiddleware(r.Resolver),
		pipeline.NewInjectionScanMiddleware(r.CompositeScanner, r.InjectionAuditor, r.Resolver, eventsRepo),
	}
}
