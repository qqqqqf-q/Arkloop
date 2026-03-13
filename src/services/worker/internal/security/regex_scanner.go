package security

import (
	"fmt"
	"regexp"
	"sync"
)

// PatternDef 外部传入的模式定义
type PatternDef struct {
	ID       string   `yaml:"id"`
	Category string   `yaml:"category"`
	Severity string   `yaml:"severity"`
	Patterns []string `yaml:"patterns"`
}

// ScanResult 表示一次模式匹配的结果
type ScanResult struct {
	Matched     bool
	PatternID   string
	Category    string
	Severity    string // "critical", "high", "medium", "low"
	MatchedText string
}

// compiledPattern 是编译后的正则模式
type compiledPattern struct {
	id       string
	category string
	severity string
	re       *regexp.Regexp
}

// RegexScanner 基于正则的注入检测扫描器
type RegexScanner struct {
	mu       sync.RWMutex
	patterns []compiledPattern
}

// NewRegexScanner 从模式定义创建扫描器，编译所有正则
func NewRegexScanner(defs []PatternDef) (*RegexScanner, error) {
	compiled, err := compilePatterns(defs)
	if err != nil {
		return nil, err
	}
	return &RegexScanner{patterns: compiled}, nil
}

// Scan 扫描文本，返回所有匹配结果（并发安全）
func (s *RegexScanner) Scan(text string) []ScanResult {
	s.mu.RLock()
	patterns := s.patterns
	s.mu.RUnlock()

	var results []ScanResult
	for _, p := range patterns {
		if m := p.re.FindString(text); m != "" {
			results = append(results, ScanResult{
				Matched:     true,
				PatternID:   p.id,
				Category:    p.category,
				Severity:    p.severity,
				MatchedText: m,
			})
		}
	}
	return results
}

// Reload 热更新模式库，替换已编译的正则集合
func (s *RegexScanner) Reload(defs []PatternDef) error {
	compiled, err := compilePatterns(defs)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.patterns = compiled
	s.mu.Unlock()
	return nil
}

// compilePatterns 将模式定义编译为正则集合
func compilePatterns(defs []PatternDef) ([]compiledPattern, error) {
	var compiled []compiledPattern
	for _, def := range defs {
		for i, raw := range def.Patterns {
			re, err := regexp.Compile(raw)
			if err != nil {
				return nil, fmt.Errorf("pattern %s[%d]: %w", def.ID, i, err)
			}
			compiled = append(compiled, compiledPattern{
				id:       def.ID,
				category: def.Category,
				severity: def.Severity,
				re:       re,
			})
		}
	}
	return compiled, nil
}
