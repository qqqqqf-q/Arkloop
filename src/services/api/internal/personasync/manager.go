package personasync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	repopersonas "arkloop/services/api/internal/personas"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

const (
	defaultSyncInterval = 2 * time.Second
	advisoryLockKey     = int64(88112001)
)

type Manager struct {
	root   string
	pool   *pgxpool.Pool
	repo   *data.PersonasRepository
	logger *slog.Logger

	trigger chan struct{}

	mu        sync.Mutex
	prevFiles map[string]fileSnapshot
	prevDB    map[string]dbSnapshot
}

type fileSnapshot struct {
	Key     string
	DirName string
	DirPath string
	MTime   time.Time
	Hash    string
	Persona repopersonas.RepoPersona
}

type dbSnapshot struct {
	Key     string
	Hash    string
	Updated time.Time
	Persona data.Persona
}

type mirrorPersonaYAML struct {
	ID                  string                             `yaml:"id"`
	Version             string                             `yaml:"version,omitempty"`
	Title               string                             `yaml:"title"`
	Description         string                             `yaml:"description,omitempty"`
	SoulFile            string                             `yaml:"soul_file,omitempty"`
	UserSelectable      bool                               `yaml:"user_selectable,omitempty"`
	SelectorName        string                             `yaml:"selector_name,omitempty"`
	SelectorOrder       *int                               `yaml:"selector_order,omitempty"`
	ToolAllowlist       []string                           `yaml:"tool_allowlist,omitempty"`
	ToolDenylist        []string                           `yaml:"tool_denylist,omitempty"`
	ConditionalTools    []repopersonas.ConditionalToolRule `yaml:"conditional_tools,omitempty"`
	Budgets             map[string]any                     `yaml:"budgets,omitempty"`
	TitleSummarize      map[string]any                     `yaml:"title_summarize,omitempty"`
	ResultSummarize     map[string]any                     `yaml:"result_summarize,omitempty"`
	PreferredCredential string                             `yaml:"preferred_credential,omitempty"`
	Model               string                             `yaml:"model,omitempty"`
	ReasoningMode       string                             `yaml:"reasoning_mode,omitempty"`
	StreamThinking      *bool                              `yaml:"stream_thinking,omitempty"`
	PromptCacheControl  string                             `yaml:"prompt_cache_control,omitempty"`
	ExecutorType        string                             `yaml:"executor_type,omitempty"`
	ExecutorConfig      map[string]any                     `yaml:"executor_config,omitempty"`
	IsSystem            bool                               `yaml:"is_system,omitempty"`
	IsBuiltin           bool                               `yaml:"is_builtin,omitempty"`
}

func NewManager(root string, pool *pgxpool.Pool, repo *data.PersonasRepository, logger *slog.Logger) *Manager {
	return &Manager{
		root:      strings.TrimSpace(root),
		pool:      pool,
		repo:      repo,
		logger:    logger,
		trigger:   make(chan struct{}, 1),
		prevFiles: map[string]fileSnapshot{},
		prevDB:    map[string]dbSnapshot{},
	}
}

func (m *Manager) Trigger() {
	select {
	case m.trigger <- struct{}{}:
	default:
	}
}

func (m *Manager) SyncNow(ctx context.Context) error {
	_, err := m.syncIfLeader(ctx)
	return err
}

func (m *Manager) Run(ctx context.Context) {
	if err := m.SyncNow(ctx); err != nil {
		m.logError("persona_sync_bootstrap_failed", err, nil)
	}

	ticker := time.NewTicker(defaultSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.trigger:
		}
		if _, err := m.syncIfLeader(ctx); err != nil {
			m.logError("persona_sync_failed", err, nil)
		}
	}
}

func (m *Manager) syncIfLeader(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m.pool == nil || m.repo == nil || m.root == "" {
		return false, nil
	}
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&acquired); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	return true, m.syncOnce(ctx)
}

func (m *Manager) syncOnce(ctx context.Context) error {
	files, err := m.loadFileSnapshots()
	if err != nil {
		return err
	}
	dbRows, err := m.loadDBSnapshots(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	prevFiles := cloneFileSnapshots(m.prevFiles)
	prevDB := cloneDBSnapshots(m.prevDB)
	m.mu.Unlock()

	keys := make(map[string]struct{}, len(files)+len(dbRows)+len(prevFiles)+len(prevDB))
	for key := range files {
		keys[key] = struct{}{}
	}
	for key := range dbRows {
		keys[key] = struct{}{}
	}
	for key := range prevFiles {
		keys[key] = struct{}{}
	}
	for key := range prevDB {
		keys[key] = struct{}{}
	}

	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)

	for _, key := range orderedKeys {
		file, hasFile := files[key]
		dbRow, hasDB := dbRows[key]
		_, hadPrevFile := prevFiles[key]
		_, hadPrevDB := prevDB[key]

		switch {
		case hasFile && hasDB:
			if file.Hash == dbRow.Hash {
				continue
			}
			if file.MTime.After(dbRow.Updated) {
				if err := m.applyFileToDB(ctx, file); err != nil {
					return err
				}
				m.logWarn("persona_sync_conflict", map[string]any{"persona_key": key, "winner": "file"})
				continue
			}
			if err := m.applyDBToFile(ctx, dbRow.Persona); err != nil {
				return err
			}
			m.logWarn("persona_sync_conflict", map[string]any{"persona_key": key, "winner": "db"})
		case hasFile && !hasDB:
			if hadPrevDB {
				if err := os.RemoveAll(file.DirPath); err != nil {
					return err
				}
				continue
			}
			if err := m.applyFileToDB(ctx, file); err != nil {
				return err
			}
		case !hasFile && hasDB:
			if hadPrevFile {
				if _, err := m.repo.DeactivatePlatformMirrorsByKey(ctx, key); err != nil {
					return err
				}
				continue
			}
			if err := m.applyDBToFile(ctx, dbRow.Persona); err != nil {
				return err
			}
		}
	}

	refreshedFiles, err := m.loadFileSnapshots()
	if err != nil {
		return err
	}
	refreshedDB, err := m.loadDBSnapshots(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.prevFiles = refreshedFiles
	m.prevDB = refreshedDB
	m.mu.Unlock()
	return nil
}

func (m *Manager) loadFileSnapshots() (map[string]fileSnapshot, error) {
	out := map[string]fileSnapshot{}
	personas, err := repopersonas.LoadFromDir(m.root)
	if err != nil {
		return nil, err
	}
	for _, persona := range personas {
		key := strings.TrimSpace(persona.ID)
		if key == "" {
			continue
		}
		dirName := strings.TrimSpace(persona.DirName)
		if dirName == "" {
			dirName = key
		}
		dirPath := filepath.Join(m.root, dirName)
		hash, err := repoPersonaHash(persona)
		if err != nil {
			return nil, err
		}
		mtime, err := latestDirMTime(dirPath)
		if err != nil {
			return nil, err
		}
		out[key] = fileSnapshot{Key: key, DirName: dirName, DirPath: dirPath, MTime: mtime, Hash: hash, Persona: persona}
	}
	return out, nil
}

func (m *Manager) loadDBSnapshots(ctx context.Context) (map[string]dbSnapshot, error) {
	out := map[string]dbSnapshot{}
	personas, err := m.repo.ListLatestPlatformMirrors(ctx)
	if err != nil {
		return nil, err
	}
	for _, persona := range personas {
		key := strings.TrimSpace(persona.PersonaKey)
		if key == "" {
			continue
		}
		hash, err := dbPersonaHash(persona)
		if err != nil {
			return nil, err
		}
		out[key] = dbSnapshot{Key: key, Hash: hash, Updated: persona.UpdatedAt, Persona: persona}
	}
	return out, nil
}

func (m *Manager) applyFileToDB(ctx context.Context, snap fileSnapshot) error {
	now := time.Now().UTC()
	persona, err := m.repo.UpsertPlatformMirror(ctx, data.PlatformMirrorUpsertParams{
		PersonaKey:           snap.Persona.ID,
		Version:              snap.Persona.Version,
		DisplayName:          strings.TrimSpace(snap.Persona.Title),
		Description:          optionalStringPtr(snap.Persona.Description),
		SoulMD:               strings.TrimSpace(snap.Persona.SoulMD),
		UserSelectable:       snap.Persona.UserSelectable,
		SelectorName:         optionalStringPtr(snap.Persona.SelectorName),
		SelectorOrder:        snap.Persona.SelectorOrder,
		PromptMD:             strings.TrimSpace(snap.Persona.PromptMD),
		ToolAllowlist:        cloneStrings(snap.Persona.ToolAllowlist),
		ToolDenylist:         cloneStrings(snap.Persona.ToolDenylist),
		CoreTools:            cloneStrings(snap.Persona.CoreTools),
		BudgetsJSON:          mustJSONRaw(snap.Persona.Budgets),
		TitleSummarizeJSON:   mustJSONRawNil(snap.Persona.TitleSummarize),
		ResultSummarizeJSON:  mustJSONRawNil(snap.Persona.ResultSummarize),
		ConditionalToolsJSON: mustJSONRawSliceNil(snap.Persona.ConditionalTools),
		PreferredCredential:  optionalStringPtr(snap.Persona.PreferredCredential),
		Model:                optionalStringPtr(snap.Persona.Model),
		ReasoningMode:        strings.TrimSpace(snap.Persona.ReasoningMode),
		StreamThinking:       snap.Persona.StreamThinking,
		PromptCacheControl:   strings.TrimSpace(snap.Persona.PromptCacheControl),
		ExecutorType:         strings.TrimSpace(snap.Persona.ExecutorType),
		ExecutorConfigJSON:   mustJSONRawNil(snap.Persona.ExecutorConfig),
		IsActive:             true,
		MirroredFileDir:      snap.DirName,
		LastSyncedAt:         &now,
	})
	if err != nil {
		return err
	}
	if persona != nil {
		return m.repo.MarkSynced(ctx, persona.ID, now)
	}
	return nil
}

func (m *Manager) applyDBToFile(ctx context.Context, persona data.Persona) error {
	dirName := strings.TrimSpace(persona.PersonaKey)
	if persona.MirroredFileDir != nil && strings.TrimSpace(*persona.MirroredFileDir) != "" {
		dirName = strings.TrimSpace(*persona.MirroredFileDir)
	}
	if dirName == "" {
		dirName = strings.TrimSpace(persona.PersonaKey)
	}
	dirPath := filepath.Join(m.root, dirName)
	if err := writePersonaDir(dirPath, persona); err != nil {
		return err
	}
	return m.repo.MarkSynced(ctx, persona.ID, time.Now().UTC())
}

func writePersonaDir(dirPath string, persona data.Persona) error {
	if err := os.RemoveAll(dirPath); err != nil {
		return err
	}
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return err
	}

	yamlDoc, scriptBody, titlePromptBody, resultPromptBody, err := buildMirrorPersonaYAML(persona)
	if err != nil {
		return err
	}
	encoded, err := yaml.Marshal(yamlDoc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dirPath, "persona.yaml"), encoded, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dirPath, "prompt.md"), []byte(strings.TrimSpace(persona.PromptMD)+"\n"), 0644); err != nil {
		return err
	}
	if strings.TrimSpace(persona.SoulMD) != "" {
		if err := os.WriteFile(filepath.Join(dirPath, "soul.md"), []byte(strings.TrimSpace(persona.SoulMD)+"\n"), 0644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(scriptBody) != "" {
		if err := os.WriteFile(filepath.Join(dirPath, "agent.lua"), []byte(strings.TrimSpace(scriptBody)+"\n"), 0644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(titlePromptBody) != "" {
		if err := os.WriteFile(filepath.Join(dirPath, "title_summarize.md"), []byte(strings.TrimSpace(titlePromptBody)+"\n"), 0644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(resultPromptBody) != "" {
		if err := os.WriteFile(filepath.Join(dirPath, "result_summarize.md"), []byte(strings.TrimSpace(resultPromptBody)+"\n"), 0644); err != nil {
			return err
		}
	}
	return nil
}

func buildMirrorPersonaYAML(persona data.Persona) (mirrorPersonaYAML, string, string, string, error) {
	budgets, err := decodeRawMap(persona.BudgetsJSON)
	if err != nil {
		return mirrorPersonaYAML{}, "", "", "", err
	}
	titleSummarize, err := decodeRawMap(persona.TitleSummarizeJSON)
	if err != nil {
		return mirrorPersonaYAML{}, "", "", "", err
	}
	resultSummarize, err := decodeRawMap(persona.ResultSummarizeJSON)
	if err != nil {
		return mirrorPersonaYAML{}, "", "", "", err
	}
	executorConfig, err := decodeRawMap(persona.ExecutorConfigJSON)
	if err != nil {
		return mirrorPersonaYAML{}, "", "", "", err
	}
	conditionalTools, err := decodeConditionalTools(persona.ConditionalToolsJSON)
	if err != nil {
		return mirrorPersonaYAML{}, "", "", "", err
	}

	scriptBody := ""
	if strings.TrimSpace(persona.ExecutorType) == "agent.lua" {
		rawScript, _ := executorConfig["script"].(string)
		if strings.TrimSpace(rawScript) == "" {
			return mirrorPersonaYAML{}, "", "", "", fmt.Errorf("agent.lua runtime persona missing script")
		}
		scriptBody = rawScript
		delete(executorConfig, "script")
		executorConfig["script_file"] = "agent.lua"
	} else {
		delete(executorConfig, "script_file")
	}
	titlePromptBody := extractPromptToFile(titleSummarize, "title_summarize.md")
	resultPromptBody := extractPromptToFile(resultSummarize, "result_summarize.md")

	doc := mirrorPersonaYAML{
		ID:               strings.TrimSpace(persona.PersonaKey),
		Version:          strings.TrimSpace(persona.Version),
		Title:            strings.TrimSpace(persona.DisplayName),
		Description:      derefString(persona.Description),
		UserSelectable:   persona.UserSelectable,
		SelectorName:     derefString(persona.SelectorName),
		SelectorOrder:    persona.SelectorOrder,
		ToolAllowlist:    cloneStrings(persona.ToolAllowlist),
		ToolDenylist:     cloneStrings(persona.ToolDenylist),
		ConditionalTools: conditionalTools,
		Budgets:          budgets,
		TitleSummarize:   titleSummarize,
		ResultSummarize:  resultSummarize,
		// DB-only 字段，不写回 yaml
		PreferredCredential: "",
		Model:               "",
		ReasoningMode:       strings.TrimSpace(persona.ReasoningMode),
		PromptCacheControl:  strings.TrimSpace(persona.PromptCacheControl),
		ExecutorType:        strings.TrimSpace(persona.ExecutorType),
		ExecutorConfig:      executorConfig,
		IsSystem:            strings.TrimSpace(persona.PersonaKey) == repopersonas.SystemSummarizerPersonaID,
		IsBuiltin:           strings.TrimSpace(persona.PersonaKey) == repopersonas.SystemSummarizerPersonaID,
	}
	if !persona.StreamThinking {
		f := false
		doc.StreamThinking = &f
	}
	if strings.TrimSpace(persona.SoulMD) != "" {
		doc.SoulFile = "soul.md"
	}
	return doc, scriptBody, titlePromptBody, resultPromptBody, nil
}

func extractPromptToFile(obj map[string]any, fileName string) string {
	if len(obj) == 0 {
		return ""
	}
	rawPrompt, _ := obj["prompt"].(string)
	prompt := strings.TrimSpace(rawPrompt)
	delete(obj, "prompt")
	if prompt == "" {
		delete(obj, "prompt_file")
		return ""
	}
	obj["prompt_file"] = fileName
	return prompt
}

func repoPersonaHash(persona repopersonas.RepoPersona) (string, error) {
	payload := map[string]any{
		"version":              strings.TrimSpace(persona.Version),
		"display_name":         strings.TrimSpace(persona.Title),
		"description":          strings.TrimSpace(persona.Description),
		"soul_md":              strings.TrimSpace(persona.SoulMD),
		"user_selectable":      persona.UserSelectable,
		"selector_name":        strings.TrimSpace(persona.SelectorName),
		"selector_order":       persona.SelectorOrder,
		"prompt_md":            strings.TrimSpace(persona.PromptMD),
		"tool_allowlist":       normalizeStringSlice(persona.ToolAllowlist),
		"tool_denylist":        normalizeStringSlice(persona.ToolDenylist),
		"conditional_tools":    persona.ConditionalTools,
		"core_tools":           normalizeStringSlice(persona.CoreTools),
		"budgets":              normalizeMap(persona.Budgets),
		"title_summarize":      normalizeMap(persona.TitleSummarize),
		"result_summarize":     normalizeMap(persona.ResultSummarize),
		"reasoning_mode":       strings.TrimSpace(persona.ReasoningMode),
		"stream_thinking":      data.NormalizePersonaStreamThinkingPtr(persona.StreamThinking),
		"prompt_cache_control": strings.TrimSpace(persona.PromptCacheControl),
		"executor_type":        strings.TrimSpace(persona.ExecutorType),
		"executor_config":      normalizeMap(persona.ExecutorConfig),
		"is_active":            true,
	}
	return hashPayload(payload)
}

func dbPersonaHash(persona data.Persona) (string, error) {
	budgets, err := decodeRawMap(persona.BudgetsJSON)
	if err != nil {
		return "", err
	}
	titleSummarize, err := decodeRawMap(persona.TitleSummarizeJSON)
	if err != nil {
		return "", err
	}
	resultSummarize, err := decodeRawMap(persona.ResultSummarizeJSON)
	if err != nil {
		return "", err
	}
	executorConfig, err := decodeRawMap(persona.ExecutorConfigJSON)
	if err != nil {
		return "", err
	}
	conditionalTools, err := decodeConditionalTools(persona.ConditionalToolsJSON)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"version":              strings.TrimSpace(persona.Version),
		"display_name":         strings.TrimSpace(persona.DisplayName),
		"description":          derefString(persona.Description),
		"soul_md":              strings.TrimSpace(persona.SoulMD),
		"user_selectable":      persona.UserSelectable,
		"selector_name":        derefString(persona.SelectorName),
		"selector_order":       persona.SelectorOrder,
		"prompt_md":            strings.TrimSpace(persona.PromptMD),
		"tool_allowlist":       normalizeStringSlice(persona.ToolAllowlist),
		"tool_denylist":        normalizeStringSlice(persona.ToolDenylist),
		"conditional_tools":    conditionalTools,
		"core_tools":           normalizeStringSlice(persona.CoreTools),
		"budgets":              budgets,
		"title_summarize":      titleSummarize,
		"result_summarize":     resultSummarize,
		"reasoning_mode":       strings.TrimSpace(persona.ReasoningMode),
		"stream_thinking":      persona.StreamThinking,
		"prompt_cache_control": strings.TrimSpace(persona.PromptCacheControl),
		"executor_type":        strings.TrimSpace(persona.ExecutorType),
		"executor_config":      executorConfig,
		"is_active":            persona.IsActive,
	}
	return hashPayload(payload)
}

func hashPayload(payload map[string]any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func decodeRawMap(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return map[string]any{}, nil
	}
	return obj, nil
}

func decodeConditionalTools(raw json.RawMessage) ([]repopersonas.ConditionalToolRule, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rules []repopersonas.ConditionalToolRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, err
	}
	return repopersonas.NormalizeConditionalToolRules(rules)
}

func normalizeMap(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

func normalizeStringSlice(raw []string) []string {
	if raw == nil {
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func latestDirMTime(dir string) (time.Time, error) {
	latest := time.Time{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	if latest.IsZero() {
		latest = time.Now().UTC()
	}
	return latest.UTC(), nil
}

func cloneStrings(raw []string) []string {
	if raw == nil {
		return []string{}
	}
	out := make([]string, len(raw))
	copy(out, raw)
	return out
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func mustJSONRaw(raw map[string]any) json.RawMessage {
	if raw == nil {
		return json.RawMessage("{}")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

func mustJSONRawNil(raw map[string]any) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return encoded
}

func mustJSONRawSliceNil[T any](raw []T) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return encoded
}

func cloneFileSnapshots(in map[string]fileSnapshot) map[string]fileSnapshot {
	out := make(map[string]fileSnapshot, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneDBSnapshots(in map[string]dbSnapshot) map[string]dbSnapshot {
	out := make(map[string]dbSnapshot, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (m *Manager) logWarn(msg string, extra map[string]any) {
	if m.logger == nil {
		return
	}
	args := make([]any, 0, len(extra)*2)
	for k, v := range extra {
		args = append(args, k, v)
	}
	m.logger.Warn(msg, args...)
}

func (m *Manager) logError(msg string, err error, extra map[string]any) {
	if m.logger == nil {
		return
	}
	args := []any{"error", err.Error()}
	for k, v := range extra {
		args = append(args, k, v)
	}
	m.logger.Error(msg, args...)
}
