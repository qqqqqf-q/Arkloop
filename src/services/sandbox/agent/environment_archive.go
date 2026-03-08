package main

import "fmt"

func environmentRoots(scope string) ([]checkpointRoot, error) {
	switch scope {
	case "profile":
		return []checkpointRoot{{HostPath: shellHomeDir, ArchivePath: "home/arkloop"}}, nil
	case "workspace":
		return []checkpointRoot{{HostPath: shellWorkspaceDir, ArchivePath: "workspace"}}, nil
	default:
		return nil, fmt.Errorf("unsupported environment scope: %s", scope)
	}
}

func exportEnvironmentArchive(scope string) ([]byte, error) {
	roots, err := environmentRoots(scope)
	if err != nil {
		return nil, err
	}
	return exportCheckpointArchive(roots)
}

func importEnvironmentArchive(scope string, archive []byte) error {
	roots, err := environmentRoots(scope)
	if err != nil {
		return err
	}
	return restoreCheckpointArchive(roots, archive)
}
