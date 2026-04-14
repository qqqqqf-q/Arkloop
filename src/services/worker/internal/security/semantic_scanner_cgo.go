//go:build cgo

package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// SemanticScanner 使用 ONNX Runtime 执行 Prompt Guard 模型推理。
// session.Run() 不是线程安全的，用 sync.Mutex 保护整个推理过程。
type SemanticScanner struct {
	mu        sync.Mutex
	session   *ort.DynamicAdvancedSession
	tokenizer *Tokenizer
	threshold float32
	labels    []string
}

var ortInitOnce sync.Once

func NewSemanticScanner(cfg SemanticScannerConfig) (*SemanticScanner, error) {
	modelPath := filepath.Join(cfg.ModelDir, "model.onnx")
	tokenizerPath := filepath.Join(cfg.ModelDir, "tokenizer.json")

	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model not found: %w", err)
	}
	if _, err := os.Stat(tokenizerPath); err != nil {
		return nil, fmt.Errorf("tokenizer not found: %w", err)
	}

	if cfg.OrtLibPath != "" {
		ort.SetSharedLibraryPath(cfg.OrtLibPath)
	}

	var initErr error
	ortInitOnce.Do(func() {
		initErr = ort.InitializeEnvironment()
	})
	if initErr != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", initErr)
	}

	maxLen := cfg.MaxSeqLen
	if maxLen <= 0 {
		maxLen = 512
	}

	tokenizer, err := LoadTokenizer(tokenizerPath, maxLen)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	inputNames := []string{"input_ids", "attention_mask"}
	outputNames := []string{"logits"}

	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("create session options: %w", err)
	}
	defer func() { _ = options.Destroy() }()

	// Bias towards lower peak memory on small Docker VMs.
	if err := options.SetExecutionMode(ort.ExecutionModeSequential); err != nil {
		return nil, fmt.Errorf("set execution mode: %w", err)
	}
	if err := options.SetGraphOptimizationLevel(ort.GraphOptimizationLevelDisableAll); err != nil {
		return nil, fmt.Errorf("set graph optimization level: %w", err)
	}
	if err := options.SetIntraOpNumThreads(1); err != nil {
		return nil, fmt.Errorf("set intra op threads: %w", err)
	}
	if err := options.SetInterOpNumThreads(1); err != nil {
		return nil, fmt.Errorf("set inter op threads: %w", err)
	}
	if err := options.SetCpuMemArena(false); err != nil {
		return nil, fmt.Errorf("disable cpu mem arena: %w", err)
	}
	if err := options.SetMemPattern(false); err != nil {
		return nil, fmt.Errorf("disable mem pattern: %w", err)
	}

	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		inputNames,
		outputNames,
		options,
	)
	if err != nil {
		return nil, fmt.Errorf("create onnx session: %w", err)
	}

	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = 0.5
	}

	return &SemanticScanner{
		session:   session,
		tokenizer: tokenizer,
		threshold: threshold,
		labels:    []string{"BENIGN", "INJECTION", "JAILBREAK"},
	}, nil
}

func (s *SemanticScanner) Classify(_ context.Context, text string) (SemanticResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return SemanticResult{}, fmt.Errorf("scanner not initialized")
	}

	inputIDs, attentionMask := s.tokenizer.Encode(text)

	seqLen := int64(len(inputIDs))
	shape := ort.Shape{1, seqLen}

	inputIDTensor, err := ort.NewTensor(shape, inputIDs)
	if err != nil {
		return SemanticResult{}, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer func() { _ = inputIDTensor.Destroy() }()

	maskTensor, err := ort.NewTensor(shape, attentionMask)
	if err != nil {
		return SemanticResult{}, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer func() { _ = maskTensor.Destroy() }()

	inputs := []ort.Value{inputIDTensor, maskTensor}
	outputs := []ort.Value{nil}

	if err := s.session.Run(inputs, outputs); err != nil {
		return SemanticResult{}, fmt.Errorf("onnx run: %w", err)
	}
	defer func() {
		for _, t := range outputs {
			if t != nil {
				_ = t.Destroy()
			}
		}
	}()

	logitsTensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return SemanticResult{}, fmt.Errorf("unexpected output tensor type")
	}

	logits := logitsTensor.GetData()
	probs := softmax(logits)

	bestIdx := 0
	bestScore := probs[0]
	for i := 1; i < len(probs); i++ {
		if probs[i] > bestScore {
			bestScore = probs[i]
			bestIdx = i
		}
	}

	label := "UNKNOWN"
	if bestIdx < len(s.labels) {
		label = s.labels[bestIdx]
	}

	return SemanticResult{
		Label:       label,
		Score:       bestScore,
		IsInjection: label != "BENIGN" && bestScore >= s.threshold,
	}, nil
}

func (s *SemanticScanner) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		_ = s.session.Destroy()
		s.session = nil
	}
}
