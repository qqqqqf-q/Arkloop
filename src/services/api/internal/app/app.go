package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	apihttp "arkloop/services/api/internal/http"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Application struct {
	config Config
	logger *observability.JSONLogger
}

func NewApplication(config Config, logger *observability.JSONLogger) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	return &Application{
		config: config,
		logger: logger,
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		pool       *pgxpool.Pool
		schemaRepo *data.SchemaRepository
	)
	var poolCloser func()

	dsn := strings.TrimSpace(a.config.DatabaseDSN)
	if dsn != "" {
		createdPool, err := data.NewPool(ctx, dsn)
		if err != nil {
			return err
		}
		pool = createdPool
		poolCloser = createdPool.Close

		repo, err := data.NewSchemaRepository(createdPool)
		if err != nil {
			createdPool.Close()
			return err
		}
		schemaRepo = repo

		schemaVersion, vErr := repo.CurrentSchemaVersion(ctx)
		if vErr != nil {
			a.logger.Error("schema version check skipped",
				observability.LogFields{},
				map[string]any{"reason": vErr.Error()},
			)
		} else if schemaVersion != migrate.ExpectedVersion {
			a.logger.Error("schema version mismatch",
				observability.LogFields{},
				map[string]any{
					"current":  schemaVersion,
					"expected": migrate.ExpectedVersion,
				},
			)
		}
	}
	if poolCloser != nil {
		defer poolCloser()
	}

	var (
		userRepo       *data.UserRepository
		credentialRepo *data.UserCredentialRepository
		membershipRepo *data.OrgMembershipRepository
		threadRepo     *data.ThreadRepository
		messageRepo    *data.MessageRepository
		runEventRepo   *data.RunEventRepository
		auditRepo      *data.AuditLogRepository

		authService         *auth.Service
		registrationService *auth.RegistrationService
		auditWriter         *audit.Writer
	)

	if pool != nil {
		var err error
		userRepo, err = data.NewUserRepository(pool)
		if err != nil {
			return err
		}
		credentialRepo, err = data.NewUserCredentialRepository(pool)
		if err != nil {
			return err
		}
		membershipRepo, err = data.NewOrgMembershipRepository(pool)
		if err != nil {
			return err
		}
		threadRepo, err = data.NewThreadRepository(pool)
		if err != nil {
			return err
		}
		messageRepo, err = data.NewMessageRepository(pool)
		if err != nil {
			return err
		}
		runEventRepo, err = data.NewRunEventRepository(pool)
		if err != nil {
			return err
		}
		auditRepo, err = data.NewAuditLogRepository(pool)
		if err != nil {
			return err
		}
	}

	if pool != nil && a.config.Auth != nil {
		passwordHasher, err := auth.NewBcryptPasswordHasher(0)
		if err != nil {
			return err
		}
		tokenService, err := auth.NewJwtAccessTokenService(a.config.Auth.JWTSecret, a.config.Auth.AccessTokenTTLSeconds)
		if err != nil {
			return err
		}
		authService, err = auth.NewService(userRepo, credentialRepo, passwordHasher, tokenService)
		if err != nil {
			return err
		}
		registrationService, err = auth.NewRegistrationService(pool, passwordHasher, tokenService)
		if err != nil {
			return err
		}

		if auditRepo != nil {
			auditWriter = audit.NewWriter(auditRepo, membershipRepo, a.logger)
		}
	}

	listener, err := net.Listen("tcp", a.config.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	server := &http.Server{
		Handler: apihttp.NewHandler(apihttp.HandlerConfig{
			Pool:                 pool,
			Logger:               a.logger,
			TrustIncomingTraceID: a.config.TrustIncomingTraceID,
			SchemaRepository:     schemaRepo,
			AuthService:          authService,
			RegistrationService:  registrationService,
			OrgMembershipRepo:    membershipRepo,
			ThreadRepo:           threadRepo,
			MessageRepo:          messageRepo,
			RunEventRepo:         runEventRepo,
			AuditWriter:          auditWriter,
			SSEConfig: apihttp.SSEConfig{
				PollSeconds:      a.config.SSE.PollSeconds,
				HeartbeatSeconds: a.config.SSE.HeartbeatSeconds,
				BatchLimit:       a.config.SSE.BatchLimit,
			},
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}

	err = <-errCh
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
