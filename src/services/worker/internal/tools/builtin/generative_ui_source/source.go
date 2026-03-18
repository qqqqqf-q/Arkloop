package generativeuisource

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed assets/sections/*
var assetsFS embed.FS

const (
	sectionDir = "assets/sections"
	sourceName = "pi-generative-ui"
)

var sectionFiles = map[string]string{
	"_preamble":          "preamble.md",
	"Modules":            "modules.md",
	"Core Design System": "core_design_system.md",
	"When nothing fits":  "when_nothing_fits.md",
	"SVG setup":          "svg_setup.md",
	"Art and illustration": "art_and_illustration.md",
	"UI components":        "ui_components.md",
	"Color palette":        "color_palette.md",
	"Charts (Chart.js)":    "charts_chart_js.md",
	"Diagram types":        "diagram_types.md",
}

type Document struct {
	Source  string
	Modules []string
	Content string
}

type sourceData struct {
	moduleOrder []string
	moduleMap   map[string][]string
	sections    map[string]string
}

var (
	loadOnce   sync.Once
	loadErr    error
	cachedData sourceData
)

func SourceName() string { return sourceName }

func AvailableModules() []string {
	data, err := loadSource()
	if err != nil {
		return nil
	}
	out := make([]string, len(data.moduleOrder))
	copy(out, data.moduleOrder)
	return out
}

func BuildDocument(modules []string) (Document, error) {
	data, err := loadSource()
	if err != nil {
		return Document{}, err
	}

	normalized := normalizeModules(modules, data.moduleOrder)
	var builder strings.Builder
	seen := make(map[string]struct{}, 8)
	for _, module := range normalized {
		sectionTitles, ok := data.moduleMap[module]
		if !ok {
			continue
		}
		for _, title := range sectionTitles {
			if _, done := seen[title]; done {
				continue
			}
			content, ok := data.sections[title]
			if !ok {
				return Document{}, fmt.Errorf("missing guideline section: %s", title)
			}
			if builder.Len() > 0 {
				builder.WriteString("\n\n\n")
			}
			builder.WriteString(content)
			seen[title] = struct{}{}
		}
	}
	builder.WriteString("\n")

	return Document{
		Source:  sourceName,
		Modules: normalized,
		Content: builder.String(),
	}, nil
}

func loadSource() (sourceData, error) {
	loadOnce.Do(func() {
		moduleMap, err := loadMapping()
		if err != nil {
			loadErr = err
			return
		}
		sections, err := loadSections()
		if err != nil {
			loadErr = err
			return
		}
		moduleOrder := make([]string, 0, len(moduleMap))
		for name := range moduleMap {
			moduleOrder = append(moduleOrder, name)
		}
		sort.Strings(moduleOrder)
		cachedData = sourceData{
			moduleOrder: moduleOrder,
			moduleMap:   moduleMap,
			sections:    sections,
		}
	})
	return cachedData, loadErr
}

func loadMapping() (map[string][]string, error) {
	raw, err := assetsFS.ReadFile(path.Join(sectionDir, "mapping.json"))
	if err != nil {
		return nil, err
	}
	parsed := map[string][]string{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func loadSections() (map[string]string, error) {
	out := make(map[string]string, len(sectionFiles))
	for title, filename := range sectionFiles {
		raw, err := assetsFS.ReadFile(path.Join(sectionDir, filename))
		if err != nil {
			return nil, err
		}
		out[title] = strings.TrimSpace(string(raw))
	}
	return out, nil
}

func normalizeModules(modules []string, ordered []string) []string {
	known := make(map[string]struct{}, len(ordered))
	for _, name := range ordered {
		known[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(modules))
	out := make([]string, 0, len(modules))
	for _, item := range modules {
		name := strings.TrimSpace(strings.ToLower(item))
		if name == "" {
			continue
		}
		if _, ok := known[name]; !ok {
			continue
		}
		if _, done := seen[name]; done {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
