package config

import "strings"

func RenderConfigurationMarkdown(registry *Registry) string {
	if registry == nil {
		registry = DefaultRegistry()
	}

	var b strings.Builder
	b.WriteString("| key | type | scope | default | sensitive | description |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")

	for _, e := range registry.List() {
		b.WriteString("| ")
		b.WriteString(escapeMarkdownTableCell(e.Key))
		b.WriteString(" | ")
		b.WriteString(escapeMarkdownTableCell(e.Type))
		b.WriteString(" | ")
		b.WriteString(escapeMarkdownTableCell(e.Scope))
		b.WriteString(" | ")
		b.WriteString(escapeMarkdownTableCell(e.Default))
		b.WriteString(" | ")
		if e.Sensitive {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		b.WriteString(" | ")
		b.WriteString(escapeMarkdownTableCell(e.Description))
		b.WriteString(" |\n")
	}

	return b.String()
}

func escapeMarkdownTableCell(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}
