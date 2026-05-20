package agentdirectory

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

//go:embed templates/*.md
var templateFS embed.FS

// seedMu prevents concurrent seeding of the same directory.
var seedMu sync.Mutex

// templateNames is the ordered list of template files to seed.
// MEMORY.md is intentionally absent -- the agent creates it per AGENTS.md guidance.
var templateNames = []string{
	"AGENTS.md",
	"SOUL.md",
	"IDENTITY.md",
	"USER.md",
	"TOOLS.md",
	"BOOTSTRAP.md",
	"HEARTBEAT.md",
}

// SeedWorkspace writes template files into dir if they don't already exist.
// Returns the number of files seeded.
func SeedWorkspace(dir string) (int, error) {
	seedMu.Lock()
	defer seedMu.Unlock()

	count := 0
	for _, name := range templateNames {
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}

		data, err := fs.ReadFile(templateFS, filepath.Join("templates", name))
		if err != nil {
			return count, err
		}

		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return count, err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return count, err
		}
		f.Close()
		count++
	}
	return count, nil
}

// IsBootstrapPending returns true if BOOTSTRAP.md exists in dir.
func IsBootstrapPending(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "BOOTSTRAP.md"))
	return err == nil
}
