package scenarios

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/tests/bench/internal/httpx"
	"arkloop/tests/bench/internal/report"
	"arkloop/tests/bench/internal/stats"
)

type BrowserNavigateConfig struct {
	BaseURL     string
	Warmup      time.Duration
	Duration    time.Duration
	Concurrency int
	TimeoutMs   int
	Threshold   BrowserNavigateThresholds
}

type BrowserNavigateThresholds struct {
	MaxMemoryBytes int64
	Min2xxRate     float64
}

func DefaultBrowserNavigateConfig(baseURL string) BrowserNavigateConfig {
	return BrowserNavigateConfig{
		BaseURL:     baseURL,
		Warmup:      5 * time.Second,
		Duration:    30 * time.Second,
		Concurrency: 20,
		TimeoutMs:   30_000,
		Threshold: BrowserNavigateThresholds{
			MaxMemoryBytes: 4 * 1024 * 1024 * 1024,
			Min2xxRate:     0.95,
		},
	}
}

func RunBrowserNavigate(ctx context.Context, cfg BrowserNavigateConfig) report.ScenarioResult {
	result := report.ScenarioResult{
		Name:       "browser_navigate",
		Config:     map[string]any{},
		Stats:      map[string]any{},
		Thresholds: map[string]any{},
		Pass:       false,
	}

	result.Config["warmup_s"] = cfg.Warmup.Seconds()
	result.Config["duration_s"] = cfg.Duration.Seconds()
	result.Config["concurrency"] = cfg.Concurrency
	result.Config["timeout_ms"] = cfg.TimeoutMs

	result.Thresholds["max_memory_bytes"] = cfg.Threshold.MaxMemoryBytes
	result.Thresholds["min_2xx_rate"] = cfg.Threshold.Min2xxRate

	if cfg.Concurrency <= 0 {
		result.Errors = append(result.Errors, "config.invalid")
		return result
	}

	client := httpx.NewClient(time.Duration(cfg.TimeoutMs+5000) * time.Millisecond)
	u, err := httpx.JoinURL(cfg.BaseURL, "/v1/navigate")
	if err != nil {
		result.Errors = append(result.Errors, "config.invalid_base_url")
		return result
	}

	memStart := sampleBrowserMemory(ctx, cfg.BaseURL)
	if memStart != nil {
		result.Stats["docker_memory_start"] = memStart
	}

	// warmup
	_, _ = runBrowserNavigatePhase(ctx, client, u, cfg.Concurrency, cfg.TimeoutMs, cfg.Warmup, false)

	// measure
	phase, errs := runBrowserNavigatePhase(ctx, client, u, cfg.Concurrency, cfg.TimeoutMs, cfg.Duration, true)
	if len(errs) > 0 {
		result.Errors = append(result.Errors, errs...)
	}

	lat, sumErr := stats.SummarizeMs(phase.LatenciesMs)
	if sumErr != nil {
		result.Errors = append(result.Errors, "browser.latency.empty")
	}
	result.Stats["latency_ms"] = lat
	result.Stats["responses_total"] = phase.TotalResponses
	result.Stats["responses_2xx"] = phase.Status2xx
	result.Stats["status_codes"] = phase.StatusCounts
	rate2xx := 0.0
	if phase.TotalResponses > 0 {
		rate2xx = float64(phase.Status2xx) / float64(phase.TotalResponses)
	}
	result.Stats["rate_2xx"] = rate2xx
	if phase.NetErrorKinds != nil {
		result.Stats["net_error_kinds"] = phase.NetErrorKinds
	}

	// docker memory sample
	memEnd := sampleBrowserMemory(ctx, cfg.BaseURL)
	if memEnd != nil {
		result.Stats["docker_memory"] = memEnd
	}

	pass := true
	if lat.Count == 0 {
		pass = false
	}
	if cfg.Threshold.Min2xxRate > 0 && rate2xx < cfg.Threshold.Min2xxRate {
		pass = false
	}
	if memEnd != nil {
		if used, ok := memEnd["used_bytes"].(int64); ok && used > 0 && used > cfg.Threshold.MaxMemoryBytes {
			pass = false
		}
	}
	result.Pass = pass
	return result
}

type browserPhaseStats struct {
	LatenciesMs    []float64
	NetErrorKinds  map[string]int64
	StatusCounts   map[string]int64
	TotalResponses int64
	Status2xx      int64
}

func runBrowserNavigatePhase(
	ctx context.Context,
	client *http.Client,
	url string,
	concurrency int,
	timeoutMs int,
	duration time.Duration,
	record bool,
) (browserPhaseStats, []string) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, duration+10*time.Second)
	defer cancel()

	var total int64
	var ok2xx int64
	statusCounts := map[string]int64{}
	var statusMu sync.Mutex
	errSet := sync.Map{}
	var netErrKinds sync.Map

	type workerStats struct {
		lat []float64
	}
	workers := make([]workerStats, concurrency)
	sessions := make([]struct {
		sessionID string
		orgID     string
		runID     string
	}, concurrency)
	for i := range sessions {
		sessions[i] = struct {
			sessionID string
			orgID     string
			runID     string
		}{
			sessionID: UUIDString(),
			orgID:     UUIDString(),
			runID:     UUIDString(),
		}
	}

	end := time.Now().Add(duration)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fresh := true
			for time.Now().Before(end) {
				body := map[string]any{
					"url":           "about:blank",
					"wait_until":    "load",
					"timeout_ms":    timeoutMs,
					"fresh_session": fresh,
				}
				fresh = false

				headers := map[string]string{
					"X-Session-ID": sessions[idx].sessionID,
					"X-Org-ID":     sessions[idx].orgID,
					"X-Run-ID":     sessions[idx].runID,
				}

				start := time.Now()
				err := httpx.DoJSON(ctx, client, http.MethodPost, url, headers, body, nil)
				latMs := float64(time.Since(start).Nanoseconds()) / 1e6

				atomic.AddInt64(&total, 1)
				addNetErrorKind(&netErrKinds, err)
				recordBrowserStatus(statusCounts, &statusMu, &errSet, err, &ok2xx)
				if record && err == nil {
					workers[idx].lat = append(workers[idx].lat, latMs)
				} else if err != nil {
					// 服务不可用时避免无休止的紧密重试，把错误“打爆”掩盖真实行为。
					select {
					case <-ctx.Done():
						return
					case <-time.After(50 * time.Millisecond):
					}
				}
			}
		}(i)
	}
	wg.Wait()

	latencies := make([]float64, 0, total)
	for _, w := range workers {
		latencies = append(latencies, w.lat...)
	}

	errors := make([]string, 0, 8)
	errSet.Range(func(key, value any) bool {
		k, _ := key.(string)
		errors = append(errors, k)
		return true
	})

	return browserPhaseStats{
		LatenciesMs:    latencies,
		NetErrorKinds:  snapshotNetErrorKinds(&netErrKinds),
		StatusCounts:   statusCounts,
		TotalResponses: atomic.LoadInt64(&total),
		Status2xx:      atomic.LoadInt64(&ok2xx),
	}, errors
}

func recordBrowserStatus(statusCounts map[string]int64, statusMu *sync.Mutex, errSet *sync.Map, err error, ok2xx *int64) {
	statusKey := "http.0"
	errKey := ""
	if err == nil {
		statusKey = "http.200"
		atomic.AddInt64(ok2xx, 1)
	} else if httpErr, ok := err.(*httpx.HTTPError); ok {
		statusKey = "http." + itoa(httpErr.Status)
		if httpErr.Code != "" {
			errKey = "browser.code." + httpErr.Code
		} else {
			errKey = "browser.http." + itoa(httpErr.Status)
		}
	} else {
		statusKey = "net.error"
		errKey = "browser.net.error"
	}

	statusMu.Lock()
	statusCounts[statusKey]++
	statusMu.Unlock()
	if errKey != "" {
		_, _ = errSet.LoadOrStore(errKey, struct{}{})
	}
}

func sampleBrowserMemory(ctx context.Context, browserBaseURL string) map[string]any {
	if ctx == nil {
		ctx = context.Background()
	}

	composeFile, projectName := resolveComposeTarget(browserBaseURL)
	if composeFile == "" {
		return nil
	}

	args := []string{"compose", "-f", composeFile}
	if projectName != "" {
		args = append(args, "-p", projectName)
	}
	args = append(args, "ps", "-q", "browser")

	idOut, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil
	}
	containerID := strings.TrimSpace(string(idOut))
	if containerID == "" {
		return nil
	}

	statsOut, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{.MemUsage}}", containerID).Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(statsOut))
	used, limit := parseDockerMemUsage(raw)

	out := map[string]any{
		"raw": raw,
	}
	if used > 0 {
		out["used_bytes"] = used
	}
	if limit > 0 {
		out["limit_bytes"] = limit
	}
	return out
}

func resolveComposeTarget(browserBaseURL string) (composeFile string, projectName string) {
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_BENCH_DOCKER_COMPOSE_FILE")); raw != "" {
		if _, err := os.Stat(raw); err == nil {
			composeFile = raw
		}
	}

	isBench := false
	if parsed, err := url.Parse(strings.TrimSpace(browserBaseURL)); err == nil {
		if parsed.Port() == "3105" {
			isBench = true
		}
	}
	if !isBench && strings.Contains(browserBaseURL, ":3105") {
		isBench = true
	}

	if composeFile == "" {
		if isBench {
			if _, err := os.Stat("compose.bench.yaml"); err == nil {
				composeFile = "compose.bench.yaml"
			}
		}
	}
	if composeFile == "" {
		if _, err := os.Stat("compose.yaml"); err == nil {
			composeFile = "compose.yaml"
		}
	}

	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_BENCH_DOCKER_PROJECT")); raw != "" {
		projectName = raw
	} else if isBench {
		projectName = "arkloop-bench"
	}

	return composeFile, projectName
}

func parseDockerMemUsage(raw string) (usedBytes int64, limitBytes int64) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	usedBytes = parseBytesUnit(strings.TrimSpace(parts[0]))
	limitBytes = parseBytesUnit(strings.TrimSpace(parts[1]))
	return usedBytes, limitBytes
}

func parseBytesUnit(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// e.g. "123.4MiB", "1.23GiB", "10MB"
	unitIdx := -1
	for i := 0; i < len(raw); i++ {
		if (raw[i] < '0' || raw[i] > '9') && raw[i] != '.' {
			unitIdx = i
			break
		}
	}
	if unitIdx <= 0 {
		return 0
	}
	numPart := strings.TrimSpace(raw[:unitIdx])
	unit := strings.TrimSpace(raw[unitIdx:])

	val, err := strconv.ParseFloat(numPart, 64)
	if err != nil || val < 0 {
		return 0
	}

	switch unit {
	case "B":
		return int64(val)
	case "kB", "KB":
		return int64(val * 1000)
	case "MB":
		return int64(val * 1000 * 1000)
	case "GB":
		return int64(val * 1000 * 1000 * 1000)
	case "KiB":
		return int64(val * 1024)
	case "MiB":
		return int64(val * 1024 * 1024)
	case "GiB":
		return int64(val * 1024 * 1024 * 1024)
	default:
		return 0
	}
}
