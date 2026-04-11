package pipeline

import (
	"fmt"
	"sync"
)

type HookRegistry struct {
	mu sync.RWMutex

	beforePromptAssemble []BeforePromptAssembleHook
	afterPromptAssemble  []AfterPromptAssembleHook
	beforeModelCall      []BeforeModelCallHook
	afterModelResponse   []AfterModelResponseHook
	afterToolCall        []AfterToolCallHook
	beforeCompact        []BeforeCompactHook
	afterCompact         []AfterCompactHook
	beforeThreadPersist  []BeforeThreadPersistHook
	afterThreadPersist   []AfterThreadPersistHook

	contextContributors []ContextContributor
	compactionAdvisors  []CompactionAdvisor
	threadProvider      ThreadPersistenceProvider
}

func NewHookRegistry() *HookRegistry {
	return &HookRegistry{}
}

func (r *HookRegistry) RegisterContextContributor(contributor ContextContributor) {
	if r == nil || contributor == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contextContributors = append(r.contextContributors, contributor)
	r.beforePromptAssemble = append(r.beforePromptAssemble, contributor)
	r.afterPromptAssemble = append(r.afterPromptAssemble, contributor)
}

func (r *HookRegistry) RegisterCompactionAdvisor(advisor CompactionAdvisor) {
	if r == nil || advisor == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.compactionAdvisors = append(r.compactionAdvisors, advisor)
	r.beforeCompact = append(r.beforeCompact, advisor)
	r.afterCompact = append(r.afterCompact, advisor)
}

func (r *HookRegistry) RegisterModelLifecycleHook(hook ModelLifecycleHook) {
	if r == nil || hook == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beforeModelCall = append(r.beforeModelCall, hook)
	r.afterModelResponse = append(r.afterModelResponse, hook)
	r.afterToolCall = append(r.afterToolCall, hook)
}

func (r *HookRegistry) SetThreadPersistenceProvider(provider ThreadPersistenceProvider) error {
	if r == nil || provider == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.threadProvider != nil {
		return fmt.Errorf("thread persistence provider already set: %s", providerName(r.threadProvider))
	}
	r.threadProvider = provider
	return nil
}

func (r *HookRegistry) RegisterBeforeThreadPersistHook(hook BeforeThreadPersistHook) {
	if r == nil || hook == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beforeThreadPersist = append(r.beforeThreadPersist, hook)
}

func (r *HookRegistry) RegisterAfterThreadPersistHook(hook AfterThreadPersistHook) {
	if r == nil || hook == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.afterThreadPersist = append(r.afterThreadPersist, hook)
}

func (r *HookRegistry) ActiveContextContributorNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.contextContributors))
	for _, contributor := range r.contextContributors {
		names = append(names, providerName(contributor))
	}
	return names
}

func (r *HookRegistry) ActiveCompactionAdvisorNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.compactionAdvisors))
	for _, advisor := range r.compactionAdvisors {
		names = append(names, providerName(advisor))
	}
	return names
}

func (r *HookRegistry) ActiveThreadPersistenceProviderName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.threadProvider == nil {
		return ""
	}
	return providerName(r.threadProvider)
}

func (r *HookRegistry) beforePromptHooks() []BeforePromptAssembleHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]BeforePromptAssembleHook(nil), r.beforePromptAssemble...)
}

func (r *HookRegistry) afterPromptHooks() []AfterPromptAssembleHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AfterPromptAssembleHook(nil), r.afterPromptAssemble...)
}

func (r *HookRegistry) beforeModelHooks() []BeforeModelCallHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]BeforeModelCallHook(nil), r.beforeModelCall...)
}

func (r *HookRegistry) afterModelHooks() []AfterModelResponseHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AfterModelResponseHook(nil), r.afterModelResponse...)
}

func (r *HookRegistry) afterToolHooks() []AfterToolCallHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AfterToolCallHook(nil), r.afterToolCall...)
}

func (r *HookRegistry) beforeCompactHooks() []BeforeCompactHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]BeforeCompactHook(nil), r.beforeCompact...)
}

func (r *HookRegistry) afterCompactHooks() []AfterCompactHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AfterCompactHook(nil), r.afterCompact...)
}

func (r *HookRegistry) beforeThreadHooks() []BeforeThreadPersistHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]BeforeThreadPersistHook(nil), r.beforeThreadPersist...)
}

func (r *HookRegistry) afterThreadHooks() []AfterThreadPersistHook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]AfterThreadPersistHook(nil), r.afterThreadPersist...)
}
