//go:build !darwin

package desktopserver

import (
	"context"
	"fmt"
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

type Server struct{}

func New(_ Config) (*Server, error) {
	return nil, fmt.Errorf("Apple VM isolation requires macOS")
}

func (s *Server) Start(_ context.Context) (string, error) {
	return "", fmt.Errorf("Apple VM isolation requires macOS")
}

func (s *Server) Addr() string { return "" }
func (s *Server) Stop()        {}
