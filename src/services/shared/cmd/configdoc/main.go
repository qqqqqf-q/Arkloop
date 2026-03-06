package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	sharedconfig "arkloop/services/shared/config"
)

func main() {
	out := flag.String("out", "", "输出文件路径")
	flag.Parse()

	target := strings.TrimSpace(*out)
	if target == "" {
		target = defaultOutPath()
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("invalid -out: %v\n", err))
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("mkdir failed: %v\n", err))
		os.Exit(1)
	}

	content := sharedconfig.RenderConfigurationMarkdown(sharedconfig.DefaultRegistry())
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("write failed: %v\n", err))
		os.Exit(1)
	}
}

func defaultOutPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "docs/reference/configuration.md"
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	return filepath.Join(repoRoot, "docs", "reference", "configuration.md")
}
