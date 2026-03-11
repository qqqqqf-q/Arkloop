package catalogapi

import (
	"archive/zip"
	"bytes"
	"testing"

	"arkloop/services/shared/skillstore"
)

func TestNormalizeGitHubImportRequestFromTreeURL(t *testing.T) {
	target, err := normalizeGitHubImportRequest("https://github.com/callowayproject/agent-skills/tree/main/skills/content-writer", "", "")
	if err != nil {
		t.Fatalf("normalizeGitHubImportRequest() error = %v", err)
	}
	if target.RepositoryURL != "https://github.com/callowayproject/agent-skills" {
		t.Fatalf("unexpected repository URL: %q", target.RepositoryURL)
	}
	if target.Ref != "main" {
		t.Fatalf("unexpected ref: %q", target.Ref)
	}
	if target.CandidatePath != "skills/content-writer" {
		t.Fatalf("unexpected candidate path: %q", target.CandidatePath)
	}
}

func TestNormalizeGitHubImportRequestHonorsExplicitOverrides(t *testing.T) {
	target, err := normalizeGitHubImportRequest("https://github.com/callowayproject/agent-skills/tree/main/skills/content-writer", "release", "skills/researcher")
	if err != nil {
		t.Fatalf("normalizeGitHubImportRequest() error = %v", err)
	}
	if target.RepositoryURL != "https://github.com/callowayproject/agent-skills" {
		t.Fatalf("unexpected repository URL: %q", target.RepositoryURL)
	}
	if target.Ref != "release" {
		t.Fatalf("unexpected ref: %q", target.Ref)
	}
	if target.CandidatePath != "skills/researcher" {
		t.Fatalf("unexpected candidate path: %q", target.CandidatePath)
	}
}

func TestNormalizeGitHubRepositoryURLStripsTreePath(t *testing.T) {
	if got := normalizeGitHubRepositoryURL("https://github.com/callowayproject/agent-skills/tree/main/skills/content-writer"); got != "https://github.com/callowayproject/agent-skills" {
		t.Fatalf("unexpected repository URL: %q", got)
	}
}

func TestBuildBundleFromEntriesAcceptsSkillMarkdownOnly(t *testing.T) {
	entries := map[string][]byte{
		"skills/research-planner/SKILL.md": []byte("# Research Planner\n\nPlan research tasks."),
	}
	bundleData, _, err := buildBundleFromEntries(entries, "skills/research-planner", "", "main")
	if err != nil {
		t.Fatalf("buildBundleFromEntries() error = %v", err)
	}
	bundle, err := skillstore.DecodeBundle(bundleData)
	if err != nil {
		t.Fatalf("DecodeBundle() error = %v", err)
	}
	if bundle.Definition.SkillKey != "research-planner" {
		t.Fatalf("unexpected skill key: %q", bundle.Definition.SkillKey)
	}
	if bundle.Definition.Version != "main" {
		t.Fatalf("unexpected version: %q", bundle.Definition.Version)
	}
	if bundle.Definition.InstructionPath != "SKILL.md" {
		t.Fatalf("unexpected instruction path: %q", bundle.Definition.InstructionPath)
	}
}

func TestNormalizeSkillsMarketPayloadSupportsPublicEndpoint(t *testing.T) {
	payload := map[string]any{
		"skills": []any{map[string]any{
			"id":          "callowayproject-agent-skills-skills-research-planner-skill-md",
			"name":        "research-planner",
			"description": "Plan research tasks.",
			"githubUrl":   "https://github.com/callowayproject/agent-skills/tree/main/skills/research-planner",
			"updatedAt":   "1772894858",
			"branch":      "main",
		}},
	}
	items := normalizeSkillsMarketPayload(payload)
	if len(items) != 1 {
		t.Fatalf("unexpected items len: %d", len(items))
	}
	if items[0].SkillKey != "research-planner" {
		t.Fatalf("unexpected skill key: %q", items[0].SkillKey)
	}
	if items[0].RepositoryURL != "https://github.com/callowayproject/agent-skills/tree/main/skills/research-planner" {
		t.Fatalf("unexpected repository url: %q", items[0].RepositoryURL)
	}
	if items[0].DetailURL != "https://clawhub.ai/skills/callowayproject-agent-skills-skills-research-planner-skill-md" {
		t.Fatalf("unexpected detail url: %q", items[0].DetailURL)
	}
}

func TestExtractClawHubDownloadBaseURL(t *testing.T) {
	raw := `const downloadURL = "https://wry-manatee-359.convex.site/api/v1/download?slug=${slug}&version=${version}"`
	if got := extractClawHubDownloadBaseURL(raw); got != "https://wry-manatee-359.convex.site" {
		t.Fatalf("unexpected download base url: %q", got)
	}
}

func TestUnzipEntriesAcceptsRootFiles(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entries := map[string]string{
		"SKILL.md":   "# Plan My Day\n",
		"_meta.json": `{"slug":"plan-my-day","version":"1.0.0"}`,
	}
	for name, content := range entries {
		item, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if _, err := item.Write([]byte(content)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	files, err := unzipEntries(buffer.Bytes())
	if err != nil {
		t.Fatalf("unzipEntries() error = %v", err)
	}
	if _, ok := files["SKILL.md"]; !ok {
		t.Fatalf("expected root SKILL.md, got %#v", files)
	}
	if _, ok := files["_meta.json"]; !ok {
		t.Fatalf("expected root _meta.json, got %#v", files)
	}
}

func TestBuildBundleFromEntriesUsesSkillMarkdownFrontMatterAndMeta(t *testing.T) {
	entries := map[string][]byte{
		"SKILL.md": []byte(`---
name: plan-my-day
description: Generate an energy-optimized, time-blocked daily plan
version: 1.0.0
---

# Plan My Day

Generate an energy-optimized, time-blocked daily plan.`),
		"_meta.json": []byte(`{"slug":"plan-my-day","version":"1.0.0"}`),
	}
	bundleData, _, err := buildBundleFromEntries(entries, "", "", "1")
	if err != nil {
		t.Fatalf("buildBundleFromEntries() error = %v", err)
	}
	bundle, err := skillstore.DecodeBundle(bundleData)
	if err != nil {
		t.Fatalf("DecodeBundle() error = %v", err)
	}
	if bundle.Definition.SkillKey != "plan-my-day" {
		t.Fatalf("unexpected skill key: %q", bundle.Definition.SkillKey)
	}
	if bundle.Definition.Version != "1.0.0" {
		t.Fatalf("unexpected version: %q", bundle.Definition.Version)
	}
	if bundle.Definition.DisplayName != "Plan My Day" {
		t.Fatalf("unexpected display name: %q", bundle.Definition.DisplayName)
	}
}
