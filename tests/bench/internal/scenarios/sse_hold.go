package scenarios

import (
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/tests/bench/internal/httpx"
	"arkloop/tests/bench/internal/report"
	"arkloop/tests/bench/internal/stats"
)

type SSEHoldConfig struct {
	APIBaseURL     string
	Token          string
	Hold           time.Duration
	ConnectTimeout time.Duration
	Concurrency    int
	Threshold      SSEHoldThresholds
}

type SSEHoldThresholds struct {
	MinRetention float64
}

func DefaultSSEHoldConfig(apiBaseURL, token string) SSEHoldConfig {
	return SSEHoldConfig{
		APIBaseURL:     apiBaseURL,
		Token:          token,
		Hold:           60 * time.Second,
		ConnectTimeout: 5 * time.Second,
		Concurrency:    500,
		Threshold: SSEHoldThresholds{
			MinRetention: 0.99,
		},
	}
}

func RunSSEHold(ctx context.Context, cfg SSEHoldConfig) report.ScenarioResult {
	result := report.ScenarioResult{
		Name:       "sse_hold",
		Config:     map[string]any{},
		Stats:      map[string]any{},
		Thresholds: map[string]any{},
		Pass:       false,
	}

	result.Config["hold_s"] = cfg.Hold.Seconds()
	result.Config["connect_timeout_s"] = cfg.ConnectTimeout.Seconds()
	result.Config["concurrency"] = cfg.Concurrency
	result.Thresholds["min_retention"] = cfg.Threshold.MinRetention

	if cfg.Token == "" {
		result.Errors = append(result.Errors, "auth.missing_token")
		return result
	}
	if cfg.Concurrency <= 0 {
		result.Errors = append(result.Errors, "config.invalid")
		return result
	}

	client := httpx.NewClient(2 * time.Second)
	sseClient := httpx.NewNoTimeoutClientWithHeaderTimeout(cfg.ConnectTimeout)
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.Token,
	}

	// 创建 thread + run，SSE 复用一个 run_id
	threadID, errCode := createThread(ctx, client, cfg.APIBaseURL, headers)
	if errCode != "" {
		result.Errors = append(result.Errors, errCode)
		return result
	}
	runID, errCode := createRun(ctx, client, cfg.APIBaseURL, threadID, headers, "lite")
	if errCode != "" {
		result.Errors = append(result.Errors, errCode)
		return result
	}

	eventsURL, err := httpx.JoinURL(cfg.APIBaseURL, "/v1/runs/"+runID+"/events")
	if err != nil {
		result.Errors = append(result.Errors, "config.invalid_base_url")
		return result
	}

	var attempted int64 = int64(cfg.Concurrency)
	var success int64
	var connected int64
	var connectErr int64
	var readErr int64

	errSet := sync.Map{}
	var netErrKinds sync.Map
	connectLat := make([]float64, 0, cfg.Concurrency)
	var connectLatMu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctxConn, cancel := context.WithCancel(ctx)
			timer := time.NewTimer(cfg.Hold)
			defer timer.Stop()

			req, _ := http.NewRequestWithContext(ctxConn, http.MethodGet, eventsURL, nil)
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			req.Header.Set("Accept", "text/event-stream")

			connectStart := time.Now()
			resp, err := sseClient.Do(req)
			if err != nil {
				addNetErrorKind(&netErrKinds, err)
				atomic.AddInt64(&connectErr, 1)
				_, _ = errSet.LoadOrStore("sse.connect.net.error", struct{}{})
				cancel()
				return
			}
			if resp.StatusCode != http.StatusOK {
				atomic.AddInt64(&connectErr, 1)
				code := "sse.connect.http." + itoa(resp.StatusCode)
				_, _ = errSet.LoadOrStore(code, struct{}{})
				_ = resp.Body.Close()
				cancel()
				return
			}
			atomic.AddInt64(&connected, 1)
			connectMs := float64(time.Since(connectStart).Nanoseconds()) / 1e6
			connectLatMu.Lock()
			connectLat = append(connectLat, connectMs)
			connectLatMu.Unlock()

			readDone := make(chan error, 1)
			go func() {
				buf := make([]byte, 256)
				for {
					_, rerr := resp.Body.Read(buf)
					if rerr != nil {
						readDone <- rerr
						return
					}
				}
			}()

			select {
			case <-timer.C:
				cancel()
				_ = resp.Body.Close()
				atomic.AddInt64(&success, 1)
				return
			case rerr := <-readDone:
				_ = resp.Body.Close()
				cancel()
				// hold 之前断开才算失败
				if rerr == io.EOF {
					atomic.AddInt64(&readErr, 1)
					_, _ = errSet.LoadOrStore("sse.read.eof", struct{}{})
					return
				}
				atomic.AddInt64(&readErr, 1)
				_, _ = errSet.LoadOrStore("sse.read.error", struct{}{})
				return
			}
		}()
	}

	wg.Wait()

	retention := 0.0
	if attempted > 0 {
		retention = float64(success) / float64(attempted)
	}

	result.Stats["attempted"] = attempted
	result.Stats["connected"] = connected
	result.Stats["held_success"] = success
	result.Stats["connect_error"] = connectErr
	result.Stats["read_error"] = readErr
	result.Stats["retention"] = retention
	if len(connectLat) > 0 {
		if sum, err := stats.SummarizeMs(connectLat); err == nil {
			result.Stats["connect_latency_ms"] = sum
		}
	}
	if kinds := snapshotNetErrorKinds(&netErrKinds); kinds != nil {
		result.Stats["net_error_kinds"] = kinds
	}

	errors := make([]string, 0, 8)
	errSet.Range(func(key, value any) bool {
		k, _ := key.(string)
		errors = append(errors, k)
		return true
	})
	result.Errors = append(result.Errors, errors...)

	result.Pass = retention >= cfg.Threshold.MinRetention
	return result
}

func createThread(ctx context.Context, client *http.Client, apiBaseURL string, headers map[string]string) (string, string) {
	u, err := httpx.JoinURL(apiBaseURL, "/v1/threads")
	if err != nil {
		return "", "config.invalid_base_url"
	}
	var resp struct {
		ID string `json:"id"`
	}
	err = httpx.DoJSON(ctx, client, http.MethodPost, u, headers, map[string]any{"title": "bench-sse"}, &resp)
	if err == nil && resp.ID != "" {
		return resp.ID, ""
	}
	return "", classifyAPIError("threads.create", err)
}

func createMessage(ctx context.Context, client *http.Client, apiBaseURL, threadID string, headers map[string]string, content string) string {
	u, err := httpx.JoinURL(apiBaseURL, "/v1/threads/"+threadID+"/messages")
	if err != nil {
		return "config.invalid_base_url"
	}
	body := map[string]any{
		"content": content,
	}
	err = httpx.DoJSON(ctx, client, http.MethodPost, u, headers, body, nil)
	if err == nil {
		return ""
	}
	return classifyAPIError("messages.create", err)
}

func createRun(ctx context.Context, client *http.Client, apiBaseURL, threadID string, headers map[string]string, personaID string) (string, string) {
	u, err := httpx.JoinURL(apiBaseURL, "/v1/threads/"+threadID+"/runs")
	if err != nil {
		return "", "config.invalid_base_url"
	}
	var resp struct {
		RunID string `json:"run_id"`
	}
	body := map[string]any{}
	if personaID != "" {
		body["persona_id"] = personaID
	}
	err = httpx.DoJSON(ctx, client, http.MethodPost, u, headers, body, &resp)
	if err == nil && resp.RunID != "" {
		return resp.RunID, ""
	}
	return "", classifyAPIError("runs.create", err)
}

func classifyAPIError(prefix string, err error) string {
	if err == nil {
		return ""
	}
	if httpErr, ok := err.(*httpx.HTTPError); ok {
		if httpErr.Code != "" {
			return prefix + ".code." + httpErr.Code
		}
		return prefix + ".http." + itoa(httpErr.Status)
	}
	return prefix + ".net.error"
}
