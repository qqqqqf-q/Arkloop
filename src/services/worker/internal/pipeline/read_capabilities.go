package pipeline

import (
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

const (
	readToolName              = "read"
	readSourceKindFilePath    = "file_path"
	readSourceKindRemoteURL   = "remote_url"
	readSourceKindMessagePart = "message_attachment"
)

// ResolveReadCapabilities 计算当前 run 的 read 能力事实来源。
func ResolveReadCapabilities(
	selected *routing.SelectedProviderRoute,
	finalSpecs []llm.ToolSpec,
	activeByGroup map[string]string,
) ReadCapabilities {
	caps := ReadCapabilities{
		NativeImageInput:   supportsImageInput(selected),
		ImageBridgeEnabled: hasImageBridgeProvider(activeByGroup),
	}

	if spec, ok := findToolSpec(finalSpecs, readToolName); ok && readSpecSupportsImageSources(spec) {
		caps.ReadImageSourcesVisible = true
	}
	return caps
}

// ApplyReadImageSourceVisibility 按 bridge 能力和主模型图片支持裁剪 read 的图片 source 暴露面。
// exposeImageSources=true 时全部暴露。nativeImageInput=true 时只暴露 message_attachment（不暴露 remote_url）。
func ApplyReadImageSourceVisibility(specs []llm.ToolSpec, exposeImageSources bool, nativeImageInput bool) []llm.ToolSpec {
	if len(specs) == 0 || exposeImageSources {
		return specs
	}
	if nativeImageInput {
		// 主模型支持图片：只暴露 message_attachment（用于回看被压缩的图片），隐藏 remote_url
		out := make([]llm.ToolSpec, 0, len(specs))
		for _, spec := range specs {
			if spec.Name != readToolName {
				out = append(out, spec)
				continue
			}
			patched := spec
			patched.JSONSchema = stripReadRemoteURLOnly(spec.JSONSchema)
			out = append(out, patched)
		}
		return out
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Name != readToolName {
			out = append(out, spec)
			continue
		}
		patched := spec
		patched.JSONSchema = stripReadImageSources(spec.JSONSchema)
		out = append(out, patched)
	}
	return out
}

func supportsImageInput(selected *routing.SelectedProviderRoute) bool {
	if selected == nil {
		return false
	}
	caps := routing.SelectedRouteModelCapabilities(selected)
	return caps.SupportsInputModality("image")
}

func hasImageBridgeProvider(activeByGroup map[string]string) bool {
	if len(activeByGroup) == 0 {
		return false
	}
	_, ok := activeByGroup[readToolName]
	return ok
}

func findToolSpec(specs []llm.ToolSpec, name string) (llm.ToolSpec, bool) {
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == name {
			return spec, true
		}
	}
	return llm.ToolSpec{}, false
}

func stripReadImageSources(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return schema
	}
	cloned, ok := cloneJSONValue(schema).(map[string]any)
	if !ok {
		return schema
	}
	properties := nestedObject(cloned, "properties")
	if len(properties) == 0 {
		return cloned
	}
	// Remove image-only top-level args when image bridge is unavailable.
	delete(properties, "prompt")
	delete(properties, "max_bytes")
	delete(properties, "timeout_ms")

	source := nestedObject(properties, "source")
	if len(source) == 0 {
		return cloned
	}
	sourceProps := nestedObject(source, "properties")
	if len(sourceProps) > 0 {
		delete(sourceProps, "attachment_key")
		delete(sourceProps, "url")
	}

	kind := nestedObject(source, "properties", "kind")
	if len(kind) > 0 {
		kind["enum"] = filterSourceKindEnum(kind["enum"])
	}

	return cloned
}

// stripReadRemoteURLOnly 移除 remote_url source，保留 message_attachment。
// 主模型原生支持图片时使用，允许 read 回看被压缩的群聊图片。
func stripReadRemoteURLOnly(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return schema
	}
	cloned, ok := cloneJSONValue(schema).(map[string]any)
	if !ok {
		return schema
	}
	properties := nestedObject(cloned, "properties")
	if len(properties) == 0 {
		return cloned
	}

	source := nestedObject(properties, "source")
	if len(source) == 0 {
		return cloned
	}
	sourceProps := nestedObject(source, "properties")
	if len(sourceProps) > 0 {
		delete(sourceProps, "url")
	}

	kind := nestedObject(source, "properties", "kind")
	if len(kind) > 0 {
		kind["enum"] = filterRemoteURLFromEnum(kind["enum"])
	}

	return cloned
}

func filterRemoteURLFromEnum(raw any) []any {
	values, ok := raw.([]any)
	if !ok {
		if stringValues, ok := raw.([]string); ok {
			values = make([]any, 0, len(stringValues))
			for _, item := range stringValues {
				values = append(values, item)
			}
		}
	}
	if len(values) == 0 {
		return values
	}
	out := make([]any, 0, len(values))
	for _, item := range values {
		value, ok := item.(string)
		if !ok {
			continue
		}
		cleaned := strings.TrimSpace(value)
		if cleaned == readSourceKindRemoteURL {
			continue
		}
		out = append(out, cleaned)
	}
	return out
}

func readSpecSupportsImageSources(spec llm.ToolSpec) bool {
	if strings.TrimSpace(spec.Name) != readToolName || len(spec.JSONSchema) == 0 {
		return false
	}
	source := nestedObject(spec.JSONSchema, "properties", "source")
	if len(source) == 0 {
		return false
	}
	kind := nestedObject(source, "properties", "kind")
	return len(kind) > 0 && enumContainsImageSourceKinds(kind["enum"])
}

func filterSourceKindEnum(raw any) []any {
	values, ok := raw.([]any)
	if !ok {
		if stringValues, ok := raw.([]string); ok {
			values = make([]any, 0, len(stringValues))
			for _, item := range stringValues {
				values = append(values, item)
			}
		}
	}
	if len(values) == 0 {
		return []any{readSourceKindFilePath}
	}
	out := make([]any, 0, len(values))
	for _, item := range values {
		value, ok := item.(string)
		if !ok {
			continue
		}
		cleaned := strings.TrimSpace(value)
		if cleaned == "" || isImageSourceKind(cleaned) {
			continue
		}
		out = append(out, cleaned)
	}
	if len(out) == 0 {
		out = append(out, readSourceKindFilePath)
	}
	return out
}

func enumContainsImageSourceKinds(raw any) bool {
	switch values := raw.(type) {
	case []any:
		for _, item := range values {
			s, ok := item.(string)
			if ok && isImageSourceKind(strings.TrimSpace(s)) {
				return true
			}
		}
	case []string:
		for _, s := range values {
			if isImageSourceKind(strings.TrimSpace(s)) {
				return true
			}
		}
	}
	return false
}

func isImageSourceKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case readSourceKindRemoteURL, readSourceKindMessagePart:
		return true
	default:
		return false
	}
}

func nestedObject(root map[string]any, keys ...string) map[string]any {
	current := root
	for _, key := range keys {
		raw, ok := current[key]
		if !ok {
			return nil
		}
		next, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func cloneJSONValue(raw any) any {
	switch value := raw.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = cloneJSONValue(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []string:
		out := make([]string, len(value))
		copy(out, value)
		return out
	default:
		return value
	}
}
