package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	root string
}

type cursor struct {
	Files map[string]fileState `json:"files"`
}

type fileState struct {
	ModTimeUnixNano int64  `json:"mod_time_unix_nano"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
}

type rawRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type payloadEnvelope struct {
	ID      string          `json:"id"`
	CWD     string          `json:"cwd"`
	TurnID  string          `json:"turn_id"`
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Phase   string          `json:"phase"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func NewDefault() (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Source{root: filepath.Join(home, ".codex", "sessions")}, nil
}

func (s *Source) Name() string {
	return "codex"
}

func (s *Source) Sync(ctx context.Context, db *store.Store) (int, error) {
	var cur cursor
	if err := db.Cursor(ctx, s.Name(), &cur); err != nil {
		return 0, err
	}
	if cur.Files == nil {
		cur.Files = map[string]fileState{}
	}
	files, err := sessionFiles(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	nextFiles := make(map[string]fileState, len(files))
	totalChanged := 0
	for _, file := range files {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		state, err := statForFile(file)
		if err != nil {
			return 0, err
		}
		rel, err := filepath.Rel(s.root, file)
		if err != nil {
			return 0, err
		}
		rel = filepath.ToSlash(rel)
		previous, ok := cur.Files[rel]
		if ok && previous.ModTimeUnixNano == state.ModTimeUnixNano && previous.Size == state.Size {
			nextFiles[rel] = previous
			continue
		}
		hash, err := sha256File(file)
		if err != nil {
			return 0, err
		}
		state.SHA256 = hash
		if ok && previous.SHA256 == state.SHA256 {
			nextFiles[rel] = state
			continue
		}
		parsed, err := parseSession(file, rel)
		if err != nil {
			return 0, err
		}
		changed, err := db.UpsertEvents(ctx, parsed)
		if err != nil {
			_ = db.SaveCursor(ctx, s.Name(), cur, err)
			return 0, err
		}
		totalChanged += changed
		nextFiles[rel] = state
		cur.Files = cloneFileStates(nextFiles)
		if err := db.SaveCursor(ctx, s.Name(), cur, nil); err != nil {
			return 0, err
		}
	}
	cur.Files = nextFiles
	if err := db.SaveCursor(ctx, s.Name(), cur, nil); err != nil {
		return 0, err
	}
	return totalChanged, nil
}

func cloneFileStates(in map[string]fileState) map[string]fileState {
	out := make(map[string]fileState, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sessionFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func statForFile(path string) (fileState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileState{}, err
	}
	return fileState{
		ModTimeUnixNano: info.ModTime().UnixNano(),
		Size:            info.Size(),
	}, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func parseSession(path string, sourceFile string) ([]store.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	projectPath := ""
	activeTurnID := ""
	sequence := 0
	candidates := make([]messageCandidate, 0)

	reader := bufio.NewReaderSize(file, 1024*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if readErr == io.EOF {
				break
			}
			continue
		}
		var rec rawRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			if readErr == io.EOF {
				break
			}
			continue
		}
		var payload payloadEnvelope
		if len(rec.Payload) > 0 {
			_ = json.Unmarshal(rec.Payload, &payload)
		}
		if rec.Type == "session_meta" {
			if payload.ID != "" {
				sessionID = payload.ID
			}
			if projectPath == "" {
				projectPath = homeRelative(payload.CWD)
			}
			continue
		}
		if rec.Type == "turn_context" {
			if projectPath == "" {
				projectPath = homeRelative(payload.CWD)
			}
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil {
			continue
		}
		var extracted []messageCandidate
		switch rec.Type {
		case "event_msg":
			nextActiveTurnID, messages := eventMessages(payload, activeTurnID)
			activeTurnID = nextActiveTurnID
			extracted = messages
		case "response_item":
			extracted = responseItemMessages(payload, activeTurnID)
		default:
			continue
		}
		for _, msg := range extracted {
			if msg.TurnID == "" || msg.Text == "" {
				continue
			}
			msg.OccurredAt = ts
			msg.Sequence = sequence
			sequence++
			candidates = append(candidates, msg)
		}
		if readErr == io.EOF {
			break
		}
	}
	return buildEvents(sessionID, sourceFile, projectPath, candidates), nil
}

type messageCandidate struct {
	TurnID     string
	Role       string
	Action     string
	Text       string
	OccurredAt time.Time
	Priority   int
	Sequence   int
}

func eventMessages(payload payloadEnvelope, activeTurnID string) (string, []messageCandidate) {
	switch payload.Type {
	case "task_started":
		return payload.TurnID, nil
	case "task_complete":
		return "", nil
	case "user_message":
		text := strings.TrimSpace(payload.Message)
		if activeTurnID == "" || text == "" {
			return activeTurnID, nil
		}
		return activeTurnID, []messageCandidate{{TurnID: activeTurnID, Role: "user", Action: "prompted", Text: text, Priority: 2}}
	case "agent_message":
		if payload.Phase != "final_answer" {
			return activeTurnID, nil
		}
		text := strings.TrimSpace(payload.Message)
		if activeTurnID == "" || text == "" {
			return activeTurnID, nil
		}
		return activeTurnID, []messageCandidate{{TurnID: activeTurnID, Role: "assistant", Action: "received", Text: text, Priority: 2}}
	default:
		return activeTurnID, nil
	}
}

func responseItemMessages(payload payloadEnvelope, activeTurnID string) []messageCandidate {
	if payload.Type != "message" {
		return nil
	}
	switch payload.Role {
	case "user":
		text := strings.TrimSpace(extractInputText(payload.Content))
		if activeTurnID == "" || text == "" || looksLikeInjectedInstructions(text) {
			return nil
		}
		return []messageCandidate{{TurnID: activeTurnID, Role: "user", Action: "prompted", Text: text, Priority: 1}}
	case "assistant":
		if payload.Phase != "final_answer" {
			return nil
		}
		text := strings.TrimSpace(extractInputText(payload.Content))
		if activeTurnID == "" || text == "" {
			return nil
		}
		return []messageCandidate{{TurnID: activeTurnID, Role: "assistant", Action: "received", Text: text, Priority: 1}}
	default:
		return nil
	}
}

func buildEvents(sessionID string, sourceFile string, projectPath string, candidates []messageCandidate) []store.Event {
	chosen := make(map[string]messageCandidate, len(candidates))
	for _, candidate := range candidates {
		key := messageKey(candidate)
		current, ok := chosen[key]
		if !ok || candidate.Priority > current.Priority || candidate.Priority == current.Priority && candidate.Sequence < current.Sequence {
			chosen[key] = candidate
		}
	}
	messages := make([]messageCandidate, 0, len(chosen))
	for _, candidate := range chosen {
		messages = append(messages, candidate)
	}
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].Sequence < messages[j].Sequence
	})

	roleOrdinals := map[string]int{}
	events := make([]store.Event, 0, len(messages))
	for _, msg := range messages {
		ordinalKey := msg.TurnID + ":" + msg.Role
		ordinal := roleOrdinals[ordinalKey]
		roleOrdinals[ordinalKey] = ordinal + 1
		messageID := "turn:" + msg.TurnID + ":" + msg.Role + ":" + strconvItoa(ordinal)
		sourceEventID := sessionID + "#" + messageID
		events = append(events, store.Event{
			Source:        "codex",
			SourceEventID: sourceEventID,
			OccurredAt:    msg.OccurredAt,
			App:           "Codex",
			Action:        msg.Action,
			Title:         msg.Text,
			Text:          msg.Text,
			Metadata: map[string]any{
				"project_path": projectPath,
				"session_id":   sessionID,
				"source_file":  sourceFile,
				"turn_id":      msg.TurnID,
				"role":         msg.Role,
			},
			RefKind: "codex_session",
			RefKey:  sessionID + "#msg:" + messageID,
		})
	}
	return events
}

func messageKey(candidate messageCandidate) string {
	return candidate.TurnID + "\x00" + candidate.Role + "\x00" + candidate.Action + "\x00" + normalizedMessage(candidate.Text)
}

func normalizedMessage(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func extractInputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var out []string
	for _, part := range parts {
		if part.Text == "" {
			continue
		}
		switch part.Type {
		case "", "input_text", "output_text", "text":
			out = append(out, part.Text)
		}
	}
	return strings.Join(out, "\n")
}

func looksLikeInjectedInstructions(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "# AGENTS.md instructions") ||
		strings.HasPrefix(trimmed, "<INSTRUCTIONS>") ||
		strings.HasPrefix(trimmed, "<permissions instructions>") ||
		strings.HasPrefix(trimmed, "<collaboration_mode>")
}

func homeRelative(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		rel := strings.TrimPrefix(path, home)
		rel = strings.TrimPrefix(rel, string(os.PathSeparator))
		if rel == "" {
			return "~"
		}
		return rel
	}
	return path
}

func strconvItoa(value int) string { return strconv.Itoa(value) }
