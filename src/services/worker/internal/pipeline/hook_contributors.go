package pipeline

import (
	"context"
	"strings"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/memory/nowledge"

	"github.com/google/uuid"
)

type notebookSnapshotReader interface {
	GetSnapshot(ctx context.Context, accountID, userID uuid.UUID, agentID string) (string, error)
}

type notebookContextContributor struct {
	reader notebookSnapshotReader
}

func NewNotebookContextContributor(reader notebookSnapshotReader) ContextContributor {
	if reader == nil {
		return nil
	}
	return &notebookContextContributor{reader: reader}
}

func (c *notebookContextContributor) HookProviderName() string { return "notebook" }

func (c *notebookContextContributor) BeforePromptAssemble(ctx context.Context, rc *RunContext) (PromptFragments, error) {
	if c == nil || c.reader == nil || rc == nil || rc.UserID == nil {
		return nil, nil
	}
	block, err := c.reader.GetSnapshot(ctx, rc.Run.AccountID, *rc.UserID, StableAgentID(rc))
	if err != nil {
		appendAsyncRunEvent(ctx, rc.MemoryServiceDB, rc.Run.ID, events.NewEmitter(rc.TraceID).Emit("notebook.snapshot.read_failed", map[string]any{
			"message": err.Error(),
		}, nil, nil))
		return nil, err
	}
	content := unwrapPromptBlock(block, "notebook")
	if content == "" {
		return nil, nil
	}
	return PromptFragments{{
		Key:      "notebook",
		XMLTag:   "notebook",
		Content:  content,
		Source:   "notebook",
		Priority: 100,
	}}, nil
}

func (c *notebookContextContributor) AfterPromptAssemble(context.Context, *RunContext, string) (PromptFragments, error) {
	return nil, nil
}

type impressionContextContributor struct {
	store ImpressionStore
}

func NewImpressionContextContributor(store ImpressionStore) ContextContributor {
	if store == nil {
		return nil
	}
	return &impressionContextContributor{store: store}
}

func (c *impressionContextContributor) HookProviderName() string { return "impression" }

func (c *impressionContextContributor) BeforePromptAssemble(ctx context.Context, rc *RunContext) (PromptFragments, error) {
	if c == nil || c.store == nil || rc == nil || rc.UserID == nil {
		return nil, nil
	}
	impression, found, err := c.store.Get(ctx, rc.Run.AccountID, *rc.UserID, StableAgentID(rc))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	content := unwrapPromptBlock(impression, "impression")
	if content == "" {
		return nil, nil
	}
	return PromptFragments{{
		Key:      "impression",
		XMLTag:   "impression",
		Content:  content,
		Source:   "impression",
		Priority: 200,
	}}, nil
}

func (c *impressionContextContributor) AfterPromptAssemble(context.Context, *RunContext, string) (PromptFragments, error) {
	return nil, nil
}

type legacyMemoryDistillObserver struct {
	snap           MemorySnapshotStore
	mdb            data.MemoryMiddlewareDB
	configResolver sharedconfig.Resolver
	impStore       ImpressionStore
	impRefresh     ImpressionRefreshFunc
}

func NewLegacyMemoryDistillObserver(
	snap MemorySnapshotStore,
	mdb data.MemoryMiddlewareDB,
	configResolver sharedconfig.Resolver,
	impStore ImpressionStore,
	impRefresh ImpressionRefreshFunc,
) AfterThreadPersistHook {
	return &legacyMemoryDistillObserver{
		snap:           snap,
		mdb:            mdb,
		configResolver: configResolver,
		impStore:       impStore,
		impRefresh:     impRefresh,
	}
}

func (o *legacyMemoryDistillObserver) HookProviderName() string { return "legacy_memory_distill" }

func (o *legacyMemoryDistillObserver) AfterThreadPersist(_ context.Context, rc *RunContext, delta ThreadDelta, result ThreadPersistResult) (PersistObservers, error) {
	if o == nil || rc == nil || rc.UserID == nil || rc.MemoryProvider == nil {
		return nil, nil
	}
	if _, ok := rc.MemoryProvider.(*nowledge.Client); ok {
		return nil, nil
	}
	ident := memory.MemoryIdentity{
		AccountID: rc.Run.AccountID,
		UserID:    *rc.UserID,
		AgentID:   StableAgentID(rc),
	}
	baseMessages := rc.BaseUserMessages()
	if len(baseMessages) == 0 && len(rc.RuntimeUserMessages()) == 0 && len(delta.Messages) > 0 {
		baseMessages = make([]memory.MemoryMessage, 0, len(delta.Messages))
		for _, msg := range delta.Messages {
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			baseMessages = append(baseMessages, memory.MemoryMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	distillAfterRun(rc.MemoryProvider, o.snap, o.mdb, o.configResolver, rc, ident, baseMessages, o.impStore, o.impRefresh)
	return nil, nil
}

func unwrapPromptBlock(block string, tag string) string {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" {
		return ""
	}
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(trimmed, openTag)
	end := strings.LastIndex(trimmed, closeTag)
	if start >= 0 && end > start {
		start += len(openTag)
		return strings.TrimSpace(trimmed[start:end])
	}
	return trimmed
}
