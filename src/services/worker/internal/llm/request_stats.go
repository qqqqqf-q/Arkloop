package llm

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
)

// RequestStats 保存 LLM request 的上下文分解统计。
type RequestStats struct {
	SystemBytes          int
	ToolsBytes           int
	MessagesBytes        int
	AbstractRequestBytes int
	ImagePartCount       int
	Base64ImageBytes     int
	RoleBytes            map[string]int
	ToolSchemaBytesMap   map[string]int
	StablePrefixHash     string
}

// ComputeRequestStats 从 Request 计算上下文分解统计。
func ComputeRequestStats(req Request) RequestStats {
	stats := RequestStats{
		RoleBytes:          make(map[string]int),
		ToolSchemaBytesMap: make(map[string]int),
	}

	for _, tool := range req.Tools {
		b, _ := json.Marshal(tool.ToJSON())
		stats.ToolsBytes += len(b)
		stats.ToolSchemaBytesMap[tool.Name] = len(b)
	}

	for _, msg := range req.Messages {
		b, _ := json.Marshal(msg.ToJSON())
		msgBytes := len(b)
		stats.MessagesBytes += msgBytes
		stats.RoleBytes[msg.Role] += msgBytes
		if msg.Role == "system" {
			stats.SystemBytes += msgBytes
		}
		for _, part := range msg.Content {
			if part.Kind() != "image" {
				continue
			}
			stats.ImagePartCount++
			if _, data, err := modelInputImage(part); err == nil {
				stats.Base64ImageBytes += base64.StdEncoding.EncodedLen(len(data))
			} else if len(part.Data) > 0 {
				stats.Base64ImageBytes += base64.StdEncoding.EncodedLen(len(part.Data))
			}
		}
	}

	stats.AbstractRequestBytes = EstimateRequestJSONBytes(req)
	stats.StablePrefixHash = computePrefixHash(req)
	return stats
}

func EstimateRequestJSONBytes(req Request) int {
	raw, err := json.Marshal(req.ToJSON())
	if err != nil {
		return 0
	}
	return len(raw)
}

func computePrefixHash(req Request) string {
	var systemText string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			for _, part := range msg.Content {
				systemText += PartPromptText(part)
			}
		}
	}

	toolNames := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		toolNames = append(toolNames, t.Name)
	}
	sort.Strings(toolNames)

	raw := fmt.Sprintf("%s|%v", systemText, toolNames)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum[:8])
}
