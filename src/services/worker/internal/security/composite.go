package security

import (
	"fmt"
	"log/slog"
)

// CompositeScanner 组合 RegexScanner 和 SemanticScanner。
// Semantic 不可用时自动降级为纯 Regex。
type CompositeScanner struct {
	regex    *RegexScanner
	semantic *SemanticScanner
}

// CompositeScanResult 综合扫描结果。
type CompositeScanResult struct {
	RegexMatches    []ScanResult
	SemanticResult  *SemanticResult
	IsInjection     bool
	Source          string // "regex", "semantic", "both", "none"
}

// NewCompositeScanner 创建组合扫描器。
// regex 为 nil 时跳过正则扫描，semantic 为 nil 时降级。
func NewCompositeScanner(regex *RegexScanner, semantic *SemanticScanner) *CompositeScanner {
	return &CompositeScanner{
		regex:    regex,
		semantic: semantic,
	}
}

// Scan 执行综合扫描。
// 始终运行所有已启用的 Scanner，合并结果取并集。
func (c *CompositeScanner) Scan(text string) CompositeScanResult {
	result := CompositeScanResult{Source: "none"}

	if c.regex != nil {
		result.RegexMatches = c.regex.Scan(text)
	}

	if c.semantic != nil {
		sr, err := c.semantic.Classify(text)
		if err != nil {
			slog.Warn("semantic scan failed, falling back to regex-only", "error", err)
		} else {
			result.SemanticResult = &sr
		}
	}

	regexHit := len(result.RegexMatches) > 0
	semanticHit := result.SemanticResult != nil && result.SemanticResult.IsInjection

	switch {
	case regexHit && semanticHit:
		result.IsInjection = true
		result.Source = "both"
	case regexHit:
		result.IsInjection = true
		result.Source = "regex"
	case semanticHit:
		result.IsInjection = true
		result.Source = "semantic"
	}

	return result
}

// HasSemantic 返回语义扫描器是否可用。
func (c *CompositeScanner) HasSemantic() bool {
	return c != nil && c.semantic != nil
}

// HasRegex 返回正则扫描器是否可用。
func (c *CompositeScanner) HasRegex() bool {
	return c != nil && c.regex != nil
}

// Close 释放资源。
func (c *CompositeScanner) Close() {
	if c.semantic != nil {
		c.semantic.Close()
	}
}

// String 返回扫描器状态描述。
func (c *CompositeScanner) String() string {
	if c == nil {
		return "CompositeScanner(nil)"
	}
	return fmt.Sprintf("CompositeScanner(regex=%v, semantic=%v)", c.regex != nil, c.semantic != nil)
}
