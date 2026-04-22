package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkoukk/tiktoken-go"
)

func NewRuntimeContextMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.ChannelContext != nil {
			rc.UpsertPromptSegment(PromptSegment{
				Name:          "runtime.channel_output_behavior",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          buildChannelOutputBehaviorBlock(),
				Stability:     PromptStabilityStablePrefix,
				CacheEligible: true,
			})
			isAdmin := checkSenderIsAdmin(ctx, rc)
			rc.SenderIsAdmin = isAdmin
		}
		rc.UpsertPromptSegment(PromptSegment{
			Name:          "runtime.context",
			Target:        PromptTargetSystemPrefix,
			Role:          "system",
			Text:          buildRuntimeContextBlock(ctx, rc),
			Stability:     PromptStabilitySessionPrefix,
			CacheEligible: true,
		})
		return next(ctx, rc)
	}
}

func buildChannelOutputBehaviorBlock() string {
	return `<channel_output_behavior>
Your text outputs are delivered to the chat platform in real-time as separate messages.
When you call tools mid-reply, text before and after the tool call becomes distinct messages visible to the user.
Avoid repeating content that was already sent. If you have nothing new to add after a tool call, use end_reply.
</channel_output_behavior>`
}

func buildRuntimeContextBlock(ctx context.Context, rc *RunContext) string {
	if rc == nil {
		return ""
	}

	timeZone := runtimeContextTimeZone(ctx, rc)
	loc := loadRuntimeLocation(timeZone)
	localDate := time.Now().UTC().In(loc).Format("2006-01-02")

	var sb strings.Builder
	sb.WriteString("<runtime_context>\n")
	sb.WriteString("User Timezone: " + timeZone + "\n")
	sb.WriteString("User Local Date: " + localDate + "\n")
	sb.WriteString("Host Mode: " + hostMode + "\n")
	sb.WriteString("Platform: " + runtime.GOOS + "/" + runtime.GOARCH)
	if hostMode == "desktop" {
		sb.WriteString("\nExecution Environment: local machine (commands run directly on the user's device, not in a cloud sandbox)")
	}

	if rc.AgentConfig != nil && rc.AgentConfig.Model != nil && strings.TrimSpace(*rc.AgentConfig.Model) != "" {
		sb.WriteString("\nModel: " + strings.TrimSpace(*rc.AgentConfig.Model))
	}

	if rc.WorkDir != "" {
		sb.WriteString("\nWorking Directory: " + rc.WorkDir)

		if shell := os.Getenv("SHELL"); shell != "" {
			sb.WriteString("\nShell: " + shell)
		}

		isRepo := runtimeGitIsRepo(rc.WorkDir)
		sb.WriteString("\nGit Repository: " + fmt.Sprintf("%t", isRepo))

		if isRepo {
			sb.WriteString("\n<git>")
			sb.WriteString(runtimeGitContext(rc.WorkDir))
			sb.WriteString("\n</git>")
		}

		if tree := runtimeDirTree(rc.WorkDir); tree != "" {
			sb.WriteString("\n\n<directory_tree>\n" + tree + "</directory_tree>")
		}

		if mem := runtimeProjectInstructions(rc.WorkDir, isRepo); mem != "" {
			sb.WriteString("\n\n" + mem)
		}
	}

	sb.WriteString("\n</runtime_context>")
	return sb.String()
}

func formatBotIdentity(cc *ChannelContext) string {
	name := cc.BotDisplayName
	uname := cc.BotUsername
	if name == "" && uname == "" {
		return ""
	}
	if name != "" && uname != "" {
		return fmt.Sprintf("%s (@%s)", name, uname)
	}
	if uname != "" {
		return "@" + uname
	}
	return name
}

func formatRuntimeLocalNow(now time.Time, timeZone string) string {
	loc := loadRuntimeLocation(timeZone)
	local := now.In(loc)
	return local.Format("2006-01-02 15:04:05") + " [" + formatRuntimeUTCOffset(local) + "]"
}

func loadRuntimeLocation(timeZone string) *time.Location {
	cleaned := strings.TrimSpace(timeZone)
	if cleaned == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return time.UTC
	}
	return loc
}

// git helpers

func runtimeGitIsRepo(workDir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = workDir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func runtimeGitContext(workDir string) string {
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = workDir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	branch := run("rev-parse", "--abbrev-ref", "HEAD")
	defaultBranch := runtimeGitDefaultBranch(workDir)
	username := run("config", "user.name")
	status := run("status", "--short")
	recentLog := run("log", "--oneline", "-5")

	if len(status) > 2000 {
		status = status[:2000] + "\n... (truncated)"
	}

	var sb strings.Builder
	if branch != "" {
		sb.WriteString("\nCurrent Branch: " + branch)
	}
	if defaultBranch != "" {
		sb.WriteString("\nDefault Branch: " + defaultBranch)
	}
	if username != "" {
		sb.WriteString("\nGit User: " + username)
	}
	if recentLog != "" {
		sb.WriteString("\nRecent Commits:\n" + recentLog)
	}
	if status != "" {
		sb.WriteString("\nGit Status:\n" + status)
	}
	return sb.String()
}

func runtimeGitDefaultBranch(workDir string) string {
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = workDir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	// try origin HEAD
	ref := run("symbolic-ref", "refs/remotes/origin/HEAD")
	if ref != "" {
		parts := strings.Split(ref, "/")
		return parts[len(parts)-1]
	}
	// fallback: check main, then master
	if run("rev-parse", "--verify", "refs/heads/main") != "" {
		return "main"
	}
	if run("rev-parse", "--verify", "refs/heads/master") != "" {
		return "master"
	}
	return ""
}

// find git root for AGENTS.md walk
func runtimeGitRoot(workDir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tiktoken for token estimation

var (
	runtimeTiktokenOnce sync.Once
	runtimeTiktokenEnc  *tiktoken.Tiktoken
)

func runtimeEstimateTokens(text string) int {
	runtimeTiktokenOnce.Do(func() {
		enc, err := tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
		if err == nil {
			runtimeTiktokenEnc = enc
		}
	})
	if runtimeTiktokenEnc == nil {
		return len(text) / 4
	}
	return len(runtimeTiktokenEnc.Encode(text, nil, nil))
}

// directory tree

const (
	dirTreeMaxDepth  = 2
	dirTreeMaxPerDir = 20
	dirTreeMaxTokens = 1600
	dirTreeMaxChars  = dirTreeMaxTokens * 4 // char proxy for fast per-line check
)

var dirTreeIgnore = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	".venv": true, "venv": true, "vendor": true, "dist": true,
	"build": true, ".DS_Store": true, ".next": true, ".nuxt": true,
	".cache": true, "coverage": true, ".idea": true, ".vscode": true,
	"target": true, ".gradle": true,
}

func runtimeDirTree(root string) string {
	var sb strings.Builder
	sb.WriteString("Directory Structure:\n")
	charCount := sb.Len()
	truncated := runtimeDirTreeRecurse(&sb, &charCount, root, "", 0)

	result := sb.String()
	// final token-based check
	if runtimeEstimateTokens(result) > dirTreeMaxTokens {
		// trim to char proxy and mark truncated
		if len(result) > dirTreeMaxChars {
			result = result[:dirTreeMaxChars]
		}
		truncated = true
	}
	if truncated {
		result += "... (truncated)\n"
	}
	return result
}

func runtimeDirTreeRecurse(sb *strings.Builder, charCount *int, dir, prefix string, depth int) bool {
	if depth >= dirTreeMaxDepth {
		return false
	}

	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	entries, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return false
	}

	// filter and sort
	var filtered []os.FileInfo
	for _, e := range entries {
		if dirTreeIgnore[e.Name()] {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.Slice(filtered, func(i, j int) bool {
		// dirs first, then alphabetical
		di, dj := filtered[i].IsDir(), filtered[j].IsDir()
		if di != dj {
			return di
		}
		return filtered[i].Name() < filtered[j].Name()
	})

	total := len(filtered)
	show := total
	if show > dirTreeMaxPerDir {
		show = dirTreeMaxPerDir - 1 // show 19 + summary
	}

	for i := 0; i < show; i++ {
		e := filtered[i]
		isLast := i == total-1 || (total > dirTreeMaxPerDir && i == show-1)
		connector := "├── "
		childPrefix := prefix + "│   "
		if isLast && total <= dirTreeMaxPerDir {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		line := prefix + connector + name + "\n"
		*charCount += len(line)
		if *charCount > dirTreeMaxChars {
			return true
		}
		sb.WriteString(line)

		if e.IsDir() {
			if runtimeDirTreeRecurse(sb, charCount, filepath.Join(dir, e.Name()), childPrefix, depth+1) {
				return true
			}
		}
	}

	if total > dirTreeMaxPerDir {
		remaining := total - show
		line := prefix + "└── ... (" + fmt.Sprintf("%d", remaining) + " more)\n"
		*charCount += len(line)
		if *charCount > dirTreeMaxChars {
			return true
		}
		sb.WriteString(line)
	}

	return false
}

// AGENTS.md project instructions

func runtimeProjectInstructions(workDir string, isRepo bool) string {
	var stopAt string
	if isRepo {
		stopAt = runtimeGitRoot(workDir)
	}

	// walk up from workDir, collect AGENTS.md paths
	var paths []string
	cur := filepath.Clean(workDir)
	for {
		candidate := filepath.Join(cur, "AGENTS.md")
		if _, err := os.Stat(candidate); err == nil {
			paths = append(paths, candidate)
		}
		if stopAt != "" && cur == filepath.Clean(stopAt) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	if len(paths) == 0 {
		return ""
	}

	// token-based budgets
	const maxTotalTokens = 2000
	const maxPerFileTokens = 4096
	var sb strings.Builder
	totalTokens := 0

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		content := string(data)
		if runtimeEstimateTokens(content) > maxPerFileTokens {
			// truncate by chars as rough proxy, then re-check
			charLimit := maxPerFileTokens * 4
			if len(content) > charLimit {
				content = content[:charLimit]
			}
		}

		block := "<project_instructions>\n(contents of AGENTS.md from " + filepath.Dir(p) + ")\n" + content + "\n</project_instructions>"
		blockTokens := runtimeEstimateTokens(block)

		if totalTokens+blockTokens > maxTotalTokens {
			remaining := maxTotalTokens - totalTokens
			if remaining > 0 {
				// rough char-level truncation for remaining budget
				charBudget := remaining * 4
				if charBudget > len(block) {
					charBudget = len(block)
				}
				sb.WriteString(block[:charBudget])
			}
			break
		}
		sb.WriteString(block)
		totalTokens += blockTokens
	}

	return sb.String()
}

func formatRuntimeUTCOffset(t time.Time) string {
	_, offsetSeconds := t.Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	if minutes == 0 {
		return fmt.Sprintf("UTC%s%d", sign, hours)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, hours, minutes)
}
