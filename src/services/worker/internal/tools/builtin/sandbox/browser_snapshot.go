package sandbox

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	browserCompactSnapshotCommand  = "snapshot -i -C -c --json"
	browserSnapshotMaxClickables   = 25
	browserSnapshotMaxFormControls = 15
	browserSnapshotMaxVisibleText  = 8
	browserSnapshotMaxTextLength   = 160
)

type browserClickable struct {
	Ref  string `json:"ref"`
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
}

type browserFormControl struct {
	Ref   string `json:"ref"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

type browserSnapshotPayload struct {
	URL          string
	Title        string
	Clickables   []browserClickable
	FormControls []browserFormControl
	VisibleText  []string
	Output       string
}

func preparedBrowserCommand(command string) string {
	if isSnapshotCommand(command) {
		return browserCompactSnapshotCommand
	}
	return strings.TrimSpace(command)
}

func isSnapshotCommand(command string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(command))
	return trimmed == "snapshot" || strings.HasPrefix(trimmed, "snapshot ")
}

func buildBrowserPublicResult(
	command string,
	resp *execSessionResponse,
	softLimits tools.PerToolSoftLimits,
	started time.Time,
) (map[string]any, *tools.ExecutionError) {
	if resp == nil {
		return nil, browserSnapshotParseResult(started).Error
	}
	if isSnapshotCommand(command) {
		return buildBrowserSnapshotResult(resp, softLimits, started)
	}
	resultJSON := map[string]any{
		"output":      strings.TrimSpace(resp.Output),
		"duration_ms": durationMs(started),
	}
	if resp.ExitCode != nil {
		resultJSON["exit_code"] = *resp.ExitCode
	}
	if len(resp.Artifacts) > 0 {
		resultJSON["artifacts"] = resp.Artifacts
	}
	return resultJSON, nil
}

func buildBrowserSnapshotResult(
	resp *execSessionResponse,
	softLimits tools.PerToolSoftLimits,
	started time.Time,
) (map[string]any, *tools.ExecutionError) {
	payload, ok := parseBrowserSnapshot(resp.Output)
	if !ok {
		return nil, browserSnapshotParseResult(started).Error
	}
	resultJSON := map[string]any{
		"url":           payload.URL,
		"title":         payload.Title,
		"clickables":    payload.Clickables,
		"form_controls": payload.FormControls,
		"visible_text":  payload.VisibleText,
		"output":        payload.Output,
		"duration_ms":   durationMs(started),
	}
	if resp.ExitCode != nil {
		resultJSON["exit_code"] = *resp.ExitCode
	}
	if len(resp.Artifacts) > 0 {
		resultJSON["artifacts"] = resp.Artifacts
	}
	return resultJSON, nil
}

func parseBrowserSnapshot(raw string) (browserSnapshotPayload, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return browserSnapshotPayload{}, false
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return browserSnapshotPayload{}, false
	}
	data := nestedMap(root, "data")
	snapshotText := firstNonEmpty(readString(data, "snapshot"), readString(root, "snapshot"))
	refs := nestedMap(data, "refs")
	if refs == nil {
		refs = nestedMap(root, "refs")
	}
	visibleText := extractSnapshotVisibleText(snapshotText)
	title := firstNonEmpty(
		readString(data, "title"),
		readString(data, "page_title"),
		readString(root, "title"),
		readString(root, "page_title"),
	)
	if title == "" && len(visibleText) > 0 {
		title = visibleText[0]
	}
	url := firstNonEmpty(
		readString(data, "url"),
		readString(data, "page_url"),
		readString(root, "url"),
		readString(root, "page_url"),
	)
	clickables := extractSnapshotClickables(refs, snapshotText)
	formControls := extractSnapshotFormControls(refs, snapshotText)
	if len(visibleText) > 0 && title != "" && visibleText[0] == title {
		visibleText = visibleText[1:]
	}
	output := renderCompactBrowserOutput(url, title, clickables, formControls, visibleText)
	if output == "" && url == "" && title == "" && len(clickables) == 0 && len(formControls) == 0 && len(visibleText) == 0 {
		return browserSnapshotPayload{}, false
	}
	return browserSnapshotPayload{
		URL:          url,
		Title:        title,
		Clickables:   clickables,
		FormControls: formControls,
		VisibleText:  visibleText,
		Output:       output,
	}, true
}

func extractSnapshotClickables(refs map[string]any, snapshotText string) []browserClickable {
	out := make([]browserClickable, 0, browserSnapshotMaxClickables)
	for _, ref := range sortedKeys(refs) {
		entry := nestedMap(refs, ref)
		if entry == nil {
			continue
		}
		role := strings.ToLower(firstNonEmpty(readString(entry, "role"), readString(entry, "tag"), readString(entry, "kind")))
		tag := strings.ToLower(readString(entry, "tag"))
		typ := strings.ToLower(readString(entry, "type"))
		if !isClickableRef(role, tag, typ) {
			continue
		}
		text := firstNonEmpty(
			readString(entry, "text"),
			readString(entry, "name"),
			readString(entry, "label"),
			readString(entry, "title"),
			readString(entry, "placeholder"),
		)
		out = append(out, browserClickable{Ref: ref, Role: role, Text: compactText(text)})
		if len(out) >= browserSnapshotMaxClickables {
			return out
		}
	}
	if len(out) > 0 {
		return out
	}
	return extractSnapshotClickablesFromText(snapshotText)
}

func extractSnapshotFormControls(refs map[string]any, snapshotText string) []browserFormControl {
	out := make([]browserFormControl, 0, browserSnapshotMaxFormControls)
	for _, ref := range sortedKeys(refs) {
		entry := nestedMap(refs, ref)
		if entry == nil {
			continue
		}
		role := strings.ToLower(firstNonEmpty(readString(entry, "role"), readString(entry, "type"), readString(entry, "tag"), readString(entry, "kind")))
		tag := strings.ToLower(readString(entry, "tag"))
		typ := strings.ToLower(readString(entry, "type"))
		if !isFormControlRef(role, tag, typ) {
			continue
		}
		label := firstNonEmpty(
			readString(entry, "label"),
			readString(entry, "name"),
			readString(entry, "text"),
			readString(entry, "placeholder"),
			readString(entry, "title"),
		)
		out = append(out, browserFormControl{Ref: ref, Type: firstNonEmpty(typ, role, tag), Label: compactText(label)})
		if len(out) >= browserSnapshotMaxFormControls {
			return out
		}
	}
	if len(out) > 0 {
		return out
	}
	return extractSnapshotFormControlsFromText(snapshotText)
}

func extractSnapshotVisibleText(snapshotText string) []string {
	if strings.TrimSpace(snapshotText) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, browserSnapshotMaxVisibleText)
	for _, line := range strings.Split(snapshotText, "\n") {
		candidate := extractSnapshotLineText(line)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
		if len(out) >= browserSnapshotMaxVisibleText {
			break
		}
	}
	return out
}

func extractSnapshotClickablesFromText(snapshotText string) []browserClickable {
	lines := strings.Split(snapshotText, "\n")
	out := make([]browserClickable, 0, browserSnapshotMaxClickables)
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "link") && !strings.Contains(lower, "button") {
			continue
		}
		ref := extractSnapshotRef(line)
		if ref == "" {
			continue
		}
		text := extractSnapshotLineText(line)
		role := "link"
		if strings.Contains(lower, "button") {
			role = "button"
		}
		out = append(out, browserClickable{Ref: ref, Role: role, Text: text})
		if len(out) >= browserSnapshotMaxClickables {
			break
		}
	}
	return out
}

func extractSnapshotFormControlsFromText(snapshotText string) []browserFormControl {
	lines := strings.Split(snapshotText, "\n")
	out := make([]browserFormControl, 0, browserSnapshotMaxFormControls)
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "textbox") && !strings.Contains(lower, "combobox") && !strings.Contains(lower, "checkbox") && !strings.Contains(lower, "radio") && !strings.Contains(lower, "searchbox") && !strings.Contains(lower, "textarea") {
			continue
		}
		ref := extractSnapshotRef(line)
		if ref == "" {
			continue
		}
		typ := "textbox"
		switch {
		case strings.Contains(lower, "combobox"):
			typ = "combobox"
		case strings.Contains(lower, "checkbox"):
			typ = "checkbox"
		case strings.Contains(lower, "radio"):
			typ = "radio"
		case strings.Contains(lower, "searchbox"):
			typ = "searchbox"
		case strings.Contains(lower, "textarea"):
			typ = "textarea"
		}
		out = append(out, browserFormControl{Ref: ref, Type: typ, Label: extractSnapshotLineText(line)})
		if len(out) >= browserSnapshotMaxFormControls {
			break
		}
	}
	return out
}

func renderCompactBrowserOutput(url, title string, clickables []browserClickable, formControls []browserFormControl, visibleText []string) string {
	parts := make([]string, 0, 5)
	if url != "" {
		parts = append(parts, "URL: "+url)
	}
	if title != "" {
		parts = append(parts, "Title: "+title)
	}
	if len(clickables) > 0 {
		items := make([]string, 0, len(clickables))
		for _, item := range clickables {
			line := item.Ref
			if item.Role != "" {
				line += " [" + item.Role + "]"
			}
			if item.Text != "" {
				line += " " + item.Text
			}
			items = append(items, line)
		}
		parts = append(parts, "Clickable:\n- "+strings.Join(items, "\n- "))
	}
	if len(formControls) > 0 {
		items := make([]string, 0, len(formControls))
		for _, item := range formControls {
			line := item.Ref
			if item.Type != "" {
				line += " [" + item.Type + "]"
			}
			if item.Label != "" {
				line += " " + item.Label
			}
			items = append(items, line)
		}
		parts = append(parts, "Form:\n- "+strings.Join(items, "\n- "))
	}
	if len(visibleText) > 0 {
		parts = append(parts, "Text:\n- "+strings.Join(visibleText, "\n- "))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractSnapshotLineText(line string) string {
	quoted := extractQuotedText(line)
	if len(quoted) > 0 {
		return compactText(strings.Join(quoted, " / "))
	}
	if idx := strings.Index(line, ":"); idx >= 0 {
		return compactText(line[idx+1:])
	}
	cleaned := stripBracketBlocks(line)
	cleaned = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cleaned), "-"))
	if cleaned == "" {
		return ""
	}
	for _, prefix := range []string{"document", "heading", "paragraph", "link", "button", "textbox", "combobox", "checkbox", "radio", "searchbox", "textarea"} {
		if strings.HasPrefix(strings.ToLower(cleaned), prefix+" ") {
			cleaned = strings.TrimSpace(cleaned[len(prefix):])
			break
		}
	}
	return compactText(cleaned)
}

func extractSnapshotRef(line string) string {
	if idx := strings.Index(line, "ref="); idx >= 0 {
		rest := line[idx+4:]
		end := strings.IndexAny(rest, "] )")
		if end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

func extractQuotedText(line string) []string {
	parts := []string{}
	for {
		start := strings.Index(line, "\"")
		if start < 0 {
			break
		}
		line = line[start+1:]
		end := strings.Index(line, "\"")
		if end < 0 {
			break
		}
		value := compactText(line[:end])
		if value != "" {
			parts = append(parts, value)
		}
		line = line[end+1:]
	}
	return parts
}

func stripBracketBlocks(value string) string {
	for {
		start := strings.Index(value, "[")
		if start < 0 {
			return value
		}
		end := strings.Index(value[start:], "]")
		if end < 0 {
			return value[:start]
		}
		value = value[:start] + value[start+end+1:]
	}
}

func compactText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > browserSnapshotMaxTextLength {
		return strings.TrimSpace(value[:browserSnapshotMaxTextLength])
	}
	return value
}

func sortedKeys(values map[string]any) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nestedMap(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	entry, _ := values[key].(map[string]any)
	return entry
}

func readString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	entry, _ := values[key].(string)
	return strings.TrimSpace(entry)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isClickableRef(role, tag, typ string) bool {
	switch role {
	case "link", "button", "menuitem", "tab", "option", "checkbox", "radio", "switch":
		return true
	}
	switch tag {
	case "a", "button":
		return true
	}
	switch typ {
	case "button", "submit", "checkbox", "radio":
		return true
	}
	return false
}

func isFormControlRef(role, tag, typ string) bool {
	switch role {
	case "textbox", "searchbox", "combobox", "listbox", "textarea", "spinbutton", "slider", "checkbox", "radio", "switch":
		return true
	}
	switch tag {
	case "input", "textarea", "select":
		return true
	}
	switch typ {
	case "text", "email", "password", "search", "url", "tel", "number", "date", "datetime-local", "time", "week", "month", "file", "checkbox", "radio":
		return true
	}
	return false
}
