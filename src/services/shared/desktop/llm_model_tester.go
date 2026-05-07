package desktop

import (
	"context"
	"sync"
)

type LLMProviderModelTestRequest struct {
	ProviderID string
	ModelID    string
}

type LLMProviderModelTester interface {
	TestLLMProviderModel(ctx context.Context, req LLMProviderModelTestRequest) error
}

var (
	llmModelTesterMu sync.Mutex
	llmModelTester   LLMProviderModelTester
)

func SetLLMProviderModelTester(tester LLMProviderModelTester) {
	llmModelTesterMu.Lock()
	llmModelTester = tester
	llmModelTesterMu.Unlock()
}

func GetLLMProviderModelTester() LLMProviderModelTester {
	llmModelTesterMu.Lock()
	defer llmModelTesterMu.Unlock()
	return llmModelTester
}
