//go:build !desktop

package http

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/shared/workspaceblob"
)

func TestProfileDefaultSkillsLifecycle(t *testing.T) {
	env := buildSkillsEnv(t)
	seedSkillPackageObjects(t, env.skillStore, "grep-helper", "1")
	promoteToPlatformAdmin(t, env.pool, env.aliceUserID)

	registerResp := doJSON(env.handler, nethttp.MethodPost, "/v1/admin/skill-packages", map[string]any{
		"skill_key": "grep-helper",
		"version":   "1",
	}, authHeader(env.aliceToken))
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register skill package: %d %s", registerResp.Code, registerResp.Body.String())
	}

	installResp := doJSON(env.handler, nethttp.MethodPost, "/v1/profiles/me/skills/install", map[string]any{
		"skill_key": "grep-helper",
		"version":   "1",
	}, authHeader(env.aliceToken))
	if installResp.Code != nethttp.StatusCreated {
		t.Fatalf("install skill package: %d %s", installResp.Code, installResp.Body.String())
	}

	listResp := doJSON(env.handler, nethttp.MethodGet, "/v1/profiles/me/default-skills", nil, authHeader(env.aliceToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list default skills: %d %s", listResp.Code, listResp.Body.String())
	}

	replaceResp := doJSON(env.handler, nethttp.MethodPut, "/v1/profiles/me/default-skills", map[string]any{
		"skills": []map[string]any{{"skill_key": "grep-helper", "version": "1"}},
	}, authHeader(env.aliceToken))
	if replaceResp.Code != nethttp.StatusOK {
		t.Fatalf("replace default skills: %d %s", replaceResp.Code, replaceResp.Body.String())
	}

	listResp = doJSON(env.handler, nethttp.MethodGet, "/v1/profiles/me/default-skills", nil, authHeader(env.aliceToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list default skills: %d %s", listResp.Code, listResp.Body.String())
	}
	body := decodeJSONBody[map[string]any](t, listResp.Body.Bytes())
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected default skills payload: %#v", body)
	}

	var defaultWorkspaceRef *string
	if err := env.pool.QueryRow(context.Background(), `SELECT default_workspace_ref FROM profile_registries WHERE owner_user_id = $1`, env.aliceUserID).Scan(&defaultWorkspaceRef); err != nil {
		t.Fatalf("load default workspace ref: %v", err)
	}
	if defaultWorkspaceRef == nil || *defaultWorkspaceRef == "" {
		t.Fatal("expected default workspace ref to be created")
	}
}

func TestMarketSkillsReflectInstallState(t *testing.T) {
	env := buildSkillsEnv(t)
	seedSkillPackageObjects(t, env.skillStore, "grep-helper", "1")
	promoteToPlatformAdmin(t, env.pool, env.aliceUserID)

	if _, err := env.pool.Exec(context.Background(), `INSERT INTO platform_settings (key, value, updated_at) VALUES ($1, $2, now()), ($3, $4, now())`, "skills.market.skillsmp_api_key", "secret", "skills.market.skillsmp_base_url", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("seed platform settings: %v", err)
	}

	registerResp := doJSON(env.handler, nethttp.MethodPost, "/v1/admin/skill-packages", map[string]any{
		"skill_key": "grep-helper",
		"version":   "1",
	}, authHeader(env.aliceToken))
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register skill package: %d %s", registerResp.Code, registerResp.Body.String())
	}
	installResp := doJSON(env.handler, nethttp.MethodPost, "/v1/profiles/me/skills/install", map[string]any{
		"skill_key": "grep-helper",
		"version":   "1",
	}, authHeader(env.aliceToken))
	if installResp.Code != nethttp.StatusCreated {
		t.Fatalf("install skill package: %d %s", installResp.Code, installResp.Body.String())
	}
	replaceResp := doJSON(env.handler, nethttp.MethodPut, "/v1/profiles/me/default-skills", map[string]any{
		"skills": []map[string]any{{"skill_key": "grep-helper", "version": "1"}},
	}, authHeader(env.aliceToken))
	if replaceResp.Code != nethttp.StatusOK {
		t.Fatalf("replace default skills: %d %s", replaceResp.Code, replaceResp.Body.String())
	}

	marketServer := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/api/v1/skills/search" {
			nethttp.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"skill_key":    "grep-helper",
				"version":      "1",
				"display_name": "Grep Helper",
				"description":  "Search files",
				"detail_url":   "https://skills.example/grep-helper",
			}},
		})
	}))
	defer marketServer.Close()
	if _, err := env.pool.Exec(context.Background(), `UPDATE platform_settings SET value = $2 WHERE key = $1`, "skills.market.skillsmp_base_url", marketServer.URL); err != nil {
		t.Fatalf("update platform settings: %v", err)
	}

	resp := doJSON(env.handler, nethttp.MethodGet, "/v1/market/skills?q=grep", nil, authHeader(env.aliceToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("market search: %d %s", resp.Code, resp.Body.String())
	}
	payload := decodeJSONBody[map[string]any](t, resp.Body.Bytes())
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected market payload: %#v", payload)
	}
	item := items[0].(map[string]any)
	if installed, _ := item["installed"].(bool); !installed {
		t.Fatalf("expected installed=true, got %#v", item)
	}
	if enabled, _ := item["enabled_by_default"].(bool); !enabled {
		t.Fatalf("expected enabled_by_default=true, got %#v", item)
	}
}

func TestUploadSkillImportAcceptsSkillBundle(t *testing.T) {
	env := buildSkillsEnv(t)
	archive := buildSkillArchive(t, "stock-analysis", "1.0.0", "Stock Analysis")
	resp := doMultipart(env.handler, "/v1/skill-packages/import/upload", "bundle.skill", archive, env.aliceToken)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("upload skill import: %d %s", resp.Code, resp.Body.String())
	}
	payload := decodeJSONBody[map[string]any](t, resp.Body.Bytes())
	if payload["skill_key"] != "stock-analysis" {
		t.Fatalf("unexpected upload response: %#v", payload)
	}
}

func buildSkillArchive(t *testing.T, skillKey, version, displayName string) []byte {
	t.Helper()
	var bundle bytes.Buffer
	tarWriter := tar.NewWriter(&bundle)
	writeTarFile(t, tarWriter, "skill.yaml", []byte("skill_key: "+skillKey+"\nversion: \""+version+"\"\ndisplay_name: "+displayName+"\ninstruction_path: SKILL.md\n"), 0o644)
	writeTarFile(t, tarWriter, "SKILL.md", []byte("Use "+skillKey), 0o644)
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	encoded, err := workspaceblob.Encode(bundle.Bytes())
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	return encoded
}

func doMultipart(handler nethttp.Handler, path string, filename string, payload []byte, accessToken string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		panic(err)
	}
	_, _ = part.Write(payload)
	_ = writer.Close()
	req := httptest.NewRequest(nethttp.MethodPost, path, &body)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestUploadSkillImportAcceptsZipArchive(t *testing.T) {
	env := buildSkillsEnv(t)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{
		"repo-main/skill.yaml": `skill_key: web-search
version: "1"
display_name: Web Search
instruction_path: SKILL.md
`,
		"repo-main/SKILL.md": "Use web search",
	}
	for name, content := range files {
		item, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := item.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	resp := doMultipart(env.handler, "/v1/skill-packages/import/upload", "web-search.zip", buffer.Bytes(), env.aliceToken)
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("upload zip import: %d %s", resp.Code, resp.Body.String())
	}
}

func TestMarketSkillsUsesRegistryConfigAndExposesScanFields(t *testing.T) {
	env := buildSkillsEnv(t)
	marketServer := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"slug":        "calendar",
					"displayName": "Calendar",
					"summary":     "Calendar management",
					"updatedAt":   1772065535894,
				}},
			})
		case r.URL.Path == "/api/v1/skills/calendar":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{
					"slug":        "calendar",
					"displayName": "Calendar",
					"summary":     "Calendar management",
					"stats":       map[string]any{"downloads": 42, "stars": 3, "versions": 1},
					"createdAt":   1771427803561,
					"updatedAt":   1772065535894,
				},
				"latestVersion": map[string]any{"version": "1.0.0", "createdAt": 1771427803561, "changelog": "Initial"},
				"owner":         map[string]any{"handle": "NDCCCCCC", "userId": "user_1"},
				"moderation":    map[string]any{"verdict": "clean", "summary": "reviewed"},
			})
		case r.URL.Path == "/api/v1/skills/calendar/versions/1.0.0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{"slug": "calendar", "displayName": "Calendar"},
				"version": map[string]any{
					"version":   "1.0.0",
					"createdAt": 1771427803561,
					"changelog": "Initial",
					"security": map[string]any{
						"status":      "suspicious",
						"hasWarnings": true,
						"checkedAt":   1771427824682,
						"model":       "gpt-5-mini",
					},
				},
			})
		default:
			nethttp.NotFound(w, r)
		}
	}))
	defer marketServer.Close()

	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO platform_settings (key, value, updated_at) VALUES
		($1, $2, now()), ($3, $4, now()), ($5, $6, now()), ($7, $8, now())`,
		"skills.market.skillsmp_base_url", "http://127.0.0.1:1",
		"skills.registry.provider", "clawhub",
		"skills.registry.base_url", marketServer.URL,
		"skills.registry.api_base_url", marketServer.URL,
	); err != nil {
		t.Fatalf("seed platform settings: %v", err)
	}

	resp := doJSON(env.handler, nethttp.MethodGet, "/v1/market/skills?q=calendar", nil, authHeader(env.aliceToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("market search: %d %s", resp.Code, resp.Body.String())
	}
	payload := decodeJSONBody[map[string]any](t, resp.Body.Bytes())
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected market payload: %#v", payload)
	}
	item := items[0].(map[string]any)
	if item["registry_provider"] != "clawhub" {
		t.Fatalf("unexpected registry provider: %#v", item)
	}
	if item["owner_handle"] != "NDCCCCCC" {
		t.Fatalf("unexpected owner_handle: %#v", item)
	}
	if item["scan_status"] != "suspicious" {
		t.Fatalf("unexpected scan_status: %#v", item)
	}
	if warned, _ := item["scan_has_warnings"].(bool); !warned {
		t.Fatalf("expected scan_has_warnings=true, got %#v", item)
	}
}

func TestMarketImportFromRegistryPersistsMetadata(t *testing.T) {
	env := buildSkillsEnv(t)
	zipPayload := buildRegistrySkillZip(t, "calendar", "1.0.0", "Calendar")
	marketServer := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/calendar":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{
					"slug":        "calendar",
					"displayName": "Calendar",
					"summary":     "Calendar management",
					"stats":       map[string]any{"downloads": 42, "stars": 3, "versions": 1},
					"createdAt":   1771427803561,
					"updatedAt":   1772065535894,
				},
				"latestVersion": map[string]any{"version": "1.0.0", "createdAt": 1771427803561, "changelog": "Initial"},
				"owner":         map[string]any{"handle": "NDCCCCCC", "userId": "user_1"},
				"moderation":    map[string]any{"verdict": "clean", "summary": "reviewed"},
			})
		case r.URL.Path == "/api/v1/skills/calendar/versions/1.0.0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{"slug": "calendar", "displayName": "Calendar"},
				"version": map[string]any{
					"version":   "1.0.0",
					"createdAt": 1771427803561,
					"changelog": "Initial",
					"security": map[string]any{
						"status":      "suspicious",
						"hasWarnings": true,
						"checkedAt":   1771427824682,
						"model":       "gpt-5-mini",
					},
				},
			})
		case r.URL.Path == "/api/v1/download":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipPayload)
		default:
			nethttp.NotFound(w, r)
		}
	}))
	defer marketServer.Close()

	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO platform_settings (key, value, updated_at) VALUES
		($1, $2, now()), ($3, $4, now()), ($5, $6, now())`,
		"skills.registry.provider", "clawhub",
		"skills.registry.base_url", marketServer.URL,
		"skills.registry.api_base_url", marketServer.URL,
	); err != nil {
		t.Fatalf("seed platform settings: %v", err)
	}

	resp := doJSON(env.handler, nethttp.MethodPost, "/v1/market/skills/import", map[string]any{
		"slug":    "calendar",
		"version": "1.0.0",
	}, authHeader(env.aliceToken))
	if resp.Code != nethttp.StatusCreated {
		t.Fatalf("market import: %d %s", resp.Code, resp.Body.String())
	}
	payload := decodeJSONBody[map[string]any](t, resp.Body.Bytes())
	if payload["registry_provider"] != "clawhub" {
		t.Fatalf("unexpected import payload: %#v", payload)
	}
	if payload["scan_status"] != "suspicious" {
		t.Fatalf("unexpected scan status: %#v", payload)
	}

	installResp := doJSON(env.handler, nethttp.MethodPost, "/v1/profiles/me/skills/install", map[string]any{
		"skill_key": "calendar",
		"version":   "1.0.0",
	}, authHeader(env.aliceToken))
	if installResp.Code != nethttp.StatusCreated {
		t.Fatalf("install imported skill: %d %s", installResp.Code, installResp.Body.String())
	}
	listResp := doJSON(env.handler, nethttp.MethodGet, "/v1/profiles/me/skills", nil, authHeader(env.aliceToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list installed skills: %d %s", listResp.Code, listResp.Body.String())
	}
	listPayload := decodeJSONBody[map[string]any](t, listResp.Body.Bytes())
	items, ok := listPayload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected installed payload: %#v", listPayload)
	}
	item := items[0].(map[string]any)
	if item["source"] != "official" {
		t.Fatalf("unexpected installed source: %#v", item)
	}
	if item["registry_slug"] != "calendar" {
		t.Fatalf("unexpected installed registry slug: %#v", item)
	}
	if item["scan_status"] != "suspicious" {
		t.Fatalf("unexpected installed scan status: %#v", item)
	}
}

func buildRegistrySkillZip(t *testing.T, skillKey, version, displayName string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{
		"registry-main/skill.yaml": fmt.Sprintf("skill_key: %s\nversion: \"%s\"\ndisplay_name: %s\ninstruction_path: SKILL.md\n", skillKey, version, displayName),
		"registry-main/SKILL.md":   "Use " + skillKey,
	}
	for name, content := range files {
		item, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := item.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}
