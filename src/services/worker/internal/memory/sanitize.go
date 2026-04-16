package memory

import "strings"

// EscapeXMLContent escapes < and > as standard HTML entities.
// This prevents XML boundary escape attacks when content is placed
// inside <memory> or <notebook> blocks.
func EscapeXMLContent(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
