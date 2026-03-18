package tools

import "strings"

var generativeUIBootstrapTools = map[string]struct{}{
	"visualize_read_me":   {},
	"artifact_guidelines": {},
}

func IsGenerativeUIBootstrapTool(toolName string) bool {
	_, ok := generativeUIBootstrapTools[strings.TrimSpace(toolName)]
	return ok
}

func ShouldBypassResultCompression(toolName string) bool {
	return IsGenerativeUIBootstrapTool(toolName)
}

func ShouldBypassResultSummarization(toolName string) bool {
	return IsGenerativeUIBootstrapTool(toolName)
}
