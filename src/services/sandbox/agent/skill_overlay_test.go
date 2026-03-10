package main

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/skillstore"
	"arkloop/services/shared/workspaceblob"
)

func TestApplySkillOverlayWritesIndexAndSkillFiles(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	t.Cleanup(func() {
		_ = filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				_ = os.Chmod(path, 0o755)
				return nil
			}
			_ = os.Chmod(path, 0o644)
			return nil
		})
	})
	encodedBundle := buildTestSkillBundle(t, "grep-helper", "1")
	req := SkillOverlayRequest{
		IndexJSON: "[]",
		Skills: []SkillOverlayItem{{
			SkillKey:         "grep-helper",
			Version:          "1",
			MountPath:        skillstore.MountPath("grep-helper", "1"),
			InstructionPath:  "SKILL.md",
			BundleDataBase64: base64.StdEncoding.EncodeToString(encodedBundle),
		}},
	}
	if err := applySkillOverlay(req); err != nil {
		t.Fatalf("apply skill overlay: %v", err)
	}
	instructionPath := filepath.Join(shellSkillsDir, "grep-helper@1", "SKILL.md")
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatalf("read skill instruction: %v", err)
	}
	if string(content) != "Use grep helper." {
		t.Fatalf("unexpected skill instruction: %q", string(content))
	}
	indexContent, err := os.ReadFile(filepath.Join(shellHomeDir, ".arkloop", "enabled-skills.json"))
	if err != nil {
		t.Fatalf("read skill index: %v", err)
	}
	if string(indexContent) != "[]" {
		t.Fatalf("unexpected skill index: %q", string(indexContent))
	}
}

func TestApplySkillOverlayCanReapplyReadonlySkillBundle(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	t.Cleanup(func() {
		_ = filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				_ = os.Chmod(path, 0o755)
				return nil
			}
			_ = os.Chmod(path, 0o644)
			return nil
		})
	})
	encodedBundle := buildTestSkillBundle(t, "deep-research", "1.0.0")
	req := SkillOverlayRequest{
		IndexJSON: `[{"skill_key":"deep-research","version":"1.0.0","mount_path":"/opt/arkloop/skills/deep-research@1.0.0","instruction_path":"SKILL.md"}]`,
		Skills: []SkillOverlayItem{{
			SkillKey:         "deep-research",
			Version:          "1.0.0",
			MountPath:        skillstore.MountPath("deep-research", "1.0.0"),
			InstructionPath:  "SKILL.md",
			BundleDataBase64: base64.StdEncoding.EncodeToString(encodedBundle),
		}},
	}
	if err := applySkillOverlay(req); err != nil {
		t.Fatalf("first apply skill overlay: %v", err)
	}
	if err := applySkillOverlay(req); err != nil {
		t.Fatalf("second apply skill overlay: %v", err)
	}
}

func buildTestSkillBundle(t *testing.T, skillKey, version string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	mustWriteTarFile(t, writer, "skill.yaml", []byte("skill_key: "+skillKey+"\nversion: \""+version+"\"\ndisplay_name: Grep Helper\ninstruction_path: SKILL.md\n"), 0o644)
	mustWriteTarFile(t, writer, "SKILL.md", []byte("Use grep helper."), 0o644)
	mustWriteTarFile(t, writer, "scripts/run.sh", []byte("#!/bin/sh\necho ok\n"), 0o755)
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	encoded, err := workspaceblob.Encode(buffer.Bytes())
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	return encoded
}

func mustWriteTarFile(t *testing.T, writer *tar.Writer, name string, data []byte, mode int64) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
}
