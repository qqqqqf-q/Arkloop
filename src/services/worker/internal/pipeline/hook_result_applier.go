package pipeline

import (
	"sort"
	"strings"
)

type HookResultApplier interface {
	ApplyPromptFragments(systemPrompt string, fragments PromptFragments) string
	ApplyCompactHints(input CompactInput, hints CompactHints) CompactInput
}

type DefaultHookResultApplier struct{}

func NewDefaultHookResultApplier() HookResultApplier {
	return DefaultHookResultApplier{}
}

func (DefaultHookResultApplier) ApplyPromptFragments(systemPrompt string, fragments PromptFragments) string {
	normalized := sortPromptFragments(fragments)
	if len(normalized) == 0 {
		return systemPrompt
	}

	out := strings.TrimSpace(systemPrompt)
	for _, fragment := range normalized {
		content := strings.TrimSpace(fragment.Content)
		if content == "" {
			continue
		}
		tag, ok := allowedPromptTag(fragment.XMLTag)
		if !ok {
			continue
		}
		block := "<" + tag + ">\n" + content + "\n</" + tag + ">"
		if out == "" {
			out = block
			continue
		}
		out += "\n\n" + block
	}
	return out
}

func (DefaultHookResultApplier) ApplyCompactHints(input CompactInput, hints CompactHints) CompactInput {
	normalized := sortCompactHints(hints)
	if len(normalized) == 0 {
		return input
	}
	lines := make([]string, 0, len(normalized))
	for _, hint := range normalized {
		content := strings.TrimSpace(hint.Content)
		if content == "" {
			continue
		}
		lines = append(lines, content)
	}
	if len(lines) == 0 {
		return input
	}
	block := "<compact_hints>\n" + strings.Join(lines, "\n") + "\n</compact_hints>"
	prompt := strings.TrimSpace(input.SystemPrompt)
	if prompt == "" {
		input.SystemPrompt = block
		return input
	}
	input.SystemPrompt = prompt + "\n\n" + block
	return input
}

func BuildCompactHintsBlock(hints CompactHints) string {
	out := DefaultHookResultApplier{}.ApplyCompactHints(CompactInput{}, hints)
	return strings.TrimSpace(out.SystemPrompt)
}

func sortPromptFragments(fragments PromptFragments) PromptFragments {
	filtered := make(PromptFragments, 0, len(fragments))
	for _, fragment := range fragments {
		if strings.TrimSpace(fragment.Content) == "" {
			continue
		}
		filtered = append(filtered, fragment)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Priority < filtered[j].Priority
	})
	return filtered
}

func sortCompactHints(hints CompactHints) CompactHints {
	filtered := make(CompactHints, 0, len(hints))
	for _, hint := range hints {
		if strings.TrimSpace(hint.Content) == "" {
			continue
		}
		filtered = append(filtered, hint)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Priority < filtered[j].Priority
	})
	return filtered
}

func allowedPromptTag(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "notebook":
		return "notebook", true
	case "impression":
		return "impression", true
	default:
		return "", false
	}
}
