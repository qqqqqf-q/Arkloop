package lsp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MaxDiagsPerFile    = 10
	MaxDiagsTotal      = 30
	ActiveWaitDelay    = 3 * time.Second
	DiagDedupeCapacity = 10000
)

type dedupedDiag struct {
	Diagnostic
	key string
}

// lruEntry is a node in the LRU doubly-linked list.
type lruEntry struct {
	key        string
	prev, next *lruEntry
}

// diagLRU is a minimal LRU set (no values, just key existence).
type diagLRU struct {
	cap        int
	items      map[string]*lruEntry
	head, tail *lruEntry // head = most recent, tail = least recent
}

func newDiagLRU(cap int) *diagLRU {
	return &diagLRU{cap: cap, items: make(map[string]*lruEntry, cap)}
}

func (l *diagLRU) contains(key string) bool {
	e, ok := l.items[key]
	if !ok {
		return false
	}
	l.moveToHead(e)
	return true
}

func (l *diagLRU) add(key string) {
	if e, ok := l.items[key]; ok {
		l.moveToHead(e)
		return
	}
	if len(l.items) >= l.cap {
		l.evictTail()
	}
	e := &lruEntry{key: key}
	l.pushHead(e)
	l.items[key] = e
}

func (l *diagLRU) remove(key string) {
	e, ok := l.items[key]
	if !ok {
		return
	}
	l.unlink(e)
	delete(l.items, key)
}

func (l *diagLRU) pushHead(e *lruEntry) {
	e.prev = nil
	e.next = l.head
	if l.head != nil {
		l.head.prev = e
	}
	l.head = e
	if l.tail == nil {
		l.tail = e
	}
}

func (l *diagLRU) unlink(e *lruEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		l.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		l.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (l *diagLRU) moveToHead(e *lruEntry) {
	if l.head == e {
		return
	}
	l.unlink(e)
	l.pushHead(e)
}

func (l *diagLRU) evictTail() {
	if l.tail == nil {
		return
	}
	e := l.tail
	l.unlink(e)
	delete(l.items, e.key)
}

// DiagnosticRegistry collects and deduplicates LSP diagnostics.
type DiagnosticRegistry struct {
	mu         sync.RWMutex
	files      map[string][]dedupedDiag // URI -> diagnostics
	seen       *diagLRU                 // LRU dedup set
	editedURIs map[string]time.Time     // URI -> last edit time
	logger     *slog.Logger
}

// NewDiagnosticRegistry creates a new registry.
func NewDiagnosticRegistry(logger *slog.Logger) *DiagnosticRegistry {
	return &DiagnosticRegistry{
		files:      make(map[string][]dedupedDiag),
		seen:       newDiagLRU(DiagDedupeCapacity),
		editedURIs: make(map[string]time.Time),
		logger:     logger,
	}
}

// diagKey produces a dedup key from a diagnostic.
func diagKey(d Diagnostic) string {
	compact := struct {
		Message  string             `json:"m"`
		Severity DiagnosticSeverity `json:"s"`
		Range    Range              `json:"r"`
		Source   string             `json:"src"`
		Code     any                `json:"c,omitempty"`
	}{d.Message, d.Severity, d.Range, d.Source, d.Code}
	b, _ := json.Marshal(compact)
	return string(b)
}

// HandlePublishDiagnostics replaces diagnostics for a URI (server sends full set each time).
func (dr *DiagnosticRegistry) HandlePublishDiagnostics(params PublishDiagnosticsParams) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	// Remove old seen keys for this URI so fixed-then-reintroduced diagnostics are not dropped.
	if old, ok := dr.files[params.URI]; ok {
		for _, d := range old {
			dr.seen.remove(d.key)
		}
	}

	deduped := make([]dedupedDiag, 0, len(params.Diagnostics))
	for _, d := range params.Diagnostics {
		k := diagKey(d)
		if dr.seen.contains(k) {
			continue
		}
		dr.seen.add(k)
		deduped = append(deduped, dedupedDiag{Diagnostic: d, key: k})
	}

	if len(deduped) == 0 {
		delete(dr.files, params.URI)
	} else {
		dr.files[params.URI] = deduped
	}
}

// MarkFileEdited records that a file was recently edited.
func (dr *DiagnosticRegistry) MarkFileEdited(uri string) {
	dr.mu.Lock()
	dr.editedURIs[uri] = time.Now()
	dr.mu.Unlock()
}

// ClearFile removes diagnostics for a URI.
func (dr *DiagnosticRegistry) ClearFile(uri string) {
	dr.mu.Lock()
	delete(dr.files, uri)
	delete(dr.editedURIs, uri)
	dr.mu.Unlock()
}

// HasRecentEdits returns true if any file was edited within ActiveWaitDelay.
func (dr *DiagnosticRegistry) HasRecentEdits() bool {
	dr.mu.RLock()
	defer dr.mu.RUnlock()
	cutoff := time.Now().Add(-ActiveWaitDelay)
	for _, t := range dr.editedURIs {
		if t.After(cutoff) {
			return true
		}
	}
	return false
}

// GetDiagnostics returns a volume-limited, severity-sorted copy.
func (dr *DiagnosticRegistry) GetDiagnostics() map[string][]Diagnostic {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	result := make(map[string][]Diagnostic)
	total := 0

	// Collect all URIs sorted for deterministic output.
	uris := make([]string, 0, len(dr.files))
	for uri := range dr.files {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	for _, uri := range uris {
		diags := dr.files[uri]
		// Sort by severity (Error first).
		sorted := make([]dedupedDiag, len(diags))
		copy(sorted, diags)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Severity < sorted[j].Severity
		})

		cap := MaxDiagsPerFile
		if total+cap > MaxDiagsTotal {
			cap = MaxDiagsTotal - total
		}
		if cap <= 0 {
			break
		}
		if len(sorted) > cap {
			sorted = sorted[:cap]
		}

		out := make([]Diagnostic, len(sorted))
		for i, d := range sorted {
			out[i] = d.Diagnostic
		}
		result[uri] = out
		total += len(out)
	}

	return result
}

// FormatForPrompt renders diagnostics as a human-readable string for LLM context.
func (dr *DiagnosticRegistry) FormatForPrompt(rootDir string) string {
	diags := dr.GetDiagnostics()
	if len(diags) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Active Diagnostics\n")

	// Sorted URIs for stable output.
	uris := make([]string, 0, len(diags))
	for uri := range diags {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	for _, uri := range uris {
		filePath, err := URIToPath(uri)
		if err != nil {
			filePath = uri
		}
		if rootDir != "" {
			if rel, err := filepath.Rel(rootDir, filePath); err == nil {
				filePath = rel
			}
		}

		b.WriteString("\n### ")
		b.WriteString(filePath)
		b.WriteByte('\n')

		for _, d := range diags[uri] {
			line := d.Range.Start.Line + 1
			col := d.Range.Start.Character + 1
			sev := severityString(d.Severity)
			b.WriteString(fmt.Sprintf("- L%d:%d %s: %s", line, col, sev, d.Message))
			if d.Source != "" {
				b.WriteString(fmt.Sprintf(" (source: %s)", d.Source))
			}
			b.WriteByte('\n')
		}
	}

	return b.String()
}

func severityString(s DiagnosticSeverity) string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInformation:
		return "info"
	case SeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}
