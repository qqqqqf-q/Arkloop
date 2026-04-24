package catalogapi

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/objectstore"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/shared/skillstore"
	"arkloop/services/shared/workspaceblob"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type skillImportError struct {
	status int
	code   string
	msg    string
}

func (e skillImportError) Error() string { return e.msg }

func importSkillFromGitHub(
	ctx context.Context,
	store skillStore,
	packagesRepo *data.SkillPackagesRepository,
	accountID uuid.UUID,
	repositoryURL string,
	ref string,
	candidatePath string,
	expectedSkillKey string,
) (data.SkillPackage, []skillImportCandidate, error) {
	target, err := normalizeGitHubImportRequest(repositoryURL, ref, candidatePath)
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.import_invalid_repository", msg: err.Error()}
	}
	archiveURL, resolvedRef, err := resolveGitHubArchiveURL(ctx, target.RepositoryURL, target.Ref)
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.import_invalid_repository", msg: err.Error()}
	}
	entries, err := downloadGitHubArchive(ctx, archiveURL)
	if err != nil {
		return data.SkillPackage{}, nil, err
	}
	item, candidates, err := importSkillFromEntries(ctx, store, packagesRepo, accountID, entries, target.CandidatePath, expectedSkillKey, deriveImportVersion(resolvedRef, "1"))
	if err != nil {
		return data.SkillPackage{}, candidates, err
	}
	_ = packagesRepo.UpdateRegistryMetadata(ctx, accountID, item.SkillKey, item.Version, data.SkillPackageRegistryMetadata{
		RegistrySourceKind: "github",
		RegistrySourceURL:  target.RepositoryURL,
	})
	if fresh, getErr := packagesRepo.Get(ctx, accountID, item.SkillKey, item.Version); getErr == nil && fresh != nil {
		item = *fresh
	}
	return item, nil, nil
}

func importSkillFromUploadData(
	ctx context.Context,
	store skillStore,
	packagesRepo *data.SkillPackagesRepository,
	accountID uuid.UUID,
	_ string,
	payload []byte,
) (data.SkillPackage, []skillImportCandidate, error) {
	if bundle, err := skillstore.DecodeBundle(payload); err == nil {
		manifest, validateErr := skillstore.ValidateManifest(skillstore.PackageManifest{
			SkillKey:        bundle.Definition.SkillKey,
			Version:         bundle.Definition.Version,
			DisplayName:     bundle.Definition.DisplayName,
			Description:     bundle.Definition.Description,
			InstructionPath: bundle.Definition.InstructionPath,
		})
		if validateErr != nil {
			return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.invalid_manifest", msg: validateErr.Error()}
		}
		manifestBytes, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			return data.SkillPackage{}, nil, marshalErr
		}
		if err := store.PutObject(ctx, manifest.ManifestKey, manifestBytes, objectstore.PutOptions{ContentType: "application/json"}); err != nil {
			return data.SkillPackage{}, nil, err
		}
		if err := store.PutObject(ctx, manifest.BundleKey, payload, objectstore.PutOptions{ContentType: "application/zstd"}); err != nil {
			return data.SkillPackage{}, nil, err
		}
		item, err := packagesRepo.Create(ctx, accountID, manifest)
		if err != nil {
			var conflict data.SkillPackageConflictError
			if ok := errorAs(err, &conflict); ok {
				existing, getErr := packagesRepo.Get(ctx, accountID, manifest.SkillKey, manifest.Version)
				if getErr != nil {
					return data.SkillPackage{}, nil, getErr
				}
				if existing != nil {
					return *existing, nil, nil
				}
			}
			return data.SkillPackage{}, nil, err
		}
		return item, nil, nil
	}

	entries, err := unzipEntries(payload)
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.invalid_manifest", msg: err.Error()}
	}
	return importSkillFromEntries(ctx, store, packagesRepo, accountID, entries, "", "", "1")
}

func importSkillFromUploadEntries(
	ctx context.Context,
	store skillStore,
	packagesRepo *data.SkillPackagesRepository,
	accountID uuid.UUID,
	entries map[string][]byte,
) (data.SkillPackage, []skillImportCandidate, error) {
	return importSkillFromEntries(ctx, store, packagesRepo, accountID, entries, "", "", "1")
}

func importSkillFromEntries(
	ctx context.Context,
	store skillStore,
	packagesRepo *data.SkillPackagesRepository,
	accountID uuid.UUID,
	entries map[string][]byte,
	candidatePath string,
	expectedSkillKey string,
	defaultVersion string,
) (data.SkillPackage, []skillImportCandidate, error) {
	bundleData, candidates, err := buildBundleFromEntries(entries, candidatePath, expectedSkillKey, defaultVersion)
	if err != nil {
		return data.SkillPackage{}, candidates, err
	}
	bundle, err := skillstore.DecodeBundle(bundleData)
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.invalid_manifest", msg: err.Error()}
	}
	manifest, err := skillstore.ValidateManifest(skillstore.PackageManifest{
		SkillKey:        bundle.Definition.SkillKey,
		Version:         bundle.Definition.Version,
		DisplayName:     bundle.Definition.DisplayName,
		Description:     bundle.Definition.Description,
		InstructionPath: bundle.Definition.InstructionPath,
	})
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.invalid_manifest", msg: err.Error()}
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return data.SkillPackage{}, nil, err
	}
	if err := store.PutObject(ctx, manifest.ManifestKey, manifestBytes, objectstore.PutOptions{ContentType: "application/json"}); err != nil {
		return data.SkillPackage{}, nil, err
	}
	if err := store.PutObject(ctx, manifest.BundleKey, bundleData, objectstore.PutOptions{ContentType: "application/zstd"}); err != nil {
		return data.SkillPackage{}, nil, err
	}
	item, err := packagesRepo.Create(ctx, accountID, manifest)
	if err != nil {
		var conflict data.SkillPackageConflictError
		if ok := errorAs(err, &conflict); ok {
			existing, getErr := packagesRepo.Get(ctx, accountID, manifest.SkillKey, manifest.Version)
			if getErr != nil {
				return data.SkillPackage{}, nil, getErr
			}
			if existing != nil {
				return *existing, nil, nil
			}
		}
		return data.SkillPackage{}, nil, err
	}
	return item, nil, nil
}

type githubImportTarget struct {
	RepositoryURL string
	Ref           string
	CandidatePath string
}

func normalizeGitHubImportRequest(repositoryURL string, ref string, candidatePath string) (githubImportTarget, error) {
	normalizedRepositoryURL, inferredRef, inferredCandidatePath, err := parseGitHubImportURL(repositoryURL)
	if err != nil {
		return githubImportTarget{}, err
	}
	normalizedCandidatePath, err := normalizeRequestedCandidatePath(candidatePath)
	if err != nil {
		return githubImportTarget{}, err
	}
	if normalizedCandidatePath == "" {
		normalizedCandidatePath = inferredCandidatePath
	}
	resolvedRef := strings.TrimSpace(ref)
	if resolvedRef == "" {
		resolvedRef = inferredRef
	}
	return githubImportTarget{
		RepositoryURL: normalizedRepositoryURL,
		Ref:           resolvedRef,
		CandidatePath: normalizedCandidatePath,
	}, nil
}

func parseGitHubImportURL(raw string) (string, string, string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = strings.TrimRight(trimmed, "/")
	if trimmed == "" {
		return "", "", "", fmt.Errorf("invalid repository")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", "", fmt.Errorf("invalid repository")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("invalid repository")
	}
	repositoryURL := "https://github.com/" + path.Join(parts[0], parts[1])
	if len(parts) < 4 {
		return repositoryURL, "", "", nil
	}
	kind := strings.TrimSpace(parts[2])
	ref := strings.TrimSpace(parts[3])
	if ref == "" {
		return repositoryURL, "", "", nil
	}
	if kind == "tree" {
		candidatePath, err := normalizeRequestedCandidatePath(strings.Join(parts[4:], "/"))
		if err != nil {
			return "", "", "", err
		}
		return repositoryURL, ref, candidatePath, nil
	}
	if kind == "blob" {
		candidatePath, err := normalizeRequestedCandidatePath(path.Dir(strings.Join(parts[4:], "/")))
		if err != nil {
			return "", "", "", err
		}
		if candidatePath == "." {
			candidatePath = ""
		}
		return repositoryURL, ref, candidatePath, nil
	}
	return repositoryURL, "", "", nil
}

func normalizeRequestedCandidatePath(raw string) (string, error) {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "", nil
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "" {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid candidate path")
	}
	return cleaned, nil
}

func normalizeGitHubRepositoryURL(raw string) string {
	target, err := normalizeGitHubImportRequest(raw, "", "")
	if err != nil {
		return ""
	}
	return target.RepositoryURL
}

func resolveGitHubArchiveURL(ctx context.Context, repositoryURL string, ref string) (string, string, error) {
	repositoryURL = normalizeGitHubRepositoryURL(repositoryURL)
	if repositoryURL == "" {
		return "", "", fmt.Errorf("invalid repository")
	}
	parsed, err := url.Parse(repositoryURL)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository")
	}
	owner, repo := parts[0], parts[1]
	if strings.TrimSpace(ref) == "" {
		ref = "main"
		apiReq, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo), nil)
		if err == nil {
			apiReq.Header.Set("User-Agent", "Arkloop/skills-import")
			if resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(15 * time.Second).Do(apiReq); err == nil {
				defer func() { _ = resp.Body.Close() }()
				var payload struct {
					DefaultBranch string `json:"default_branch"`
				}
				if resp.StatusCode < 400 && json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload) == nil && strings.TrimSpace(payload.DefaultBranch) != "" {
					ref = strings.TrimSpace(payload.DefaultBranch)
				}
			}
		}
	}
	return fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", owner, repo, url.PathEscape(ref)), ref, nil
}

func downloadGitHubArchive(ctx context.Context, archiveURL string) (map[string][]byte, error) {
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(archiveURL); err != nil {
		return nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.import_invalid_repository", msg: err.Error()}
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Arkloop/skills-import")
	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, skillImportError{status: nethttp.StatusBadGateway, code: "skills.import_not_found", msg: "skill archive not found"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return unzipEntries(body)
}

func unzipEntries(body []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		cleaned := normalizeImportArchivePath(file.Name)
		if cleaned == "" {
			return nil, fmt.Errorf("archive path is invalid")
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(rc)
		if closeErr := rc.Close(); closeErr != nil {
			return nil, closeErr
		}
		if readErr != nil {
			return nil, readErr
		}
		entries[cleaned] = content
	}
	return stripCommonArchiveRoot(entries), nil
}

func normalizeImportArchivePath(raw string) string {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = path.Clean(cleaned)
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

func stripCommonArchiveRoot(entries map[string][]byte) map[string][]byte {
	if len(entries) == 0 {
		return entries
	}
	commonRoot := ""
	for name := range entries {
		parts := strings.Split(name, "/")
		if len(parts) <= 1 {
			return entries
		}
		if commonRoot == "" {
			commonRoot = parts[0]
			continue
		}
		if commonRoot != parts[0] {
			return entries
		}
	}
	trimmed := make(map[string][]byte, len(entries))
	for name, content := range entries {
		parts := strings.Split(name, "/")
		trimmed[strings.Join(parts[1:], "/")] = content
	}
	return trimmed
}

func buildBundleFromEntries(entries map[string][]byte, candidatePath, expectedSkillKey string, defaultVersion string) ([]byte, []skillImportCandidate, error) {
	candidates := detectSkillCandidates(entries, defaultVersion)
	selected, list, err := selectSkillCandidate(candidates, candidatePath, expectedSkillKey)
	if err != nil {
		return nil, list, err
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	dirs := make(map[string]struct{})
	filesToWrite := make(map[string][]byte, len(selected.Files)+2)
	for fullPath, content := range selected.Files {
		rel := strings.TrimPrefix(fullPath, selected.Path+"/")
		if rel == fullPath {
			rel = path.Base(fullPath)
		}
		filesToWrite[rel] = content
	}
	if selected.Synthetic {
		manifest := fmt.Sprintf("skill_key: %s\nversion: %q\ndisplay_name: %s\ndescription: %s\ninstruction_path: SKILL.md\n", selected.SkillKey, selected.Version, selected.DisplayName, marshalYAMLScalar(selected.Description))
		filesToWrite["skill.yaml"] = []byte(manifest)
		if instructionRel := strings.TrimPrefix(selected.InstructionPath, selected.Path+"/"); instructionRel != "SKILL.md" {
			if content, ok := selected.Files[selected.InstructionPath]; ok {
				filesToWrite["SKILL.md"] = content
			}
		}
	}
	paths := make([]string, 0, len(filesToWrite))
	for rel := range filesToWrite {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		for _, dir := range parentDirs(rel) {
			if _, ok := dirs[dir]; ok {
				continue
			}
			dirs[dir] = struct{}{}
			if err := writer.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				return nil, nil, err
			}
		}
		content := filesToWrite[rel]
		if err := writer.WriteHeader(&tar.Header{Name: rel, Mode: fileMode(rel), Size: int64(len(content))}); err != nil {
			return nil, nil, err
		}
		if _, err := writer.Write(content); err != nil {
			return nil, nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, nil, err
	}
	encoded, err := workspaceblob.Encode(buffer.Bytes())
	if err != nil {
		return nil, nil, err
	}
	return encoded, nil, nil
}

type skillCandidate struct {
	Path            string
	SkillKey        string
	Version         string
	DisplayName     string
	Description     string
	InstructionPath string
	Synthetic       bool
	Files           map[string][]byte
}

func detectSkillCandidates(entries map[string][]byte, defaultVersion string) map[string]skillCandidate {
	roots := make(map[string]map[string][]byte)
	for fullPath, content := range entries {
		dir := path.Dir(fullPath)
		if dir == "." {
			dir = ""
		}
		if roots[dir] == nil {
			roots[dir] = map[string][]byte{}
		}
		roots[dir][fullPath] = content
	}
	result := make(map[string]skillCandidate)
	for root, files := range roots {
		skillYAML, hasYAML := files[path.Join(root, "skill.yaml")]
		if hasYAML {
			var def skillstore.SkillDefinition
			if yaml.Unmarshal(skillYAML, &def) != nil {
				continue
			}
			result[root] = skillCandidate{
				Path:            root,
				SkillKey:        strings.TrimSpace(def.SkillKey),
				Version:         strings.TrimSpace(def.Version),
				DisplayName:     strings.TrimSpace(def.DisplayName),
				Description:     strings.TrimSpace(def.Description),
				InstructionPath: strings.TrimSpace(def.InstructionPath),
				Files:           files,
			}
			continue
		}
		instructionPath := detectSkillInstructionPath(root, files)
		if instructionPath == "" {
			continue
		}
		markdownMeta := parseSkillMarkdownMetadata(files[instructionPath])
		archiveMeta := parseArchiveSkillMetadata(files[path.Join(root, "_meta.json")])
		skillKey := firstNonEmpty(markdownMeta.Name, archiveMeta.Slug, deriveSkillKeyFromRoot(root))
		if skillKey == "" {
			continue
		}
		result[root] = skillCandidate{
			Path:            root,
			SkillKey:        skillKey,
			Version:         deriveImportVersion(firstNonEmpty(markdownMeta.Version, archiveMeta.Version, defaultVersion), "1"),
			DisplayName:     firstNonEmpty(markdownMeta.DisplayName, humanizeSkillKey(skillKey)),
			Description:     firstNonEmpty(markdownMeta.Description, extractSkillDescription(files[instructionPath])),
			InstructionPath: instructionPath,
			Synthetic:       true,
			Files:           files,
		}
	}
	return result
}

func detectSkillInstructionPath(root string, files map[string][]byte) string {
	for fullPath := range files {
		dir := path.Dir(fullPath)
		if dir == "." {
			dir = ""
		}
		if dir != root {
			continue
		}
		if strings.EqualFold(path.Base(fullPath), "SKILL.md") {
			return fullPath
		}
	}
	return ""
}

type skillMarkdownMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	DisplayName string
}

type archiveSkillMetadata struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
}

func parseSkillMarkdownMetadata(content []byte) skillMarkdownMetadata {
	result := skillMarkdownMetadata{}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[4:], "\n---\n"); end >= 0 {
			_ = yaml.Unmarshal([]byte(text[4:4+end]), &result)
			text = text[4+end+5:]
		}
	}
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "# ") {
			result.DisplayName = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	return result
}

func parseArchiveSkillMetadata(content []byte) archiveSkillMetadata {
	if len(content) == 0 {
		return archiveSkillMetadata{}
	}
	var result archiveSkillMetadata
	if err := json.Unmarshal(content, &result); err != nil {
		return archiveSkillMetadata{}
	}
	return result
}

func humanizeSkillKey(skillKey string) string {
	skillKey = strings.TrimSpace(skillKey)
	if skillKey == "" {
		return "Skill"
	}
	parts := strings.FieldsFunc(skillKey, func(ch rune) bool {
		return ch == '-' || ch == '_' || ch == '.' || ch == '/'
	})
	if len(parts) == 0 {
		return "Skill"
	}
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func deriveSkillKeyFromRoot(root string) string {
	base := strings.TrimSpace(path.Base(root))
	if base == "." || base == "" || base == "/" {
		base = "skill"
	}
	base = strings.ToLower(base)
	var builder strings.Builder
	lastDash := false
	for _, ch := range base {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			builder.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "skill"
	}
	return result
}

func deriveImportVersion(raw string, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "_", "-")
	var builder strings.Builder
	lastDash := false
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == ':' || ch == '-' {
			builder.WriteRune(ch)
			lastDash = ch == '-'
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-.")
	if result == "" {
		return "1"
	}
	if len(result) > 64 {
		return strings.Trim(result[:64], "-.")
	}
	return result
}

func extractSkillDescription(content []byte) string {
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") {
			continue
		}
		if len(line) > 280 {
			return strings.TrimSpace(line[:280])
		}
		return line
	}
	return ""
}

func selectSkillCandidate(candidates map[string]skillCandidate, candidatePath, expectedSkillKey string) (skillCandidate, []skillImportCandidate, error) {
	candidatePath = strings.TrimSpace(candidatePath)
	if candidatePath != "" {
		candidate, ok := candidates[candidatePath]
		if !ok {
			return skillCandidate{}, toSkillImportCandidates(candidates), skillImportError{status: nethttp.StatusBadRequest, code: "skills.import_not_found", msg: "skill candidate not found"}
		}
		return candidate, nil, nil
	}
	expectedSkillKey = strings.TrimSpace(expectedSkillKey)
	if expectedSkillKey != "" {
		for _, candidate := range candidates {
			if candidate.SkillKey == expectedSkillKey {
				return candidate, nil, nil
			}
		}
	}
	if len(candidates) == 1 {
		for _, candidate := range candidates {
			return candidate, nil, nil
		}
	}
	list := toSkillImportCandidates(candidates)
	if len(list) == 0 {
		return skillCandidate{}, nil, skillImportError{status: nethttp.StatusNotFound, code: "skills.import_not_found", msg: "skill package not found"}
	}
	return skillCandidate{}, list, skillImportError{status: nethttp.StatusConflict, code: "skills.import_ambiguous", msg: "multiple skill packages found"}
}

func toSkillImportCandidates(candidates map[string]skillCandidate) []skillImportCandidate {
	list := make([]skillImportCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		list = append(list, skillImportCandidate{
			Path:        candidate.Path,
			SkillKey:    candidate.SkillKey,
			Version:     candidate.Version,
			DisplayName: candidate.DisplayName,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list
}

func parentDirs(pathValue string) []string {
	dir := path.Dir(pathValue)
	if dir == "." || dir == "" {
		return nil
	}
	parts := strings.Split(dir, "/")
	out := make([]string, 0, len(parts))
	current := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		out = append(out, current)
	}
	return out
}

func fileMode(name string) int64 {
	if strings.HasSuffix(name, ".sh") {
		return 0o755
	}
	return 0o644
}

func marshalYAMLScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return `""`
	}
	encoded, err := yaml.Marshal(trimmed)
	if err != nil {
		return `""`
	}
	return strings.TrimSpace(string(encoded))
}
