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

func (c *notebookContextContributor) BeforePromptSegments(ctx context.Context, rc *RunContext) (PromptSegments, error) {
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
	return PromptSegments{{
		Name:          "hook.before.notebook.notebook",
		Target:        PromptTargetSystemPrefix,
		Role:          "system",
		Text:          "<notebook>\n" + content + "\n</notebook>",
		Stability:     PromptStabilitySessionPrefix,
		CacheEligible: true,
	}}, nil
}

func (c *notebookContextContributor) AfterPromptSegments(context.Context, *RunContext, string) (PromptSegments, error) {
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

func (c *impressionContextContributor) BeforePromptSegments(ctx context.Context, rc *RunContext) (PromptSegments, error) {
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
	return PromptSegments{{
		Name:          "hook.before.impression.impression",
		Target:        PromptTargetSystemPrefix,
		Role:          "system",
		Text:          "<impression>\n" + content + "\n</impression>",
		Stability:     PromptStabilitySessionPrefix,
		CacheEligible: true,
	}}, nil
}

func (c *impressionContextContributor) AfterPromptSegments(context.Context, *RunContext, string) (PromptSegments, error) {
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

func (o *legacyMemoryDistillObserver) AfterThreadPersist(ctx context.Context, rc *RunContext, delta ThreadDelta, result ThreadPersistResult) (PersistObservers, error) {
	if o == nil || rc == nil || rc.UserID == nil || rc.MemoryProvider == nil {
		return nil, nil
	}
	// Hand-triggered impression rebuild runs should not re-enter semantic distill/snapshot/impression-score
	// lifecycles, otherwise a single click can cascade into multiple impression refresh runs.
	if rc.ImpressionRun || isImpressionRun(rc) {
		return nil, nil
	}

	// Nowledge semantics: thread persistence is handled by the HookRegistry provider; once committed,
	// we can distill and refresh snapshots from here to avoid a split between hook vs legacy flows.
	if typed, ok := rc.MemoryProvider.(*nowledge.Client); ok {
		if !resolveDistillEnabled(ctx, o.configResolver) {
			return nil, nil
		}
		if result.Err != nil || !result.Handled || !result.Committed {
			return nil, nil
		}

		threadID := strings.TrimSpace(result.ExternalThreadID)
		if threadID == "" {
			return nil, nil
		}
		conversation := buildNowledgeConversation(delta)
		if strings.TrimSpace(conversation) == "" {
			return nil, nil
		}
		ident := memory.MemoryIdentity{
			AccountID: delta.AccountID,
			UserID:    delta.UserID,
			AgentID:   delta.AgentID,
		}
		triage, err := typed.TriageConversation(ctx, ident, conversation)
		if err != nil || !triage.ShouldDistill {
			return nil, err
		}
		distill, err := typed.DistillThread(ctx, ident, threadID, buildNowledgeThreadTitle(delta), conversation)
		if err != nil {
			return nil, err
		}
		if distill.MemoriesCreated <= 0 {
			return nil, nil
		}
		if o.impStore != nil {
			addImpressionScore(ctx, o.impStore, ident, impressionScoreForRun(rc), o.configResolver, o.impRefresh)
		}
		scheduleSnapshotRefresh(
			typed,
			o.snap,
			o.mdb,
			rc.Run.ID,
			rc.TraceID,
			ident,
			threadID,
			buildNowledgeSnapshotQueries(delta),
			"memory.distill",
			"distill",
		)
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
