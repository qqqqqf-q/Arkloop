package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPrepareStickerDeliveryOutputs_OnlySplitsWhenPlaceholderExists(t *testing.T) {
	clean, segments := prepareStickerDeliveryOutputs([]string{"hello world"})
	if len(clean) != 1 || clean[0] != "hello world" {
		t.Fatalf("unexpected clean outputs: %#v", clean)
	}
	if len(segments) != 0 {
		t.Fatalf("expected no segments, got %#v", segments)
	}
}

func TestPrepareStickerDeliveryOutputs_ParsesStickerSequence(t *testing.T) {
	clean, segments := prepareStickerDeliveryOutputs([]string{"hi [sticker:abc] there"})
	if len(clean) != 1 || clean[0] != "hi there" {
		t.Fatalf("unexpected clean outputs: %#v", clean)
	}
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %#v", segments)
	}
	if segments[0].Kind != "text" || segments[0].Text != "hi" {
		t.Fatalf("unexpected first segment: %#v", segments[0])
	}
	if segments[1].Kind != "sticker" || segments[1].StickerID != "abc" {
		t.Fatalf("unexpected sticker segment: %#v", segments[1])
	}
	if segments[2].Kind != "text" || segments[2].Text != "there" {
		t.Fatalf("unexpected last segment: %#v", segments[2])
	}
}

func TestParseStickerBuilderOutput(t *testing.T) {
	description, tags, ok := parseStickerBuilderOutput("描述: 无语又想笑的狗头\n标签: 狗头, 阴阳怪气, 吐槽")
	if !ok {
		t.Fatal("expected parse success")
	}
	if description != "无语又想笑的狗头" {
		t.Fatalf("unexpected description: %q", description)
	}
	if tags != "狗头, 阴阳怪气, 吐槽" {
		t.Fatalf("unexpected tags: %q", tags)
	}
}

func TestStripStickerPlaceholders_PreservesFormatting(t *testing.T) {
	got := stripStickerPlaceholders("第一行\n\n[sticker:abc]\n第三行")
	if got != "第一行\n\n第三行" {
		t.Fatalf("unexpected stripped text: %q", got)
	}
}

func TestStickerToolMiddleware_AddsToolForTelegramRuns(t *testing.T) {
	rc := &RunContext{
		Run: data.Run{AccountID: uuid.New()},
		ChannelContext: &ChannelContext{
			ChannelType: "telegram",
		},
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolRegistry:  tools.NewRegistry(),
		PersonaDefinition: &personas.Definition{
			ToolAllowlist: []string{stickerSearchToolName, stickerListToolName},
		},
	}
	mw := NewStickerToolMiddleware(fakeStickerQueryDB{})
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if _, ok := rc.AllowlistSet[stickerSearchToolName]; !ok {
		t.Fatalf("expected %s in allowlist", stickerSearchToolName)
	}
	if _, ok := rc.AllowlistSet[stickerListToolName]; !ok {
		t.Fatalf("expected %s in allowlist", stickerListToolName)
	}
	if rc.ToolExecutors[stickerSearchToolName] == nil {
		t.Fatalf("expected %s executor bound", stickerSearchToolName)
	}
	if rc.ToolExecutors[stickerListToolName] == nil {
		t.Fatalf("expected %s executor bound", stickerListToolName)
	}
	if _, ok := rc.ToolRegistry.Get(stickerSearchToolName); !ok {
		t.Fatalf("expected %s registered", stickerSearchToolName)
	}
	if _, ok := rc.ToolRegistry.Get(stickerListToolName); !ok {
		t.Fatalf("expected %s registered", stickerListToolName)
	}
}

func TestStickerToolMiddleware_RespectsPersonaAllowlist(t *testing.T) {
	rc := &RunContext{
		Run: data.Run{AccountID: uuid.New()},
		ChannelContext: &ChannelContext{
			ChannelType: "telegram",
		},
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolRegistry:  tools.NewRegistry(),
		PersonaDefinition: &personas.Definition{
			ToolAllowlist: []string{"read"},
		},
	}
	mw := NewStickerToolMiddleware(fakeStickerQueryDB{})
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if _, ok := rc.AllowlistSet[stickerSearchToolName]; ok {
		t.Fatalf("did not expect %s in allowlist", stickerSearchToolName)
	}
	if rc.ToolExecutors[stickerSearchToolName] != nil {
		t.Fatalf("did not expect %s executor bound", stickerSearchToolName)
	}
	if _, ok := rc.AllowlistSet[stickerListToolName]; ok {
		t.Fatalf("did not expect %s in allowlist", stickerListToolName)
	}
	if rc.ToolExecutors[stickerListToolName] != nil {
		t.Fatalf("did not expect %s executor bound", stickerListToolName)
	}
}

func TestStickerToolMiddleware_AllowsOnlyStickerListWhenListed(t *testing.T) {
	rc := &RunContext{
		Run: data.Run{AccountID: uuid.New()},
		ChannelContext: &ChannelContext{
			ChannelType: "telegram",
		},
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolRegistry:  tools.NewRegistry(),
		PersonaDefinition: &personas.Definition{
			ToolAllowlist: []string{stickerListToolName},
		},
	}
	mw := NewStickerToolMiddleware(fakeStickerQueryDB{})
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if _, ok := rc.AllowlistSet[stickerSearchToolName]; ok {
		t.Fatalf("did not expect %s in allowlist", stickerSearchToolName)
	}
	if rc.ToolExecutors[stickerSearchToolName] != nil {
		t.Fatalf("did not expect %s executor bound", stickerSearchToolName)
	}
	if _, ok := rc.AllowlistSet[stickerListToolName]; !ok {
		t.Fatalf("expected %s in allowlist", stickerListToolName)
	}
	if rc.ToolExecutors[stickerListToolName] == nil {
		t.Fatalf("expected %s executor bound", stickerListToolName)
	}
}

func TestStickerToolMiddleware_DenySearchKeepsStickerList(t *testing.T) {
	rc := &RunContext{
		Run: data.Run{AccountID: uuid.New()},
		ChannelContext: &ChannelContext{
			ChannelType: "telegram",
		},
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolRegistry:  tools.NewRegistry(),
		ToolDenylist:  []string{stickerSearchToolName},
	}
	mw := NewStickerToolMiddleware(fakeStickerQueryDB{})
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if _, ok := rc.AllowlistSet[stickerSearchToolName]; ok {
		t.Fatalf("did not expect %s in allowlist", stickerSearchToolName)
	}
	if rc.ToolExecutors[stickerSearchToolName] != nil {
		t.Fatalf("did not expect %s executor bound", stickerSearchToolName)
	}
	if _, ok := rc.AllowlistSet[stickerListToolName]; !ok {
		t.Fatalf("expected %s in allowlist", stickerListToolName)
	}
	if rc.ToolExecutors[stickerListToolName] == nil {
		t.Fatalf("expected %s executor bound", stickerListToolName)
	}
}

func TestRenderHotStickerPrompt_EscapesXMLAttributes(t *testing.T) {
	got := renderHotStickerPrompt([]data.AccountSticker{{
		ContentHash: `hash"&<>`,
		ShortTags:   `doge" & <meme>`,
	}})
	if !strings.Contains(got, `id="hash&#34;&amp;&lt;&gt;"`) {
		t.Fatalf("expected escaped id, got %q", got)
	}
	if !strings.Contains(got, `short="doge&#34; &amp; &lt;meme&gt;"`) {
		t.Fatalf("expected escaped short tags, got %q", got)
	}
}

func TestStickerInjectMiddleware_AddsInstructionWithoutHotList(t *testing.T) {
	rc := &RunContext{
		Run: data.Run{AccountID: uuid.New()},
		ChannelContext: &ChannelContext{
			ChannelType: "telegram",
		},
	}
	mw := NewStickerInjectMiddleware(fakeStickerListErrorDB{})
	if err := mw(context.Background(), rc, func(ctx context.Context, rc *RunContext) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	found := false
	segmentText := ""
	for _, segment := range rc.PromptSegments() {
		if segment.Name != "telegram.sticker_instruction" {
			continue
		}
		found = true
		segmentText = segment.Text
		break
	}
	if !found {
		t.Fatal("expected sticker instruction segment")
	}
	if !strings.Contains(segmentText, "[sticker:<id>]") {
		t.Fatalf("expected sticker send placeholder instruction, got %q", segmentText)
	}
	if !strings.Contains(segmentText, "sticker_list") || !strings.Contains(segmentText, "sticker_search") {
		t.Fatalf("expected sticker tool guidance, got %q", segmentText)
	}
}

func TestStickerToolSpecs_DescribeHowToSend(t *testing.T) {
	searchDesc := ""
	if stickerSearchLlmSpec.Description != nil {
		searchDesc = *stickerSearchLlmSpec.Description
	}
	if !strings.Contains(searchDesc, "[sticker:<id>]") || !strings.Contains(searchDesc, "telegram_send_file") {
		t.Fatalf("unexpected sticker_search description: %q", searchDesc)
	}
	listDesc := ""
	if stickerListLlmSpec.Description != nil {
		listDesc = *stickerListLlmSpec.Description
	}
	if !strings.Contains(listDesc, "[sticker:<id>]") || !strings.Contains(listDesc, "telegram_send_file") {
		t.Fatalf("unexpected sticker_list description: %q", listDesc)
	}
}

func TestStickerPrepareMiddleware_SkipsLLMWithoutPreview(t *testing.T) {
	mw := NewStickerPrepareMiddleware(
		fakeStickerPrepareDB{
			row: fakeStickerPrepareRow{
				sticker: &data.AccountSticker{
					AccountID:    uuid.New(),
					ContentHash:  "hash",
					StorageKey:   "raw.webp",
					IsRegistered: false,
				},
			},
		},
		fakeStickerAttachmentStore{},
	)
	nextCalled := false
	err := mw(context.Background(), &RunContext{
		Run:       data.Run{AccountID: uuid.New()},
		InputJSON: map[string]any{"run_kind": "sticker_register", "sticker_id": "hash"},
	}, func(ctx context.Context, rc *RunContext) error {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if nextCalled {
		t.Fatal("expected middleware to short-circuit builder run")
	}
}

func TestStickerPrepareMiddleware_SkipsLLMWithoutVisionRoute(t *testing.T) {
	mw := NewStickerPrepareMiddleware(
		fakeStickerPrepareDB{
			row: fakeStickerPrepareRow{
				sticker: &data.AccountSticker{
					AccountID:         uuid.New(),
					ContentHash:       "hash",
					StorageKey:        "raw.webp",
					PreviewStorageKey: "preview.jpg",
				},
			},
		},
		fakeStickerAttachmentStore{},
	)
	nextCalled := false
	err := mw(context.Background(), &RunContext{
		Run:       data.Run{AccountID: uuid.New()},
		InputJSON: map[string]any{"run_kind": "sticker_register", "sticker_id": "hash"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{Model: "gpt-3.5-turbo"},
		},
	}, func(ctx context.Context, rc *RunContext) error {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if nextCalled {
		t.Fatal("expected middleware to skip builder LLM path")
	}
}

func TestStickerPrepareMiddleware_PropagatesPersistenceErrors(t *testing.T) {
	dbErr := errors.New("write failed")
	mw := NewStickerPrepareMiddleware(
		fakeStickerPrepareDB{
			row: fakeStickerPrepareRow{
				sticker: &data.AccountSticker{
					AccountID:         uuid.New(),
					ContentHash:       "hash",
					StorageKey:        "raw.webp",
					PreviewStorageKey: "preview.jpg",
				},
			},
			execErr: dbErr,
		},
		fakeStickerAttachmentStore{
			bytes: []byte("image"),
			mime:  "image/jpeg",
		},
	)
	err := mw(context.Background(), &RunContext{
		Run:       data.Run{AccountID: uuid.New()},
		InputJSON: map[string]any{"run_kind": "sticker_register", "sticker_id": "hash"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				Model: "gpt-4o",
				AdvancedJSON: map[string]any{
					"available_catalog": map[string]any{
						"input_modalities": []any{"text", "image"},
					},
				},
			},
		},
	}, func(ctx context.Context, rc *RunContext) error {
		rc.FinalAssistantOutput = "描述: 开心到发疯\n标签: 开心, 激动"
		return nil
	})
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected exec error, got %v", err)
	}
}

type fakeStickerQueryDB struct{}

func (fakeStickerQueryDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (fakeStickerQueryDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeStickerRow{}
}

type fakeStickerRow struct{}

func (fakeStickerRow) Scan(...any) error { return nil }

type fakeStickerListErrorDB struct{}

func (fakeStickerListErrorDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("list hot failed")
}

func (fakeStickerListErrorDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeStickerRow{}
}

type fakeStickerPrepareDB struct {
	row     fakeStickerPrepareRow
	execErr error
}

func (db fakeStickerPrepareDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, db.execErr
}

func (db fakeStickerPrepareDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (db fakeStickerPrepareDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return db.row
}

func (db fakeStickerPrepareDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

type fakeStickerPrepareRow struct {
	sticker *data.AccountSticker
	err     error
}

func (r fakeStickerPrepareRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.sticker == nil {
		return pgx.ErrNoRows
	}
	values := []any{
		r.sticker.ID,
		r.sticker.AccountID,
		r.sticker.ContentHash,
		r.sticker.StorageKey,
		r.sticker.PreviewStorageKey,
		r.sticker.FileSize,
		r.sticker.MimeType,
		r.sticker.IsAnimated,
		r.sticker.ShortTags,
		r.sticker.LongDesc,
		r.sticker.UsageCount,
		r.sticker.LastUsedAt,
		r.sticker.IsRegistered,
		r.sticker.CreatedAt,
		r.sticker.UpdatedAt,
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = values[i].(uuid.UUID)
		case *string:
			*d = values[i].(string)
		case *int64:
			*d = values[i].(int64)
		case *bool:
			*d = values[i].(bool)
		case *int:
			*d = values[i].(int)
		case **time.Time:
			*d = values[i].(*time.Time)
		case *time.Time:
			*d = values[i].(time.Time)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

type fakeStickerAttachmentStore struct {
	bytes []byte
	mime  string
	err   error
}

func (s fakeStickerAttachmentStore) Get(context.Context, string) ([]byte, error) {
	return s.bytes, s.err
}

func (s fakeStickerAttachmentStore) GetWithContentType(context.Context, string) ([]byte, string, error) {
	return s.bytes, s.mime, s.err
}
