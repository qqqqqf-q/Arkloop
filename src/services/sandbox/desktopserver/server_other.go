//go:build !darwin || !cgo

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
	return nil, fmt.Errorf("apple VM isolation requires macOS with cgo enabled")
}

func (s *Server) Start(_ context.Context) (string, error) {
	return "", fmt.Errorf("apple VM isolation requires macOS with cgo enabled")
}

func (s *Server) Addr() string { return "" }
func (s *Server) Stop()        {}
