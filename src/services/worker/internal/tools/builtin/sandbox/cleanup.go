package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// CleanupSession 在 run 结束后调用，删除对应的 sandbox session。
// 使用独立 context 避免阻塞主流程；失败仅 warn 不影响结果。
func CleanupSession(baseURL, authToken, sessionID, orgID string) {
	if baseURL == "" || sessionID == "" {
		return
	}

	for _, id := range cleanupSessionIDs(sessionID) {
		deleteSession(baseURL, authToken, id, orgID)
	}
}

func cleanupSessionIDs(runSessionID string) []string {
	ids := []string{runSessionID}
	shellID := defaultExecSessionID(runSessionID)
	if shellID != runSessionID {
		ids = append(ids, shellID)
	}
	return ids
}

func deleteSession(baseURL, authToken, sessionID, orgID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	endpoint := fmt.Sprintf("%s/v1/sessions/%s", baseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		slog.Warn("sandbox cleanup: build request failed", "session_id", sessionID, "error", err)
		return
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("sandbox cleanup: request failed", "session_id", sessionID, "error", err)
		return
	}
	resp.Body.Close()
}
