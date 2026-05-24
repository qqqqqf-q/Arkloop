package shell

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	historyFiles []string
}

type cursor struct {
	Files map[string]int64 `json:"files"`
}

func NewDefault() (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var files []string
	candidates := []string{
		filepath.Join(home, ".zsh_history"),
		filepath.Join(home, ".bash_history"),
	}
	for _, f := range candidates {
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no shell history files found")
	}
	return &Source{historyFiles: files}, nil
}

func (s *Source) Name() string {
	return "shell"
}

func (s *Source) Sync(ctx context.Context, db *store.Store) (int, error) {
	var cur cursor
	if err := db.Cursor(ctx, s.Name(), &cur); err != nil {
		return 0, err
	}
	if cur.Files == nil {
		cur.Files = map[string]int64{}
	}

	next := cursor{Files: make(map[string]int64, len(cur.Files))}
	for k, v := range cur.Files {
		next.Files[k] = v
	}

	var events []store.Event
	for _, histFile := range s.historyFiles {
		shell := shellName(histFile)
		savedSize := cur.Files[histFile]
		fileEvents, newSize, err := parseHistoryFile(histFile, shell, savedSize)
		if err != nil {
			continue
		}
		events = append(events, fileEvents...)
		next.Files[histFile] = newSize
	}

	inserted, err := db.UpsertEvents(ctx, events)
	if err != nil {
		_ = db.SaveCursor(ctx, s.Name(), cur, err)
		return 0, err
	}
	if err := db.SaveCursor(ctx, s.Name(), next, nil); err != nil {
		return 0, err
	}
	return inserted, nil
}

func parseHistoryFile(path, shell string, savedSize int64) ([]store.Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, savedSize, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, savedSize, err
	}
	currentSize := info.Size()

	if currentSize == savedSize {
		return nil, savedSize, nil
	}
	startAfter := savedSize
	if currentSize < savedSize {
		startAfter = 0
	}

	var events []store.Event
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	bytesRead := int64(0)

	for scanner.Scan() {
		raw := scanner.Bytes()
		bytesRead += int64(len(raw)) + 1

		if bytesRead <= startAfter {
			continue
		}

		line := string(raw)

		var cmd string
		var occurredAt time.Time

		if ts, parsed, ok := parseZshExtended(line); ok {
			cmd = strings.TrimSpace(parsed)
			occurredAt = time.Unix(ts, 0).UTC()
		} else if strings.HasPrefix(line, ": ") {
			continue
		} else {
			cmd = strings.TrimSpace(line)
			occurredAt = info.ModTime()
		}

		if cmd == "" {
			continue
		}

		h := commandHash(cmd)
		if seen[h] {
			continue
		}
		seen[h] = true

		events = append(events, store.Event{
			Source:        "shell",
			SourceEventID: fmt.Sprintf("shell:%s:%s", shell, h[:16]),
			OccurredAt:    occurredAt,
			App:           shell,
			Action:        "command",
			Title:         truncate(cmd, 200),
			Text:          cmd,
			Metadata: map[string]any{
				"shell": shell,
			},
		})
	}

	return events, currentSize, scanner.Err()
}

// : 1234567890:0;command
func parseZshExtended(line string) (int64, string, bool) {
	if !strings.HasPrefix(line, ": ") {
		return 0, "", false
	}
	rest := line[2:]
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return 0, "", false
	}
	ts, err := strconv.ParseInt(rest[:idx], 10, 64)
	if err != nil {
		return 0, "", false
	}
	semicolon := strings.Index(rest, ";")
	if semicolon < 0 {
		return 0, "", false
	}
	return ts, rest[semicolon+1:], true
}

func shellName(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.Contains(base, "zsh"):
		return "zsh"
	case strings.Contains(base, "bash"):
		return "bash"
	default:
		return "shell"
	}
}

func commandHash(cmd string) string {
	h := sha256.Sum256([]byte(cmd))
	return fmt.Sprintf("%x", h)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
