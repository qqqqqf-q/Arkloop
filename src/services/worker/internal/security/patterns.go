package security

// DefaultPatterns 返回编译时内置的默认正则模式
func DefaultPatterns() []PatternDef {
	return []PatternDef{
		{
			ID:       "system_override",
			Category: "instruction_override",
			Severity: "high",
			Patterns: []string{
				`(?i)ignore\s+(all\s+)?previous\s+instructions?`,
				`(?i)forget\s+(all\s+)?(your\s+)?instructions?`,
				`(?i)disregard\s+(all\s+)?prior\s+(instructions?|rules?)`,
				`(?i)you\s+are\s+now\s+(a|an)\s+`,
				`(?i)new\s+instructions?:\s*`,
				`(?i)system\s*:\s*you\s+(must|should|will)`,
			},
		},
		{
			ID:       "role_hijack",
			Category: "role_manipulation",
			Severity: "high",
			Patterns: []string{
				`(?i)<\/?system>`,
				`(?i)\[SYSTEM\]`,
				`(?i)ADMIN\s*MODE`,
				`(?i)developer\s+mode\s+(enabled|on|activated)`,
				`(?i)jailbreak`,
				`(?i)DAN\s+mode`,
			},
		},
		{
			ID:       "exfiltration",
			Category: "data_exfiltration",
			Severity: "critical",
			Patterns: []string{
				`(?i)send\s+(all|this|the)\s+(data|info|content|conversation)\s+to`,
				`(?i)forward\s+(all|this|the)\s+.{0,30}\s+to\s+https?://`,
				`(?i)encode\s+(and\s+)?send`,
				`(?i)base64\s+encode\s+.{0,30}\s+(and\s+)?(send|post|fetch)`,
			},
		},
		{
			ID:       "hidden_instruction",
			Category: "hidden_content",
			Severity: "medium",
			Patterns: []string{
				`<!--\s*(SYSTEM|INSTRUCTION|ADMIN|IGNORE)`,
				`\x00`,
				`(?i)\[hidden\]`,
				`(?i)invisible\s+instruction`,
			},
		},
	}
}
