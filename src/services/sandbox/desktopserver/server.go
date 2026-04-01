//go:build darwin && cgo

// Package desktopserver provides a lightweight embedded sandbox HTTP server
// for the desktop sidecar. It uses the VZ pool to run shell commands inside
// isolated Apple VMs.
package desktopserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"arkloop/services/sandbox/internal/acp"
	sandboxhttp "arkloop/services/sandbox/internal/http"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/shell"
	sandboxskills "arkloop/services/sandbox/internal/skills"
	vzpool "arkloop/services/sandbox/internal/vz"
)

type Config struct {
	ListenAddr     string
	KernelImage    string
	InitrdPath     string
	RootfsPath     string
	SocketBaseDir  string
	BootTimeout    int
	GuestAgentPort uint32
	AuthToken      string
}

type Server struct {
	cfg      Config
	listener net.Listener
	server   *http.Server
	pool     *vzpool.Pool
	sessMgr  *session.Manager
	logger   *logging.JSONLogger
}

func New(cfg Config) (*Server, error) {
	if err := os.MkdirAll(cfg.SocketBaseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	logger := logging.NewJSONLogger("desktop-sandbox", os.Stderr)

	pool := vzpool.New(vzpool.Config{
		WarmSizes:             map[string]int{"lite": 0, "browser": 0},
		RefillIntervalSeconds: 60,
		MaxRefillConcurrency:  1,
		KernelImagePath:       cfg.KernelImage,
		InitrdPath:            cfg.InitrdPath,
		RootfsPath:            cfg.RootfsPath,
		SocketBaseDir:         cfg.SocketBaseDir,
		BootTimeoutSeconds:    cfg.BootTimeout,
		GuestAgentPort:        cfg.GuestAgentPort,
		Logger:                logger,
	})

	sessMgr := session.NewManager(session.ManagerConfig{
		MaxSessions: 10,
		Pool:        pool,
		IdleTimeouts: map[string]int{
			session.TierLite:    300,
			session.TierPro:     600,
			session.TierBrowser: 180,
		},
		MaxLifetimes: map[string]int{
			session.TierLite:    3600,
			session.TierPro:     3600,
			session.TierBrowser: 1200,
		},
	})

	restoreRegistry := shell.NewMemorySessionRestoreRegistry()
	shellMgr := shell.NewManager(sessMgr, nil, nil, restoreRegistry, nil, nil, logger, shell.Config{})
	acpSvc := acp.NewManager(sessMgr, logger)
	skillMgr := sandboxskills.NewOverlayManager(nil)

	handler := sandboxhttp.NewHandler(sessMgr, nil, skillMgr, shellMgr, acpSvc, nil, logger, cfg.AuthToken)

	return &Server{
		cfg:     cfg,
		pool:    pool,
		sessMgr: sessMgr,
		logger:  logger,
		server:  &http.Server{Handler: handler},
	}, nil
}

// Start begins listening. Returns the actual address.
func (s *Server) Start(ctx context.Context) (string, error) {
	var err error
	s.listener, err = net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	addr := s.listener.Addr().String()
	s.logger.Info("desktop sandbox listening", logging.LogFields{}, map[string]any{"addr": addr})

	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			s.logger.Warn("desktop sandbox serve error", logging.LogFields{}, map[string]any{"error": err.Error()})
		}
	}()

	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	return addr, nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) Stop() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.server.Shutdown(shutdownCtx)
	s.sessMgr.CloseAll(shutdownCtx)
	s.pool.Drain(shutdownCtx)
}
