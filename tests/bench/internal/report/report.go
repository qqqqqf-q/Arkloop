package report

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Targets struct {
	GatewayBaseURL    string `json:"gateway_base_url"`
	APIBaseURL        string `json:"api_base_url"`
	BrowserBaseURL    string `json:"browser_base_url"`
	OpenVikingBaseURL string `json:"openviking_base_url"`
}

type Meta struct {
	GeneratedAt string   `json:"generated_at"`
	GitSHA      string   `json:"git_sha,omitempty"`
	GitDirty    bool     `json:"git_dirty,omitempty"`
	GoVersion   string   `json:"go_version"`
	GOOS        string   `json:"goos"`
	GOARCH      string   `json:"goarch"`
	NumCPU      int      `json:"num_cpu"`
	Targets     Targets  `json:"targets"`
	Assumptions []string `json:"assumptions,omitempty"`
}

type ScenarioResult struct {
	Name       string         `json:"name"`
	Config     map[string]any `json:"config"`
	Stats      map[string]any `json:"stats"`
	Thresholds map[string]any `json:"thresholds"`
	Pass       bool           `json:"pass"`
	Errors     []string       `json:"errors,omitempty"`
}

type Report struct {
	Meta        Meta              `json:"meta"`
	Results     []ScenarioResult  `json:"results"`
	OverallPass bool              `json:"overall_pass"`
	ManualNotes map[string]string `json:"manual_notes,omitempty"`
}

func BuildMeta(ctx context.Context, targets Targets) Meta {
	if ctx == nil {
		ctx = context.Background()
	}
	return Meta{
		GeneratedAt: time.Now().Format(time.RFC3339),
		GitSHA:      strings.TrimSpace(resolveGitSHA(ctx)),
		GitDirty:    resolveGitDirty(ctx),
		GoVersion:   runtime.Version(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		NumCPU:      runtime.NumCPU(),
		Targets:     targets,
		Assumptions: []string{
			"env.docker_compose_single_node",
			"worker.llm_stub_by_default",
			"openviking.skipped_in_baseline_by_default",
			"browser.separate_benchmark_command",
			"browser_memory.sample_only",
		},
	}
}

func Encode(r Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func resolveGitSHA(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Stderr = new(bytes.Buffer)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func resolveGitDirty(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Stderr = new(bytes.Buffer)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(out)) > 0
}
