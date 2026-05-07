package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type impressionTestGateway struct{}

func (impressionTestGateway) Stream(_ context.Context, _ llm.Request, _ func(llm.StreamEvent) error) error {
	return nil
}

type impressionTestRow struct {
	values []any
	err    error
}

func (r impressionTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch ptr := dest[i].(type) {
		case *string:
			value, ok := r.values[i].(string)
			if !ok {
				return fmt.Errorf("unexpected value type %T", r.values[i])
			}
			*ptr = value
		default:
			return fmt.Errorf("unexpected scan target %T", dest[i])
		}
	}
	return nil
}

type impressionTestDB struct {
	selector string
}

func (db impressionTestDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, nil
}

func (db impressionTestDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if db.selector == "" {
		return impressionTestRow{err: pgx.ErrNoRows}
	}
	return impressionTestRow{values: []any{db.selector}}
}

func (db impressionTestDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (db impressionTestDB) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return nil, fmt.Errorf("BeginTx should not be called in this test")
}

type impressionMemoryProviderStub struct {
	contents map[string]string
	children map[string][]string
}

func (p impressionMemoryProviderStub) Find(_ context.Context, _ memory.MemoryIdentity, _ string, _ string, _ int) ([]memory.MemoryHit, error) {
	return nil, nil
}

func (p impressionMemoryProviderStub) Content(_ context.Context, _ memory.MemoryIdentity, uri string, layer memory.MemoryLayer) (string, error) {
	key := string(layer) + ":" + uri
	value, ok := p.contents[key]
	if !ok {
		return "", fmt.Errorf("unexpected content request %s", key)
	}
	return value, nil
}

func (p impressionMemoryProviderStub) ListDir(_ context.Context, _ memory.MemoryIdentity, uri string) ([]string, error) {
	children, ok := p.children[uri]
	if !ok {
		return nil, nil
	}
	return append([]string(nil), children...), nil
}

func (p impressionMemoryProviderStub) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	return nil
}

func (p impressionMemoryProviderStub) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

func (p impressionMemoryProviderStub) Write(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ memory.MemoryEntry) error {
	return nil
}

func (p impressionMemoryProviderStub) Delete(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

type impressionFragmentProviderStub struct {
	fragments   []memory.MemoryFragment
	treeTouched bool
}

func (p *impressionFragmentProviderStub) Find(context.Context, memory.MemoryIdentity, string, string, int) ([]memory.MemoryHit, error) {
	return nil, nil
}

func (p *impressionFragmentProviderStub) Content(context.Context, memory.MemoryIdentity, string, memory.MemoryLayer) (string, error) {
	p.treeTouched = true
	return "", fmt.Errorf("tree path should not be used")
}

func (p *impressionFragmentProviderStub) ListDir(context.Context, memory.MemoryIdentity, string) ([]string, error) {
	p.treeTouched = true
	return nil, fmt.Errorf("tree path should not be used")
}

func (p *impressionFragmentProviderStub) AppendSessionMessages(context.Context, memory.MemoryIdentity, string, []memory.MemoryMessage) error {
	return nil
}

func (p *impressionFragmentProviderStub) CommitSession(context.Context, memory.MemoryIdentity, string) error {
	return nil
}

func (p *impressionFragmentProviderStub) Write(context.Context, memory.MemoryIdentity, memory.MemoryScope, memory.MemoryEntry) error {
	return nil
}

func (p *impressionFragmentProviderStub) Delete(context.Context, memory.MemoryIdentity, string) error {
	return nil
}

func (p *impressionFragmentProviderStub) ListFragments(_ context.Context, _ memory.MemoryIdentity, _ int) ([]memory.MemoryFragment, error) {
	return append([]memory.MemoryFragment(nil), p.fragments...), nil
}

func TestImpressionPrepareMiddlewareUsesAccountToolRoute(t *testing.T) {
	routeCfg := routing.ProviderRoutingConfig{
		Credentials: []routing.ProviderCredential{
			{
				ID:           "cred-tool",
				Name:         "tool-cred",
				OwnerKind:    routing.CredentialScopePlatform,
				ProviderKind: routing.ProviderKindStub,
			},
		},
		Routes: []routing.ProviderRouteRule{
			{
				ID:           "route-chat",
				Model:        "chat-model",
				CredentialID: "cred-tool",
			},
			{
				ID:           "route-tool",
				Model:        "tool-model",
				CredentialID: "cred-tool",
				Priority:     10,
			},
		},
	}
	loader := routing.NewDesktopSQLiteRoutingLoader(func(context.Context) (routing.ProviderRoutingConfig, error) {
		return routeCfg, nil
	}, routing.ProviderRoutingConfig{})
	auxGateway := impressionTestGateway{}

	mw := NewImpressionPrepareMiddleware(nil, impressionTestDB{selector: "tool-cred^tool-model"}, auxGateway, false, loader)

	uid := uuid.New()
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
		},
		InputJSON: map[string]any{
			"run_kind": "impression",
		},
		UserID:  &uid,
		Gateway: auxGateway,
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:    "route-chat",
				Model: "chat-model",
			},
			Credential: routeCfg.Credentials[0],
		},
		RoutingByokEnabled: true,
	}

	err := mw(context.Background(), rc, func(_ context.Context, inner *RunContext) error {
		if inner.Gateway == nil {
			t.Fatal("expected gateway override")
		}
		if inner.SelectedRoute == nil {
			t.Fatal("expected selected route override")
		}
		if inner.SelectedRoute.Route.ID != "route-tool" {
			t.Fatalf("got route id %q, want %q", inner.SelectedRoute.Route.ID, "route-tool")
		}
		if inner.SelectedRoute.Route.Model != "tool-model" {
			t.Fatalf("got model %q, want %q", inner.SelectedRoute.Route.Model, "tool-model")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImpressionPrepareMiddlewareInjectsOverviewAndLeafReadContent(t *testing.T) {
	uid := uuid.New()
	rootURI := memory.SelfURI(uid.String())
	projectDirURI := rootURI + "projects/"
	leafURI := projectDirURI + "arkloop"

	provider := impressionMemoryProviderStub{
		contents: map[string]string{
			"overview:" + rootURI:       "owner 总览：关注 Arkloop 与长期记忆质量",
			"overview:" + projectDirURI: "projects 总览：Arkloop 是当前重点项目",
			"read:" + leafURI:           "Vic 和 owner 正在讨论 impression 应该更长，只注入画像，不要整块 memory。",
		},
		children: map[string][]string{
			rootURI:       {projectDirURI},
			projectDirURI: {leafURI},
		},
	}

	mw := NewImpressionPrepareMiddleware(nil, nil, nil, false, nil)
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
		},
		InputJSON: map[string]any{
			"run_kind": "impression",
		},
		UserID:         &uid,
		MemoryProvider: provider,
	}

	err := mw(context.Background(), rc, func(_ context.Context, inner *RunContext) error {
		if len(inner.Messages) != 1 {
			t.Fatalf("expected one injected message, got %d", len(inner.Messages))
		}
		if inner.Messages[0].Role != "user" {
			t.Fatalf("expected injected user message, got %q", inner.Messages[0].Role)
		}
		text := llm.VisibleMessageText(inner.Messages[0])
		if !strings.Contains(text, "## 记忆目录概览") {
			t.Fatalf("expected overview section, got %q", text)
		}
		if !strings.Contains(text, "## 记忆条目原文") {
			t.Fatalf("expected leaf read section, got %q", text)
		}
		if !strings.Contains(text, "Vic 和 owner 正在讨论 impression 应该更长") {
			t.Fatalf("expected L2 leaf content in injected message, got %q", text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImpressionPrepareMiddlewareUsesFragmentsWhenAvailable(t *testing.T) {
	uid := uuid.New()
	provider := &impressionFragmentProviderStub{
		fragments: []memory.MemoryFragment{{
			ID:         "mem-1",
			URI:        "nowledge://memory/mem-1",
			Title:      "Owner preference",
			Content:    "回答保持简洁，聚焦后端语义和真实数据闭环。",
			Abstract:   "owner 偏好简洁、真实数据闭环的后端讨论。",
			Score:      0.875,
			Labels:     []string{"preference", "backend"},
			RecordedAt: "2026-05-06T18:20:17Z",
		}},
	}

	mw := NewImpressionPrepareMiddleware(nil, nil, nil, false, nil)
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
		},
		InputJSON: map[string]any{
			"run_kind": "impression",
		},
		UserID:         &uid,
		MemoryProvider: provider,
	}

	err := mw(context.Background(), rc, func(_ context.Context, inner *RunContext) error {
		if len(inner.Messages) != 1 {
			t.Fatalf("expected one injected message, got %d", len(inner.Messages))
		}
		text := llm.VisibleMessageText(inner.Messages[0])
		if !strings.Contains(text, "## 记忆条目") {
			t.Fatalf("expected fragment section, got %q", text)
		}
		if strings.Contains(text, "## 记忆目录概览") {
			t.Fatalf("did not expect tree overview section, got %q", text)
		}
		if !strings.Contains(text, "Owner preference") || !strings.Contains(text, "后端语义和真实数据闭环") {
			t.Fatalf("expected fragment title and content, got %q", text)
		}
		if !strings.Contains(text, "URI：nowledge://memory/mem-1") {
			t.Fatalf("expected fragment uri for follow-up reads, got %q", text)
		}
		if !strings.Contains(text, "时间：2026-05-06T18:20:17Z") {
			t.Fatalf("expected fragment recorded time, got %q", text)
		}
		if !strings.Contains(text, "标签：preference, backend") {
			t.Fatalf("expected fragment labels, got %q", text)
		}
		if !strings.Contains(text, "重要度：0.875") {
			t.Fatalf("expected fragment score, got %q", text)
		}
		if !strings.Contains(text, "摘要：owner 偏好简洁、真实数据闭环的后端讨论。") {
			t.Fatalf("expected fragment abstract, got %q", text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.treeTouched {
		t.Fatal("did not expect tree snapshot path when fragment source is available")
	}
}

func TestImpressionPrepareMiddlewareSkipsInjectionWhenFragmentsEmpty(t *testing.T) {
	uid := uuid.New()
	provider := &impressionFragmentProviderStub{}

	mw := NewImpressionPrepareMiddleware(nil, nil, nil, false, nil)
	rc := &RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
		},
		InputJSON: map[string]any{
			"run_kind": "impression",
		},
		UserID:         &uid,
		MemoryProvider: provider,
	}

	err := mw(context.Background(), rc, func(_ context.Context, inner *RunContext) error {
		if len(inner.Messages) != 0 {
			t.Fatalf("expected no injected message, got %#v", inner.Messages)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
