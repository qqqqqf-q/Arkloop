//go:build !desktop

package mcpfilesync

import (
	"context"
	"encoding/json"
	"fmt"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

type DiscoveryRequest struct {
	WorkspaceRoot *string
	Paths         []string
}

type ProposedInstall struct {
	InstallKey      string          `json:"install_key"`
	DisplayName     string          `json:"display_name"`
	Transport       string          `json:"transport"`
	LaunchSpecJSON  json.RawMessage `json:"launch_spec_json"`
	HostRequirement string          `json:"host_requirement"`
	SourceKind      string          `json:"source_kind"`
	SourceURI       string          `json:"source_uri"`
	SyncMode        string          `json:"sync_mode"`
}

type DiscoverySource struct {
	SourceURI        string            `json:"source_uri"`
	SourceKind       string            `json:"source_kind"`
	Installable      bool              `json:"installable"`
	ValidationErrors []string          `json:"validation_errors"`
	HostWarnings     []string          `json:"host_warnings"`
	ProposedInstalls []ProposedInstall `json:"proposed_installs"`
}

type DiscoveryResponse struct {
	Sources []DiscoverySource `json:"sources"`
}

type Service struct{}

func NewService(_ string, _ *data.ProfileMCPInstallsRepository, _ *data.SecretsRepository) (*Service, error) {
	return &Service{}, nil
}

func (s *Service) SyncDesktopMirror(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func (s *Service) DiscoverSources(_ context.Context, _ DiscoveryRequest) (DiscoveryResponse, error) {
	return DiscoveryResponse{}, nil
}

func (s *Service) StartWatcher(_ context.Context, _ uuid.UUID, _ string, _ interface{}) {
}

func (s *Service) SyncFromOfficialFile(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func ErrDesktopOnly() error {
	return fmt.Errorf("mcp file sync is only available in desktop mode")
}
