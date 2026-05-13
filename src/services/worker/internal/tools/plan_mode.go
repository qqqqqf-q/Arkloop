package tools

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PlanModeChecker is implemented by ExecutionContext.PipelineRC (the pipeline RunContext)
// to expose plan-mode state to write tools without importing the pipeline package.
type PlanModeChecker interface {
	IsPlanModeActive() bool
}

type PlanModeWritePathChecker interface {
	PlanModeChecker
	PlanModeWritePathAllowed(path string) bool
}

type PlanModePlanFilePathProvider interface {
	PlanFilePathValue() string
}

type PlanModePlanFileBinder interface {
	SetPlanFilePath(path string)
}

func DefaultPlanDirectory() string {
	if base := strings.TrimSpace(os.Getenv("ARKLOOP_PLAN_DIR")); base != "" {
		return filepath.Clean(base)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".arkloop", "home", "plans")
	}
	return filepath.Join(os.TempDir(), "arkloop-plans")
}

// PlanModeBlocked returns a populated ExecutionResult when plan mode is active,
// otherwise it returns the zero-value result and false. Write tools should call this
// at the top of Execute and short-circuit when blocked is true.
func PlanModeBlocked(rc any, started time.Time) (ExecutionResult, bool) {
	checker, ok := rc.(PlanModeChecker)
	if !ok || checker == nil || !checker.IsPlanModeActive() {
		return ExecutionResult{}, false
	}
	return ExecutionResult{
		Error: &ExecutionError{
			ErrorClass: ErrorClassToolExecutionFailed,
			Message:    "tool not available in plan mode",
		},
		DurationMs: int(time.Since(started).Milliseconds()),
	}, true
}

func PlanModeWriteBlocked(rc any, started time.Time, filePath string) (ExecutionResult, bool) {
	checker, ok := rc.(PlanModeChecker)
	if !ok || checker == nil || !checker.IsPlanModeActive() {
		return ExecutionResult{}, false
	}
	if pathChecker, ok := rc.(PlanModeWritePathChecker); ok && pathChecker.PlanModeWritePathAllowed(filePath) {
		return ExecutionResult{}, false
	}
	return ExecutionResult{
		Error: &ExecutionError{
			ErrorClass: ErrorClassToolExecutionFailed,
			Message:    "tool not available in plan mode outside the plan file",
		},
		DurationMs: int(time.Since(started).Milliseconds()),
	}, true
}

func PlanModePlanFileMetadata(rc any, workDir string, path string) (map[string]any, bool) {
	checker, ok := rc.(PlanModeChecker)
	if !ok || checker == nil || !checker.IsPlanModeActive() {
		return nil, false
	}
	provider, ok := rc.(PlanModePlanFilePathProvider)
	if !ok || provider == nil {
		return nil, false
	}
	if path == "" {
		return nil, false
	}
	pathAbs := resolvePlanModePath(workDir, path)
	if pathAbs == "" {
		return nil, false
	}
	planPath := provider.PlanFilePathValue()
	if planPath != "" {
		planAbs := resolvePlanModePath(workDir, planPath)
		if planAbs == "" || planAbs != pathAbs {
			return nil, false
		}
	} else {
		if !PlanModePlanFileCandidate(workDir, path) {
			return nil, false
		}
		if binder, ok := rc.(PlanModePlanFileBinder); ok && binder != nil {
			binder.SetPlanFilePath(pathAbs)
		}
		planPath = pathAbs
	}
	filename := filepath.Base(pathAbs)
	return map[string]any{
		"plan_file_path": pathAbs,
		"filename":       filename,
	}, true
}

func PlanModeSamePath(workDir string, target string, path string) bool {
	targetAbs := resolvePlanModePath(workDir, strings.TrimSpace(target))
	pathAbs := resolvePlanModePath(workDir, strings.TrimSpace(path))
	return targetAbs != "" && pathAbs != "" && targetAbs == pathAbs
}

func PlanModePlanFileCandidate(workDir string, path string) bool {
	pathAbs := resolvePlanModePath(workDir, strings.TrimSpace(path))
	if pathAbs == "" {
		return false
	}
	if !strings.HasSuffix(filepath.Base(pathAbs), ".plan.md") {
		return false
	}
	base := filepath.Clean(DefaultPlanDirectory())
	if base == "" {
		return false
	}
	rel, err := filepath.Rel(base, pathAbs)
	if err != nil {
		return false
	}
	return rel != "." && rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func resolvePlanModePath(workDir string, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && workDir != "" {
		path = filepath.Join(workDir, path)
	}
	return filepath.Clean(path)
}
