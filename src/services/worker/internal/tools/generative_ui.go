package tools

import "strings"

var generativeUIBootstrapTools = map[string]struct{}{
	"visualize_read_me":   {},
	"artifact_guidelines": {},
}

var productHelpTools = map[string]struct{}{
	"arkloop_help": {},
}

func IsGenerativeUIBootstrapTool(toolName string) bool {
	_, ok := generativeUIBootstrapTools[strings.TrimSpace(toolName)]
	return ok
}

func ShouldBypassResultCompression(toolName string) bool {
	name := strings.TrimSpace(toolName)
	if _, ok := generativeUIBootstrapTools[name]; ok {
		return true
	}
	if _, ok := productHelpTools[name]; ok {
		return true
	}
	return false
}

func ShouldBypassResultSummarization(toolName string) bool {
	return ShouldBypassResultCompression(toolName)
}
