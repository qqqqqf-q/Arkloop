//go:build smoke

package smoke_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

const envAPIURL = "ARKLOOP_SMOKE_API_URL"

func TestComposeSmoke(t *testing.T) {
	baseURL := os.Getenv(envAPIURL)
	if baseURL == "" {
		t.Skipf("%s not set, skipping smoke test", envAPIURL)
	}

	client := &http.Client{Timeout: 15 * time.Second}

	t.Log("step 1: health check")
	status, body := doGet(t, client, baseURL+"/healthz", nil)
	if status != http.StatusOK {
		t.Fatalf("healthz: expected 200, got %d body=%s", status, truncate(body, 512))
	}
	var health map[string]string
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("healthz: unmarshal: %v body=%s", err, truncate(body, 512))
	}
	if health["status"] != "ok" {
		t.Fatalf("healthz: expected status=ok, got %q", health["status"])
	}

	t.Log("step 2: register user")
	login := "smoke_" + randHex(t, 4)
	status, body = doPost(t, client, baseURL+"/v1/auth/register", nil,
		map[string]any{"login": login, "password": "smoke_pwd_123456", "email": login + "@smoke-test.local"})
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d body=%s", status, truncate(body, 512))
	}
	var regResp struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &regResp); err != nil {
		t.Fatalf("register: unmarshal: %v body=%s", err, truncate(body, 512))
	}
	if regResp.AccessToken == "" {
		t.Fatalf("register: empty access_token")
	}
	token := regResp.AccessToken
	authHeaders := map[string]string{"Authorization": "Bearer " + token}

	t.Log("step 3: create thread")
	status, body = doPost(t, client, baseURL+"/v1/threads", authHeaders,
		map[string]any{"title": "smoke-test"})
	if status != http.StatusCreated {
		t.Fatalf("create thread: expected 201, got %d body=%s", status, truncate(body, 512))
	}
	var threadResp struct {
		ID        string `json:"id"`
		AccountID string `json:"account_id"`
	}
	if err := json.Unmarshal(body, &threadResp); err != nil {
		t.Fatalf("create thread: unmarshal: %v body=%s", err, truncate(body, 512))
	}
	if threadResp.ID == "" || threadResp.AccountID == "" {
		t.Fatalf("create thread: missing id or account_id: %s", truncate(body, 512))
	}

	t.Log("step 4: create run")
	status, body = doPost(t, client, baseURL+"/v1/threads/"+threadResp.ID+"/runs", authHeaders, nil)
	if status != http.StatusCreated {
		t.Fatalf("create run: expected 201, got %d body=%s", status, truncate(body, 512))
	}
	var runResp struct {
		RunID   string `json:"run_id"`
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(body, &runResp); err != nil {
		t.Fatalf("create run: unmarshal: %v body=%s", err, truncate(body, 512))
	}
	if runResp.RunID == "" || runResp.TraceID == "" {
		t.Fatalf("create run: missing run_id or trace_id: %s", truncate(body, 512))
	}

	t.Log("step 5: verify run via GET")
	status, body = doGet(t, client, baseURL+"/v1/runs/"+runResp.RunID, authHeaders)
	if status != http.StatusOK {
		t.Fatalf("get run: expected 200, got %d body=%s", status, truncate(body, 512))
	}
	var getRunResp struct {
		RunID     string `json:"run_id"`
		ThreadID  string `json:"thread_id"`
		AccountID string `json:"account_id"`
	}
	if err := json.Unmarshal(body, &getRunResp); err != nil {
		t.Fatalf("get run: unmarshal: %v body=%s", err, truncate(body, 512))
	}
	if getRunResp.RunID != runResp.RunID {
		t.Fatalf("get run: run_id mismatch: expected %q, got %q", runResp.RunID, getRunResp.RunID)
	}
	if getRunResp.ThreadID != threadResp.ID {
		t.Fatalf("get run: thread_id mismatch: expected %q, got %q", threadResp.ID, getRunResp.ThreadID)
	}
	if getRunResp.AccountID != threadResp.AccountID {
		t.Fatalf("get run: account_id mismatch: expected %q, got %q", threadResp.AccountID, getRunResp.AccountID)
	}

	t.Log("smoke test passed")
}

func doPost(t *testing.T, client *http.Client, url string, headers map[string]string, payload any) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var reqBody io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		t.Fatalf("new request POST %s: %v", url, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read response POST %s: %v", url, err)
	}
	return resp.StatusCode, body
}

func doGet(t *testing.T, client *http.Client, url string, headers map[string]string) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request GET %s: %v", url, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read response GET %s: %v", url, err)
	}
	return resp.StatusCode, body
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return hex.EncodeToString(buf)
}

func truncate(b []byte, maxLen int) string {
	s := string(b)
	if len(s) <= maxLen {
		return s
	}
	return fmt.Sprintf("%s...(truncated, total %d bytes)", s[:maxLen], len(s))
}
