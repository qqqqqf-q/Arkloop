package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sharedconfig "arkloop/services/shared/config"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/memory/nowledge"

	"github.com/google/uuid"
)

type nowledgeLinkStoreStub struct{}

func (nowledgeLinkStoreStub) Get(context.Context, uuid.UUID, uuid.UUID, string) (string, bool, error) {
	return "", false, fmt.Errorf("unexpected link lookup")
}

func (nowledgeLinkStoreStub) Upsert(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return fmt.Errorf("unexpected link upsert")
}

type nowledgeResolverStub struct {
	values map[string]string
}

func (s nowledgeResolverStub) Resolve(_ context.Context, key string, _ sharedconfig.Scope) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return value, nil
}

func (s nowledgeResolverStub) ResolvePrefix(context.Context, string, sharedconfig.Scope) (map[string]string, error) {
	return nil, fmt.Errorf("not implemented")
}

type nowledgeSnapshotStoreCapture struct {
	block string
	hits  []data.MemoryHitCache
	done  chan struct{}
}

func (s *nowledgeSnapshotStoreCapture) Get(context.Context, uuid.UUID, uuid.UUID, string) (string, bool, error) {
	return "", false, nil
}

func (s *nowledgeSnapshotStoreCapture) UpsertWithHits(_ context.Context, _, _ uuid.UUID, _ string, block string, hits []data.MemoryHitCache) error {
	s.block = block
	s.hits = append([]data.MemoryHitCache(nil), hits...)
	if s.done != nil {
		select {
		case s.done <- struct{}{}:
		default:
		}
	}
	return nil
}

type nowledgeImpressionStoreStub struct {
	score      int
	resetCalls int
}

func (s *nowledgeImpressionStoreStub) Get(context.Context, uuid.UUID, uuid.UUID, string) (string, bool, error) {
	return "", false, nil
}

func (s *nowledgeImpressionStoreStub) Upsert(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return nil
}

func (s *nowledgeImpressionStoreStub) AddScore(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, delta int) (int, error) {
	s.score += delta
	return s.score, nil
}

func (s *nowledgeImpressionStoreStub) ResetScore(context.Context, uuid.UUID, uuid.UUID, string) error {
	s.score = 0
	s.resetCalls++
	return nil
}

func TestNowledgeDistillObserverSkipsWhenDisabled(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	observer := NewNowledgeDistillObserver(provider, nowledgeLinkStoreStub{}, nowledgeResolverStub{
		values: map[string]string{"memory.distill_enabled": "false"},
	}, nil, nil, nil, nil)
	if observer == nil {
		t.Fatal("expected observer")
	}

	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
	}

	delta := ThreadDelta{
		AccountID:       rc.Run.AccountID,
		ThreadID:        rc.Run.ThreadID,
		UserID:          userID,
		AgentID:         "user_" + userID.String(),
		Messages:        []ThreadDeltaMessage{{Role: "user", Content: "你好"}, {Role: "assistant", Content: "你好，我记住了。"}},
		AssistantOutput: "你好，我记住了。",
	}
	result := ThreadPersistResult{
		Handled:          true,
		Committed:        true,
		ExternalThreadID: "thread-1",
		Provider:         "nowledge",
	}

	if _, err := observer.AfterThreadPersist(context.Background(), rc, delta, result); err != nil {
		t.Fatalf("AfterThreadPersist: %v", err)
	}
}

func TestNowledgeDistillObserverRunsWhenEnabled(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	var triageCalled bool
	var distillCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/memories/distill/triage":
			triageCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"should_distill": true})
		case "/memories/distill":
			distillCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"memories_created": 1})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	observer := NewNowledgeDistillObserver(provider, nowledgeLinkStoreStub{}, nil, nil, nil, nil, nil)
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
	}
	delta := ThreadDelta{
		AccountID:       rc.Run.AccountID,
		ThreadID:        rc.Run.ThreadID,
		UserID:          userID,
		AgentID:         "user_" + userID.String(),
		Messages:        []ThreadDeltaMessage{{Role: "user", Content: "今天决定切到 nowledge"}, {Role: "assistant", Content: "我会记住这个决定。"}},
		AssistantOutput: "我会记住这个决定。",
	}
	result := ThreadPersistResult{
		Handled:          true,
		Committed:        true,
		ExternalThreadID: "thread-1",
		Provider:         "nowledge",
	}

	if _, err := observer.AfterThreadPersist(context.Background(), rc, delta, result); err != nil {
		t.Fatalf("AfterThreadPersist: %v", err)
	}
	if !triageCalled || !distillCalled {
		t.Fatalf("expected nowledge distill flow to run, triage=%v distill=%v", triageCalled, distillCalled)
	}
}

func TestNowledgeDistillObserverRefreshesSnapshotAndImpressionWithoutCreatedCount(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	prevWindow := snapshotRefreshWindow
	prevInterval := snapshotRefreshRetryInterval
	prevAttempts := snapshotRefreshMaxAttempts
	snapshotRefreshWindow = 200 * time.Millisecond
	snapshotRefreshRetryInterval = 10 * time.Millisecond
	snapshotRefreshMaxAttempts = 3
	defer func() {
		snapshotRefreshWindow = prevWindow
		snapshotRefreshRetryInterval = prevInterval
		snapshotRefreshMaxAttempts = prevAttempts
	}()

	var triageCalled bool
	var distillCalled bool
	var listCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/memories/distill/triage":
			triageCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"should_distill": true})
		case "/memories/distill":
			distillCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"memories_created": 0})
		case "/memories":
			listCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memories": []map[string]any{{
					"id":         "mem-1",
					"title":      "迁移决策",
					"content":    "团队决定切到 nowledge knowledge 链路。",
					"confidence": 0.91,
				}},
			})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	snapshotStore := &nowledgeSnapshotStoreCapture{done: make(chan struct{}, 1)}
	impressionStore := &nowledgeImpressionStoreStub{score: 2}
	refreshTriggered := make(chan struct{}, 1)
	observer := NewNowledgeDistillObserver(
		provider,
		nowledgeLinkStoreStub{},
		nowledgeResolverStub{values: map[string]string{"memory.impression_score_threshold": "3"}},
		snapshotStore,
		nil,
		impressionStore,
		func(context.Context, memory.MemoryIdentity, uuid.UUID, uuid.UUID) {
			select {
			case refreshTriggered <- struct{}{}:
			default:
			}
		},
	)

	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID:  &userID,
		TraceID: "trace-nowledge-refresh",
	}
	delta := ThreadDelta{
		AccountID:       rc.Run.AccountID,
		ThreadID:        rc.Run.ThreadID,
		UserID:          userID,
		AgentID:         "user_" + userID.String(),
		Messages:        []ThreadDeltaMessage{{Role: "user", Content: "今天决定切到 nowledge knowledge"}, {Role: "assistant", Content: "我记住这个迁移决策。"}},
		AssistantOutput: "我记住这个迁移决策。",
	}
	result := ThreadPersistResult{
		Handled:          true,
		Committed:        true,
		ExternalThreadID: "thread-1",
		Provider:         "nowledge",
	}

	if _, err := observer.AfterThreadPersist(context.Background(), rc, delta, result); err != nil {
		t.Fatalf("AfterThreadPersist: %v", err)
	}

	select {
	case <-snapshotStore.done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timeout waiting for snapshot refresh")
	}

	if !triageCalled || !distillCalled || !listCalled {
		t.Fatalf("expected full nowledge flow, triage=%v distill=%v list=%v", triageCalled, distillCalled, listCalled)
	}
	if !strings.Contains(snapshotStore.block, "迁移决策") {
		t.Fatalf("expected snapshot block to include latest memory, got %q", snapshotStore.block)
	}
	if len(snapshotStore.hits) != 1 || snapshotStore.hits[0].URI != "nowledge://memory/mem-1" {
		t.Fatalf("unexpected snapshot hits: %#v", snapshotStore.hits)
	}
	if impressionStore.resetCalls != 1 {
		t.Fatalf("expected impression score reset once, got %d", impressionStore.resetCalls)
	}

	select {
	case <-refreshTriggered:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected impression refresh trigger")
	}
}

func TestLegacyMemoryDistillObserverRefreshesNowledgeSnapshotWithoutCreatedCount(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	prevWindow := snapshotRefreshWindow
	prevInterval := snapshotRefreshRetryInterval
	prevAttempts := snapshotRefreshMaxAttempts
	snapshotRefreshWindow = 200 * time.Millisecond
	snapshotRefreshRetryInterval = 10 * time.Millisecond
	snapshotRefreshMaxAttempts = 3
	defer func() {
		snapshotRefreshWindow = prevWindow
		snapshotRefreshRetryInterval = prevInterval
		snapshotRefreshMaxAttempts = prevAttempts
	}()

	var triageCalled bool
	var distillCalled bool
	var listCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/memories/distill/triage":
			triageCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"should_distill": true})
		case "/memories/distill":
			distillCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"memories_created": 0})
		case "/memories":
			listCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memories": []map[string]any{{
					"id":         "mem-legacy",
					"title":      "上一轮同步",
					"content":    "Nowledge 已经有可投影到 Settings 的记忆。",
					"confidence": 0.88,
				}},
			})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	snapshotStore := &nowledgeSnapshotStoreCapture{done: make(chan struct{}, 1)}
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID:         &userID,
		MemoryProvider: provider,
		TraceID:        "trace-nowledge-legacy-refresh",
	}
	delta := ThreadDelta{
		AccountID:       rc.Run.AccountID,
		ThreadID:        rc.Run.ThreadID,
		UserID:          userID,
		AgentID:         "user_" + userID.String(),
		Messages:        []ThreadDeltaMessage{{Role: "user", Content: "同步上一轮消息"}, {Role: "assistant", Content: "我会更新记忆。"}},
		AssistantOutput: "我会更新记忆。",
	}
	result := ThreadPersistResult{
		Handled:          true,
		Committed:        true,
		ExternalThreadID: "thread-legacy",
		Provider:         "nowledge",
	}
	observer := NewLegacyMemoryDistillObserver(snapshotStore, nil, nil, nil, nil, nil, nil)

	if _, err := observer.AfterThreadPersist(context.Background(), rc, delta, result); err != nil {
		t.Fatalf("AfterThreadPersist: %v", err)
	}

	select {
	case <-snapshotStore.done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timeout waiting for snapshot refresh")
	}

	if !triageCalled || !distillCalled || !listCalled {
		t.Fatalf("expected nowledge refresh flow, triage=%v distill=%v list=%v", triageCalled, distillCalled, listCalled)
	}
	if !strings.Contains(snapshotStore.block, "上一轮同步") {
		t.Fatalf("expected snapshot block to include listed memory, got %q", snapshotStore.block)
	}
	if len(snapshotStore.hits) != 1 || snapshotStore.hits[0].URI != "nowledge://memory/mem-legacy" {
		t.Fatalf("unexpected snapshot hits: %#v", snapshotStore.hits)
	}
}

func TestBuildNowledgeThreadPayloadCarriesOpenClawStyleMetadata(t *testing.T) {
	delta := ThreadDelta{
		RunID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ThreadID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		TraceID:         "trace-1",
		Messages:        []ThreadDeltaMessage{{Role: "user", Content: "第一句"}, {Role: "assistant", Content: "第二句"}},
		AssistantOutput: "最终回复",
	}

	payload := buildNowledgeThreadPayload(delta)
	if len(payload) != 3 {
		t.Fatalf("unexpected payload length: %d", len(payload))
	}
	for index, message := range payload {
		if message.Metadata["source"] != nowledgeThreadSource {
			t.Fatalf("unexpected source at %d: %#v", index, message.Metadata)
		}
		if message.Metadata["session_key"] != delta.ThreadID.String() {
			t.Fatalf("unexpected session_key at %d: %#v", index, message.Metadata)
		}
		if message.Metadata["session_id"] != delta.RunID.String() {
			t.Fatalf("unexpected session_id at %d: %#v", index, message.Metadata)
		}
		if message.Metadata["trace_id"] != delta.TraceID {
			t.Fatalf("unexpected trace_id at %d: %#v", index, message.Metadata)
		}
		if externalID, _ := message.Metadata["external_id"].(string); externalID == "" {
			t.Fatalf("missing external_id at %d: %#v", index, message.Metadata)
		}
	}
}

func TestNowledgeContextContributorInjectsBehavioralGuidanceWithoutRecall(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/working-memory":
			_ = json.NewEncoder(w).Encode(map[string]any{"exists": false, "content": ""})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	contributor := NewNowledgeContextContributor(provider, 5, 0)
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "hi"}}},
		},
	}

	segments, err := contributor.BeforePromptSegments(context.Background(), rc)
	if err != nil {
		t.Fatalf("BeforePromptSegments: %v", err)
	}
	guidance := ""
	for _, segment := range segments {
		if segment.Name == "hook.before.nowledge.guidance" {
			guidance = segment.Text
			break
		}
	}
	if guidance == "" {
		t.Fatal("expected guidance segment")
	}
	if !strings.Contains(guidance, "memory_search") || !strings.Contains(guidance, "memory_connections") || !strings.Contains(guidance, "memory_timeline") {
		t.Fatalf("guidance missing tool references: %q", guidance)
	}
	if strings.Contains(guidance, "已注入") {
		t.Fatalf("guidance should not claim injected context: %q", guidance)
	}
}

func TestNowledgeContextContributorInjectsWorkingMemoryWithoutAutoRecall(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/working-memory":
			_ = json.NewEncoder(w).Encode(map[string]any{"exists": true, "content": "今天聚焦 memory 系统"})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	contributor := NewNowledgeContextContributor(provider, 5, 0)
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "附件存储方案最后定了吗"}}},
		},
	}

	segments, err := contributor.BeforePromptSegments(context.Background(), rc)
	if err != nil {
		t.Fatalf("BeforePromptSegments: %v", err)
	}
	keys := map[string]PromptSegment{}
	for _, segment := range segments {
		keys[segment.Name] = segment
	}
	if _, ok := keys["hook.before.nowledge.working_memory"]; !ok {
		t.Fatal("expected working memory segment")
	}
	if _, ok := keys["hook.before.nowledge.recalled_memories"]; ok {
		t.Fatalf("unexpected recalled memories segment: %#v", keys["hook.before.nowledge.recalled_memories"])
	}
	guidance := ""
	for _, segment := range segments {
		if segment.Name == "hook.before.nowledge.guidance" {
			guidance = segment.Text
			break
		}
	}
	if !strings.Contains(guidance, "已注入") || !strings.Contains(guidance, "Working Memory") {
		t.Fatalf("guidance should acknowledge injected context: %q", guidance)
	}
}

func TestNowledgeContextContributorDoesNotSearchWhenUserMessageIsRecallable(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/working-memory":
			_ = json.NewEncoder(w).Encode(map[string]any{"exists": true, "content": "今天聚焦 memory 系统"})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	contributor := NewNowledgeContextContributor(provider, 5, 0)
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "附件存储方案最后定了吗"}}},
		},
	}

	segments, err := contributor.BeforePromptSegments(context.Background(), rc)
	if err != nil {
		t.Fatalf("BeforePromptSegments: %v", err)
	}
	if len(segments) == 0 || segments[0].Name != "hook.before.nowledge.working_memory" {
		t.Fatalf("unexpected segments: %#v", segments)
	}
	foundGuidance := false
	for _, segment := range segments {
		if segment.Name == "hook.before.nowledge.guidance" {
			foundGuidance = true
			break
		}
	}
	if !foundGuidance {
		t.Fatal("expected guidance segment")
	}
}

func TestNowledgeContextContributorBeforePromptSegmentsIncludesGuidanceSegment(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/working-memory":
			_ = json.NewEncoder(w).Encode(map[string]any{"exists": false, "content": ""})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	contributor := NewNowledgeContextContributor(provider, 5, 0)
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "hi"}}},
		},
	}

	segmentHook, ok := contributor.(BeforePromptSegmentsHook)
	if !ok {
		t.Fatal("expected before prompt segments hook")
	}
	segments, err := segmentHook.BeforePromptSegments(context.Background(), rc)
	if err != nil {
		t.Fatalf("BeforePromptSegments: %v", err)
	}
	found := false
	for _, segment := range segments {
		if segment.Name != "hook.before.nowledge.guidance" {
			continue
		}
		found = true
		if segment.Target != PromptTargetSystemPrefix || segment.Role != "system" {
			t.Fatalf("unexpected guidance segment placement: %#v", segment)
		}
		if !strings.Contains(segment.Text, "memory_search") {
			t.Fatalf("unexpected guidance text: %#v", segment)
		}
	}
	if !found {
		t.Fatal("expected guidance segment")
	}
}

func TestBuildNowledgeRecallQueryTieredThresholds(t *testing.T) {
	tests := []struct {
		name     string
		messages []memory.MemoryMessage
		want     string
	}{
		{
			name:     "empty messages",
			messages: nil,
			want:     "",
		},
		{
			name:     "too short (<3 chars)",
			messages: []memory.MemoryMessage{{Role: "user", Content: "hi"}},
			want:     "",
		},
		{
			name:     "slash command skipped",
			messages: []memory.MemoryMessage{{Role: "user", Content: "/help me"}},
			want:     "",
		},
		{
			name: "short query (3-39 chars) with context",
			messages: []memory.MemoryMessage{
				{Role: "user", Content: "之前讨论了数据库选型，最终选了 PostgreSQL 16"},
				{Role: "assistant", Content: "好的，PG 16 确认"},
				{Role: "user", Content: "附件方案呢"},
			},
			want: "附件方案呢",
		},
		{
			name:     "long query (>=40 chars) used directly",
			messages: []memory.MemoryMessage{{Role: "user", Content: "我想了解一下之前关于 memory system 的 recall 功能是怎么实现的，能给我讲讲吗"}},
			want:     "我想了解一下之前关于 memory system 的 recall 功能是怎么实现的，能给我讲讲吗",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &RunContext{}
			rc.SetBaseUserMessages(tt.messages)
			got := buildNowledgeRecallQuery(rc)
			if tt.want == "" {
				if got != "" {
					t.Fatalf("expected empty query, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("expected query to contain %q, got %q", tt.want, got)
			}
		})
	}
}

func TestBuildNowledgeRecallQueryShortWithContext(t *testing.T) {
	rc := &RunContext{}
	rc.SetBaseUserMessages([]memory.MemoryMessage{
		{Role: "user", Content: "之前讨论了数据库选型"},
		{Role: "assistant", Content: "选了 PG 16"},
		{Role: "user", Content: "附件方案呢"},
	})
	got := buildNowledgeRecallQuery(rc)
	if !strings.Contains(got, "附件方案呢") {
		t.Fatalf("expected latest message in query, got %q", got)
	}
	if !strings.Contains(got, "之前讨论了数据库选型") {
		t.Fatalf("expected context in query, got %q", got)
	}
}

func TestBuildRecalledKnowledgeBlock(t *testing.T) {
	results := []nowledge.SearchResult{
		{Title: "数据库选型", Score: 0.85, RelevanceReason: "matches db topic", Labels: []string{"architecture"}, Content: "团队最终选择了 PostgreSQL 16"},
		{Title: "附件方案", Score: 0.6, Content: "S3 + CDN 方案"},
		{Title: "低分记忆", Score: 0.3, Content: "不太相关"},
	}

	t.Run("no min score", func(t *testing.T) {
		block := buildRecalledKnowledgeBlock(results, 0)
		if !strings.Contains(block, "<recalled-knowledge>") {
			t.Fatal("expected recalled-knowledge block")
		}
		if !strings.Contains(block, "Untrusted historical context") {
			t.Fatal("expected safety prompt")
		}
		if !strings.Contains(block, "数据库选型") || !strings.Contains(block, "85%") {
			t.Fatal("expected first result with score")
		}
		if !strings.Contains(block, "matches db topic") {
			t.Fatal("expected relevance reason")
		}
		if !strings.Contains(block, "[architecture]") {
			t.Fatal("expected labels")
		}
		if !strings.Contains(block, "低分记忆") {
			t.Fatal("expected all results when minScore=0")
		}
	})

	t.Run("with min score filter", func(t *testing.T) {
		block := buildRecalledKnowledgeBlock(results, 0.5)
		if strings.Contains(block, "低分记忆") {
			t.Fatal("expected low-score result to be filtered out")
		}
		if !strings.Contains(block, "数据库选型") || !strings.Contains(block, "附件方案") {
			t.Fatal("expected high-score results to remain")
		}
	})

	t.Run("all filtered returns empty", func(t *testing.T) {
		block := buildRecalledKnowledgeBlock(results, 0.9)
		if block != "" {
			t.Fatalf("expected empty block when all filtered, got %q", block)
		}
	})
}

func TestNowledgeContextContributorRecallInjection(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/working-memory":
			_ = json.NewEncoder(w).Encode(map[string]any{"exists": false, "content": ""})
		case "/memories/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memories": []map[string]any{
					{"id": "m1", "title": "数据库选型", "content": "选了 PG 16", "score": 0.85, "relevance_reason": "db topic"},
				},
			})
		case "/threads/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"threads": []any{}})
		default:
			t.Fatalf("unexpected nowledge request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := nowledge.NewClient(nowledge.Config{BaseURL: srv.URL})
	contributor := NewNowledgeContextContributor(provider, 5, 0)
	userID := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID: &userID,
	}
	rc.SetBaseUserMessages([]memory.MemoryMessage{
		{Role: "user", Content: "我想了解一下之前关于数据库选型的讨论，最终方案是什么"},
	})

	segments, err := contributor.BeforePromptSegments(context.Background(), rc)
	if err != nil {
		t.Fatalf("BeforePromptSegments: %v", err)
	}

	var recallSeg *PromptSegment
	var guidanceSeg *PromptSegment
	for i := range segments {
		switch segments[i].Name {
		case "hook.before.nowledge.recalled_memories":
			recallSeg = &segments[i]
		case "hook.before.nowledge.guidance":
			guidanceSeg = &segments[i]
		}
	}
	if recallSeg == nil {
		t.Fatal("expected recalled_memories segment")
	}
	if recallSeg.Target != PromptTargetSystemPrefix {
		t.Fatalf("expected SystemPrefix target, got %v", recallSeg.Target)
	}
	if recallSeg.Stability != PromptStabilityVolatileTail {
		t.Fatalf("expected VolatileTail stability, got %v", recallSeg.Stability)
	}
	if recallSeg.CacheEligible {
		t.Fatal("expected CacheEligible=false")
	}
	if !strings.Contains(recallSeg.Text, "<recalled-knowledge>") {
		t.Fatalf("expected recalled-knowledge block, got %q", recallSeg.Text)
	}
	if !strings.Contains(recallSeg.Text, "数据库选型") {
		t.Fatalf("expected recall content, got %q", recallSeg.Text)
	}
	if guidanceSeg == nil {
		t.Fatal("expected guidance segment")
	}
	if !strings.Contains(guidanceSeg.Text, "已注入") && !strings.Contains(guidanceSeg.Text, "相关记忆") {
		t.Fatalf("guidance should mention injected recall: %q", guidanceSeg.Text)
	}
}

func TestBuildNowledgeGuidanceTextVariants(t *testing.T) {
	t.Run("both injected", func(t *testing.T) {
		text := buildNowledgeGuidanceText(true, true)
		if !strings.Contains(text, "Working Memory") || !strings.Contains(text, "相关记忆") {
			t.Fatalf("expected both mentioned: %q", text)
		}
		if !strings.Contains(text, "已注入") {
			t.Fatalf("expected injected notice: %q", text)
		}
	})
	t.Run("only working memory", func(t *testing.T) {
		text := buildNowledgeGuidanceText(true, false)
		if !strings.Contains(text, "Working Memory") {
			t.Fatalf("expected WM mentioned: %q", text)
		}
		if !strings.Contains(text, "已注入") {
			t.Fatalf("expected injected notice: %q", text)
		}
	})
	t.Run("neither injected", func(t *testing.T) {
		text := buildNowledgeGuidanceText(false, false)
		if !strings.Contains(text, "memory_search") {
			t.Fatalf("expected proactive search guidance: %q", text)
		}
		if strings.Contains(text, "已注入") {
			t.Fatalf("should not mention injection: %q", text)
		}
	})
}
