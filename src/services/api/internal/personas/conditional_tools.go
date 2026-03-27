package personas

import (
	"fmt"
	"strings"
)

type ConditionalToolRule struct {
	When  ConditionalToolWhen `yaml:"when" json:"when"`
	Tools []string            `yaml:"tools" json:"tools"`
}

type ConditionalToolWhen struct {
	LacksInputModalities []string `yaml:"lacks_input_modalities,omitempty" json:"lacks_input_modalities,omitempty"`
}

func NormalizeConditionalToolRules(rules []ConditionalToolRule) ([]ConditionalToolRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]ConditionalToolRule, 0, len(rules))
	for i, rule := range rules {
		tools, err := normalizeConditionalToolNames(rule.Tools, fmt.Sprintf("conditional_tools[%d].tools", i))
		if err != nil {
			return nil, err
		}
		modalities, err := normalizeConditionalStringList(rule.When.LacksInputModalities, fmt.Sprintf("conditional_tools[%d].when.lacks_input_modalities", i))
		if err != nil {
			return nil, err
		}
		out = append(out, ConditionalToolRule{
			When: ConditionalToolWhen{
				LacksInputModalities: modalities,
			},
			Tools: tools,
		})
	}
	return out, nil
}

func normalizeConditionalToolNames(items []string, field string) ([]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	return out, nil
}

func normalizeConditionalStringList(items []string, field string) ([]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		cleaned := strings.ToLower(strings.TrimSpace(item))
		if cleaned == "" {
			continue
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	return out, nil
}
