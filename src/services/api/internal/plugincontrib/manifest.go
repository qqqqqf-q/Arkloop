package plugincontrib

import (
	"fmt"
	"strings"

	sharedpluginmanifest "arkloop/services/shared/pluginmanifest"
)

type Manifest = sharedpluginmanifest.Manifest
type ManifestMCPServer = sharedpluginmanifest.MCPServerConfig
type ManifestSkill = sharedpluginmanifest.SkillConfig

func decodeManifest(payload []byte) (Manifest, []byte, error) {
	manifest, err := sharedpluginmanifest.Parse(payload)
	if err != nil {
		return Manifest{}, nil, err
	}
	normalizedPayload, err := sharedpluginmanifest.ToManifestJSON(manifest)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("encode plugin manifest: %w", err)
	}
	return manifest, normalizedPayload, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
