package acp

import (
	"context"
	"fmt"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

type HostKind string

const (
	HostKindSandbox HostKind = "sandbox"
	HostKindLocal   HostKind = "local"
)

type ProcessHost interface {
	Start(ctx context.Context, req StartRequest) (*StartResponse, error)
	Write(ctx context.Context, req WriteRequest) error
	Read(ctx context.Context, req ReadRequest) (*ReadResponse, error)
	Stop(ctx context.Context, req StopRequest) error
	Wait(ctx context.Context, req WaitRequest) (*WaitResponse, error)
	Status(ctx context.Context, req StatusRequest) (*StatusResponse, error)
}

func ResolveProcessHost(provider ResolvedProvider, snapshot *sharedtoolruntime.RuntimeSnapshot) (ProcessHost, error) {
	switch provider.HostKind {
	case HostKindSandbox:
		if snapshot == nil || snapshot.SandboxBaseURL == "" {
			return nil, fmt.Errorf("sandbox host unavailable")
		}
		return NewSandboxProcessHost(snapshot.SandboxBaseURL, snapshot.SandboxAuthToken), nil
	case HostKindLocal:
		return NewLocalProcessHost(), nil
	default:
		return nil, fmt.Errorf("unsupported ACP host kind: %s", provider.HostKind)
	}
}

func NewSandboxProcessHost(baseURL, authToken string) ProcessHost {
	return NewClient(baseURL, authToken)
}
