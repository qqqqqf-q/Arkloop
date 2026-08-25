package lsp

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxResultLen = 100_000

// editSummary describes the result of applying a WorkspaceEdit.
type editSummary struct {
	ChangedFiles []string
	TotalEdits   int
}

func formatLocations(locs []Location, rootDir, kind string) string {
	if len(locs) == 0 {
		return fmt.Sprintf("No %ss found.", kind)
	}

	var b strings.Builder
	count := len(locs)
	if count > 100 {
		count = 100
	}
	fmt.Fprintf(&b, "Found %d %s(s):\n", len(locs), kind)

	for i := 0; i < count; i++ {
		loc := locs[i]
		p := uriToRelPath(loc.URI, rootDir)
		line := loc.Range.Start.Line + 1
		col := loc.Range.Start.Character + 1
		fmt.Fprintf(&b, "  %s:%d:%d\n", p, line, col)
	}
	if len(locs) > 100 {
		fmt.Fprintf(&b, "  ... (%d more)\n", len(locs)-100)
	}
	return truncate(b.String())
}

// formatLocationsWithMeta prepends a metadata line for quick result scanning.
func formatLocationsWithMeta(locs []Location, rootDir, kind string) string {
	// count unique files
	files := make(map[string]struct{})
	for _, loc := range locs {
		files[loc.URI] = struct{}{}
	}
	meta := fmt.Sprintf("[resultCount=%d fileCount=%d]\n", len(locs), len(files))
	return meta + formatLocations(locs, rootDir, kind)
}

func formatHover(h *Hover) string {
	if h == nil {
		return "No hover information available."
	}
	return truncate(h.Contents.Value)
}

func formatDocumentSymbols(syms []DocumentSymbol, indent int) string {
	if len(syms) == 0 && indent == 0 {
		return "No symbols found."
	}

	var b strings.Builder
	prefix := strings.Repeat("  ", indent)
	for _, s := range syms {
		startLine := s.Range.Start.Line + 1
		endLine := s.Range.End.Line + 1
		kind := s.Kind.String()

		if startLine == endLine {
			fmt.Fprintf(&b, "%s%s %s (L%d)\n", prefix, kind, s.Name, startLine)
		} else {
			fmt.Fprintf(&b, "%s%s %s (L%d-L%d)\n", prefix, kind, s.Name, startLine, endLine)
		}

		if len(s.Children) > 0 {
			b.WriteString(formatDocumentSymbols(s.Children, indent+1))
		}
	}
	return truncate(b.String())
}

func formatWorkspaceSymbols(syms []WorkspaceSymbol, rootDir string) string {
	if len(syms) == 0 {
		return "No matching symbols found."
	}

	var b strings.Builder
	count := len(syms)
	if count > 100 {
		count = 100
	}
	fmt.Fprintf(&b, "Found %d symbol(s):\n", len(syms))

	for i := 0; i < count; i++ {
		s := syms[i]
		p := uriToRelPath(s.Location.URI, rootDir)
		line := s.Location.Range.Start.Line + 1
		fmt.Fprintf(&b, "  %s %s - %s:%d\n", s.Kind.String(), s.Name, p, line)
	}
	if len(syms) > 100 {
		fmt.Fprintf(&b, "  ... (%d more)\n", len(syms)-100)
	}
	return truncate(b.String())
}

func formatCompletions(cl *CompletionList) string {
	if cl == nil || len(cl.Items) == 0 {
		return "No completions available."
	}

	var b strings.Builder
	count := len(cl.Items)
	if count > 20 {
		count = 20
	}
	fmt.Fprintf(&b, "Completions (%d total):\n", len(cl.Items))

	for i := 0; i < count; i++ {
		item := cl.Items[i]
		fmt.Fprintf(&b, "  %s", item.Label)
		if item.Detail != "" {
			fmt.Fprintf(&b, " - %s", item.Detail)
		}
		b.WriteByte('\n')
	}
	if len(cl.Items) > 20 {
		fmt.Fprintf(&b, "  ... (%d more)\n", len(cl.Items)-20)
	}
	return truncate(b.String())
}

func formatSignatureHelp(sh *SignatureHelp) string {
	if sh == nil || len(sh.Signatures) == 0 {
		return "No signature help available."
	}

	var b strings.Builder
	for i, sig := range sh.Signatures {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sig.Label)
		b.WriteByte('\n')

		if sh.ActiveParameter != nil && int(*sh.ActiveParameter) < len(sig.Parameters) {
			param := sig.Parameters[*sh.ActiveParameter]
			if label, ok := param.Label.(string); ok {
				fmt.Fprintf(&b, "  Active parameter: %s\n", label)
			}
			if doc := anyToString(param.Documentation); doc != "" {
				fmt.Fprintf(&b, "  %s\n", doc)
			}
		}
	}
	return truncate(b.String())
}

func formatRenameResult(s *editSummary) string {
	if s == nil || s.TotalEdits == 0 {
		return "No changes applied."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Renamed across %d file(s), %d edit(s):\n", len(s.ChangedFiles), s.TotalEdits)
	for _, f := range s.ChangedFiles {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	return b.String()
}

// pendingWrite holds the computed new content and original backup for a file.
type pendingWrite struct {
	path    string
	content []byte
	backup  []byte
}

// applyWorkspaceEdit writes edits to disk with rollback on failure.
// All edits are computed in memory first, then written atomically.
func applyWorkspaceEdit(edit *WorkspaceEdit) (*editSummary, error) {
	if edit == nil {
		return &editSummary{}, nil
	}

	// collect (path, edits) pairs
	type fileEdits struct {
		path  string
		edits []TextEdit
	}
	var targets []fileEdits

	if len(edit.DocumentChanges) > 0 {
		for _, dc := range edit.DocumentChanges {
			p, err := URIToPath(dc.TextDocument.URI)
			if err != nil {
				return nil, fmt.Errorf("uri to path: %w", err)
			}
			targets = append(targets, fileEdits{path: p, edits: dc.Edits})
		}
	} else {
		uris := make([]string, 0, len(edit.Changes))
		for uri := range edit.Changes {
			uris = append(uris, uri)
		}
		sort.Strings(uris)
		for _, uri := range uris {
			p, err := URIToPath(uri)
			if err != nil {
				return nil, fmt.Errorf("uri to path: %w", err)
			}
			targets = append(targets, fileEdits{path: p, edits: edit.Changes[uri]})
		}
	}

	// phase 1: read all files and compute new content in memory
	pending := make([]pendingWrite, 0, len(targets))
	for _, t := range targets {
		backup, err := os.ReadFile(t.path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", t.path, err)
		}
		newContent, err := computeEdits(backup, t.edits)
		if err != nil {
			return nil, fmt.Errorf("compute edits for %s: %w", t.path, err)
		}
		pending = append(pending, pendingWrite{path: t.path, content: newContent, backup: backup})
	}

	// phase 2: write all files; rollback on failure
	for i, pw := range pending {
		if err := os.WriteFile(pw.path, pw.content, 0644); err != nil {
			// rollback already-written files
			for j := 0; j < i; j++ {
				if rbErr := os.WriteFile(pending[j].path, pending[j].backup, 0644); rbErr != nil {
					slog.Error("rollback write failed", "path", pending[j].path, "err", rbErr)
				}
			}
			return nil, fmt.Errorf("write %s: %w", pw.path, err)
		}
	}

	summary := &editSummary{}
	for _, t := range targets {
		summary.ChangedFiles = append(summary.ChangedFiles, t.path)
		summary.TotalEdits += len(t.edits)
	}
	return summary, nil
}

// computeEdits applies text edits to file content in memory and returns the result.
func computeEdits(content []byte, edits []TextEdit) ([]byte, error) {
	text := string(content)
	hasCRLF := strings.Contains(text, "\r\n")
	if hasCRLF {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}

	lines := strings.Split(text, "\n")

	sorted := make([]TextEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Range.Start.Line != sorted[j].Range.Start.Line {
			return sorted[i].Range.Start.Line > sorted[j].Range.Start.Line
		}
		return sorted[i].Range.Start.Character > sorted[j].Range.Start.Character
	})

	for _, e := range sorted {
		startOff := positionToOffset(lines, e.Range.Start)
		endOff := positionToOffset(lines, e.Range.End)
		if startOff < 0 || endOff < 0 || startOff > len(text) || endOff > len(text) {
			continue
		}
		text = text[:startOff] + e.NewText + text[endOff:]
		lines = strings.Split(text, "\n")
	}

	if hasCRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(text), nil
}

// positionToOffset converts an LSP Position to a byte offset in the text.
func positionToOffset(lines []string, pos Position) int {
	offset := 0
	for i := 0; i < int(pos.Line) && i < len(lines); i++ {
		offset += len(lines[i]) + 1 // +1 for \n
	}
	if int(pos.Line) < len(lines) {
		charOff := UTF8OffsetFromUTF16(lines[pos.Line], pos.Character)
		offset += charOff
	}
	return offset
}

func uriToRelPath(uri, rootDir string) string {
	p, err := URIToPath(uri)
	if err != nil {
		return uri
	}
	if rootDir != "" {
		if rel, err := filepath.Rel(rootDir, p); err == nil {
			return rel
		}
	}
	return p
}

func truncate(s string) string {
	if len(s) > maxResultLen {
		return s[:maxResultLen] + "\n... (truncated)"
	}
	return s
}

func anyToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case MarkupContent:
		return val.Value
	case map[string]any:
		if v, ok := val["value"].(string); ok {
			return v
		}
	}
	return ""
}

func formatCallHierarchyItems(items []CallHierarchyItem, rootDir string) string {
	if len(items) == 0 {
		return "No call hierarchy item found at this position."
	}

	var b strings.Builder
	if len(items) == 1 {
		item := items[0]
		p := uriToRelPath(item.URI, rootDir)
		line := item.Range.Start.Line + 1
		fmt.Fprintf(&b, "Call hierarchy item: %s (%s) - %s:%d", item.Name, item.Kind.String(), p, line)
		return b.String()
	}

	fmt.Fprintf(&b, "Found %d call hierarchy items:\n", len(items))
	for _, item := range items {
		p := uriToRelPath(item.URI, rootDir)
		line := item.Range.Start.Line + 1
		fmt.Fprintf(&b, "  %s (%s) - %s:%d\n", item.Name, item.Kind.String(), p, line)
	}
	return truncate(b.String())
}

func formatIncomingCalls(calls []CallHierarchyIncomingCall, rootDir string) string {
	if len(calls) == 0 {
		return "No incoming calls found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Called by:\n")
	for _, call := range calls {
		p := uriToRelPath(call.From.URI, rootDir)
		line := call.From.Range.Start.Line + 1
		fmt.Fprintf(&b, "  %s (%s) - %s:%d\n", call.From.Name, call.From.Kind.String(), p, line)
	}
	return truncate(b.String())
}

func formatOutgoingCalls(calls []CallHierarchyOutgoingCall, rootDir string) string {
	if len(calls) == 0 {
		return "No outgoing calls found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Calls:\n")
	for _, call := range calls {
		p := uriToRelPath(call.To.URI, rootDir)
		line := call.To.Range.Start.Line + 1
		fmt.Fprintf(&b, "  %s (%s) - %s:%d\n", call.To.Name, call.To.Kind.String(), p, line)
	}
	return truncate(b.String())
}
