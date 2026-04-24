package llm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func toGeminiPayload(request Request, advancedJSON map[string]any) (map[string]any, error) {
	systemInstruction, contents, err := toGeminiContents(request.Messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"contents": contents,
	}
	if systemInstruction != nil {
		payload["systemInstruction"] = systemInstruction
	}
	if len(request.Tools) > 0 {
		payload["tools"] = toGeminiTools(request.Tools)
		if tc := geminiToolConfig(request.ToolChoice); tc != nil {
			payload["toolConfig"] = tc
		}
	}

	// generationConfig 合并：advancedJSON 优先
	genConfig := map[string]any{}
	if existing, ok := advancedJSON["generationConfig"].(map[string]any); ok {
		for k, v := range existing {
			genConfig[k] = v
		}
	}
	if request.Temperature != nil {
		if _, has := genConfig["temperature"]; !has {
			genConfig["temperature"] = *request.Temperature
		}
	}
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens > 0 {
		if _, has := genConfig["maxOutputTokens"]; !has {
			genConfig["maxOutputTokens"] = *request.MaxOutputTokens
		}
	}

	normalizeGeminiThinkingConfig(genConfig, request.ReasoningMode)

	if len(genConfig) > 0 {
		payload["generationConfig"] = genConfig
	}

	// 合并 advancedJSON 中不在 denylist 且不是 generationConfig 的字段
	for k, v := range advancedJSON {
		if _, denied := geminiAdvancedJSONDenylist[k]; denied {
			continue
		}
		if k == "generationConfig" {
			continue // 已处理
		}
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}

	return payload, nil
}

// toGeminiContents 将消息列表转换为 Gemini contents 格式。
// system 消息提取为 systemInstruction，连续 tool 响应合并到同一 user content。
func toGeminiContents(messages []Message) (systemInstruction map[string]any, contents []map[string]any, err error) {
	contents = []map[string]any{}
	var pendingToolParts []map[string]any

	flushToolParts := func() {
		if len(pendingToolParts) == 0 {
			return
		}
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": pendingToolParts,
		})
		pendingToolParts = nil
	}

	for _, msg := range messages {
		text := joinParts(msg.Content)

		switch msg.Role {
		case "system":
			if strings.TrimSpace(text) != "" {
				if systemInstruction == nil {
					systemInstruction = map[string]any{
						"parts": []map[string]any{{"text": text}},
					}
				} else {
					parts := systemInstruction["parts"].([]map[string]any)
					systemInstruction["parts"] = append(parts, map[string]any{"text": text})
				}
			}
			continue

		case "tool":
			part, parseErr := geminiToolResponsePart(text)
			if parseErr != nil {
				err = parseErr
				return
			}
			pendingToolParts = append(pendingToolParts, part)
			for _, cp := range msg.Content {
				if cp.Kind() != "image" || cp.Attachment == nil || len(cp.Data) == 0 {
					continue
				}
				mimeType, data, imageErr := modelInputImage(cp)
				if imageErr != nil {
					err = imageErr
					return
				}
				pendingToolParts = append(pendingToolParts, map[string]any{
					"inlineData": map[string]any{
						"mimeType": mimeType,
						"data":     base64.StdEncoding.EncodeToString(data),
					},
				})
			}
			continue

		default:
			flushToolParts()
		}

		switch msg.Role {
		case "assistant":
			parts := []map[string]any{}
			if strings.TrimSpace(text) != "" {
				parts = append(parts, map[string]any{"text": text})
			}
			for _, call := range msg.ToolCalls {
				call = CanonicalToolCall(call)
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": call.ToolName,
						"args": mapOrEmpty(call.ArgumentsJSON),
					},
				})
			}
			if len(parts) == 0 {
				parts = []map[string]any{{"text": ""}}
			}
			contents = append(contents, map[string]any{
				"role":  "model",
				"parts": parts,
			})

		case "user":
			parts, buildErr := geminiUserParts(msg.Content)
			if buildErr != nil {
				err = buildErr
				return
			}
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": parts,
			})
		}
	}

	flushToolParts()
	return
}

func geminiUserParts(content []ContentPart) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, len(content))
	for _, p := range content {
		switch p.Kind() {
		case "text":
			parts = append(parts, map[string]any{"text": p.Text})
		case "file":
			t := PartPromptText(p)
			if strings.TrimSpace(t) != "" {
				parts = append(parts, map[string]any{"text": t})
			}
		case "image":
			mimeType, data, err := modelInputImage(p)
			if err != nil {
				return nil, fmt.Errorf("image part missing data")
			}
			parts = append(parts, map[string]any{
				"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     base64.StdEncoding.EncodeToString(data),
				},
			})
		}
	}
	if len(parts) == 0 {
		parts = []map[string]any{{"text": ""}}
	}
	return parts, nil
}

func geminiToolResponsePart(text string) (map[string]any, error) {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return nil, fmt.Errorf("tool message is not valid JSON")
	}
	toolName, _ := envelope["tool_name"].(string)
	toolName = CanonicalToolName(toolName)
	if toolName == "" {
		// 降级：从 tool_call_id 中能读到名字的情况
		toolName = "unknown"
	}

	var response any
	if errObj, ok := envelope["error"]; ok && errObj != nil {
		response = map[string]any{"error": errObj}
	} else {
		response = envelope["result"]
	}
	if response == nil {
		response = map[string]any{}
	}

	return map[string]any{
		"functionResponse": map[string]any{
			"name":     toolName,
			"response": response,
		},
	}, nil
}

func geminiToolConfig(tc *ToolChoice) map[string]any {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case "required":
		return map[string]any{
			"functionCallingConfig": map[string]any{"mode": "ANY"},
		}
	case "specific":
		return map[string]any{
			"functionCallingConfig": map[string]any{
				"mode":                 "ANY",
				"allowedFunctionNames": []string{CanonicalToolName(tc.ToolName)},
			},
		}
	default:
		return nil
	}
}

func toGeminiTools(specs []ToolSpec) []map[string]any {
	decls := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		name := CanonicalToolName(spec.Name)
		if name == "" {
			name = spec.Name
		}
		decl := map[string]any{
			"name":                 name,
			"parametersJsonSchema": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			decl["description"] = *spec.Description
		}
		decls = append(decls, decl)
	}
	return []map[string]any{
		{"functionDeclarations": decls},
	}
}
