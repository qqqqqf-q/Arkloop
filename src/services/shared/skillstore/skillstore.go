package skillstore

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"arkloop/services/shared/workspaceblob"
	"gopkg.in/yaml.v3"
)

const (
	InstructionPathDefault = "SKILL.md"
	MountRoot              = "/opt/arkloop/skills"
	IndexPath              = "/home/arkloop/.arkloop/enabled-skills.json"
	defaultBundleMaxBytes  = 32 << 20
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

type PackageManifest struct {
	SkillKey        string   `json:"skill_key"`
	Version         string   `json:"version"`
	DisplayName     string   `json:"display_name"`
	Description     string   `json:"description,omitempty"`
	InstructionPath string   `json:"instruction_path,omitempty"`
	ManifestKey     string   `json:"manifest_key,omitempty"`
	BundleKey       string   `json:"bundle_key,omitempty"`
	FilesPrefix     string   `json:"files_prefix,omitempty"`
	Platforms       []string `json:"platforms,omitempty"`
}

type SkillDefinition struct {
	SkillKey        string `yaml:"skill_key"`
	Version         string `yaml:"version"`
	DisplayName     string `yaml:"display_name"`
	Description     string `yaml:"description"`
	InstructionPath string `yaml:"instruction_path"`
}

type BundleFile struct {
	Path  string
	Mode  int64
	Data  []byte
	IsDir bool
}

type BundleImage struct {
	Definition SkillDefinition
	Files      []BundleFile
}

type ResolvedSkill struct {
	SkillKey        string `json:"skill_key"`
	Version         string `json:"version"`
	ManifestRef     string `json:"manifest_ref"`
	BundleRef       string `json:"bundle_ref"`
	MountPath       string `json:"mount_path"`
	InstructionPath string `json:"instruction_path,omitempty"`
	AutoInject      bool   `json:"auto_inject"`
	ContentHash     string `json:"content_hash,omitempty"`
}

type IndexEntry struct {
	SkillKey        string `json:"skill_key"`
	Version         string `json:"version"`
	MountPath       string `json:"mount_path"`
	InstructionPath string `json:"instruction_path"`
}

func DerivedManifestKey(skillKey, version string) string {
	return "skills/" + strings.TrimSpace(skillKey) + "/" + strings.TrimSpace(version) + "/manifest.json"
}

func DerivedBundleKey(skillKey, version string) string {
	return "skills/" + strings.TrimSpace(skillKey) + "/" + strings.TrimSpace(version) + "/bundle.tar.zst"
}

func DerivedFilesPrefix(skillKey, version string) string {
	return "skills/" + strings.TrimSpace(skillKey) + "/" + strings.TrimSpace(version) + "/files/"
}

func MountPath(skillKey, version string) string {
	return MountRoot + "/" + strings.TrimSpace(skillKey) + "@" + strings.TrimSpace(version)
}

func NormalizeManifest(manifest PackageManifest) PackageManifest {
	manifest.SkillKey = strings.TrimSpace(manifest.SkillKey)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.DisplayName = strings.TrimSpace(manifest.DisplayName)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.InstructionPath = normalizeRelativePath(manifest.InstructionPath)
	if manifest.InstructionPath == "" {
		manifest.InstructionPath = InstructionPathDefault
	}
	if strings.TrimSpace(manifest.ManifestKey) == "" {
		manifest.ManifestKey = DerivedManifestKey(manifest.SkillKey, manifest.Version)
	}
	if strings.TrimSpace(manifest.BundleKey) == "" {
		manifest.BundleKey = DerivedBundleKey(manifest.SkillKey, manifest.Version)
	}
	if strings.TrimSpace(manifest.FilesPrefix) == "" {
		manifest.FilesPrefix = DerivedFilesPrefix(manifest.SkillKey, manifest.Version)
	}
	manifest.Platforms = dedupeSorted(manifest.Platforms)
	return manifest
}

func ValidateManifest(manifest PackageManifest) (PackageManifest, error) {
	normalized := NormalizeManifest(manifest)
	if !keyPattern.MatchString(normalized.SkillKey) {
		return PackageManifest{}, fmt.Errorf("skill_key format is invalid")
	}
	if !keyPattern.MatchString(normalized.Version) {
		return PackageManifest{}, fmt.Errorf("version format is invalid")
	}
	if normalized.DisplayName == "" {
		return PackageManifest{}, fmt.Errorf("display_name must not be empty")
	}
	if normalized.ManifestKey != DerivedManifestKey(normalized.SkillKey, normalized.Version) {
		return PackageManifest{}, fmt.Errorf("manifest_key must match derived location")
	}
	if normalized.BundleKey != DerivedBundleKey(normalized.SkillKey, normalized.Version) {
		return PackageManifest{}, fmt.Errorf("bundle_key must match derived location")
	}
	if normalized.FilesPrefix != DerivedFilesPrefix(normalized.SkillKey, normalized.Version) {
		return PackageManifest{}, fmt.Errorf("files_prefix must match derived location")
	}
	if normalized.InstructionPath == "" {
		return PackageManifest{}, fmt.Errorf("instruction_path must not be empty")
	}
	for _, platform := range normalized.Platforms {
		switch platform {
		case "linux", "darwin", "windows":
		default:
			return PackageManifest{}, fmt.Errorf("platform %q is invalid", platform)
		}
	}
	return normalized, nil
}

func DecodeManifest(data []byte) (PackageManifest, error) {
	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageManifest{}, fmt.Errorf("decode manifest.json: %w", err)
	}
	normalized, err := ValidateManifest(manifest)
	if err != nil {
		return PackageManifest{}, err
	}
	return normalized, nil
}

func DecodeBundle(data []byte) (BundleImage, error) {
	return DecodeBundleWithLimit(data, defaultBundleMaxBytes)
}

func DecodeBundleWithLimit(data []byte, maxBytes int64) (BundleImage, error) {
	decoded, err := workspaceblob.Decode(data)
	if err != nil {
		return BundleImage{}, err
	}
	if maxBytes > 0 && int64(len(decoded)) > maxBytes {
		return BundleImage{}, fmt.Errorf("bundle exceeds size limit")
	}
	reader := tar.NewReader(bytes.NewReader(decoded))
	files := make([]BundleFile, 0)
	dirs := map[string]int64{}
	var skillYAML []byte
	hasSkillDoc := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return BundleImage{}, fmt.Errorf("read bundle tar: %w", err)
		}
		rel := normalizeRelativePath(header.Name)
		if rel == "" {
			return BundleImage{}, fmt.Errorf("bundle path is invalid")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			dirs[rel] = int64(header.FileInfo().Mode().Perm())
		case tar.TypeReg, 0:
			fileData, readErr := io.ReadAll(reader)
			if readErr != nil {
				return BundleImage{}, fmt.Errorf("read bundle file %s: %w", rel, readErr)
			}
			files = append(files, BundleFile{Path: rel, Mode: int64(header.FileInfo().Mode().Perm()), Data: fileData, IsDir: false})
			if rel == "skill.yaml" {
				skillYAML = append([]byte(nil), fileData...)
			}
			if rel == InstructionPathDefault {
				hasSkillDoc = true
			}
		default:
			return BundleImage{}, fmt.Errorf("bundle entry %s has unsupported type", rel)
		}
	}
	if len(skillYAML) == 0 {
		return BundleImage{}, fmt.Errorf("bundle missing skill.yaml")
	}
	if !hasSkillDoc {
		return BundleImage{}, fmt.Errorf("bundle missing SKILL.md")
	}
	var definition SkillDefinition
	if err := yaml.Unmarshal(skillYAML, &definition); err != nil {
		return BundleImage{}, fmt.Errorf("decode skill.yaml: %w", err)
	}
	definition.SkillKey = strings.TrimSpace(definition.SkillKey)
	definition.Version = strings.TrimSpace(definition.Version)
	definition.DisplayName = strings.TrimSpace(definition.DisplayName)
	definition.Description = strings.TrimSpace(definition.Description)
	definition.InstructionPath = normalizeRelativePath(definition.InstructionPath)
	if definition.InstructionPath == "" {
		definition.InstructionPath = InstructionPathDefault
	}
	if !keyPattern.MatchString(definition.SkillKey) {
		return BundleImage{}, fmt.Errorf("skill.yaml skill_key format is invalid")
	}
	if !keyPattern.MatchString(definition.Version) {
		return BundleImage{}, fmt.Errorf("skill.yaml version format is invalid")
	}
	if definition.DisplayName == "" {
		return BundleImage{}, fmt.Errorf("skill.yaml display_name must not be empty")
	}
	foundInstruction := definition.InstructionPath == InstructionPathDefault
	if !foundInstruction {
		for _, file := range files {
			if file.Path == definition.InstructionPath {
				foundInstruction = true
				break
			}
		}
	}
	if !foundInstruction {
		return BundleImage{}, fmt.Errorf("bundle missing instruction file %s", definition.InstructionPath)
	}
	for dirPath, mode := range dirs {
		files = append(files, BundleFile{Path: dirPath, Mode: mode, IsDir: true})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return BundleImage{Definition: definition, Files: files}, nil
}

func ValidateBundleAgainstManifest(manifest PackageManifest, bundle BundleImage) error {
	if manifest.SkillKey != bundle.Definition.SkillKey {
		return fmt.Errorf("bundle skill_key mismatch")
	}
	if manifest.Version != bundle.Definition.Version {
		return fmt.Errorf("bundle version mismatch")
	}
	if manifest.DisplayName != bundle.Definition.DisplayName {
		return fmt.Errorf("bundle display_name mismatch")
	}
	if manifest.InstructionPath != bundle.Definition.InstructionPath {
		return fmt.Errorf("bundle instruction_path mismatch")
	}
	return nil
}

func BuildIndex(skills []ResolvedSkill) ([]byte, error) {
	entries := make([]IndexEntry, 0, len(skills))
	for _, item := range skills {
		instructionPath := normalizeRelativePath(item.InstructionPath)
		if instructionPath == "" {
			instructionPath = InstructionPathDefault
		}
		entries = append(entries, IndexEntry{
			SkillKey:        strings.TrimSpace(item.SkillKey),
			Version:         strings.TrimSpace(item.Version),
			MountPath:       strings.TrimSpace(item.MountPath),
			InstructionPath: instructionPath,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].SkillKey + "@" + entries[i].Version
		right := entries[j].SkillKey + "@" + entries[j].Version
		return left < right
	})
	return json.Marshal(entries)
}

func normalizeRelativePath(value string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return ""
	}
	cleaned = strings.ReplaceAll(cleaned, "\\", "/")
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = path.Clean(cleaned)
	if cleaned == "." || cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func dedupeSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, ok := set[cleaned]; ok {
			continue
		}
		set[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	sort.Strings(out)
	return out
}
