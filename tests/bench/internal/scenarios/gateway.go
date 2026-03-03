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

type GatewayConfig struct {
	BaseURL   string
	Warmup    time.Duration
	Duration  time.Duration
	QPS       int
	Workers   int
	Timeout   time.Duration
	Threshold GatewayThresholds
}

type GatewayThresholds struct {
	P99Ms      float64
	Min2xxRate float64
}

func DefaultGatewayConfig(baseURL string) GatewayConfig {
	return GatewayConfig{
		BaseURL:  baseURL,
		Warmup:   5 * time.Second,
		Duration: 30 * time.Second,
		QPS:      1000,
		Workers:  200,
		Timeout:  2 * time.Second,
		Threshold: GatewayThresholds{
			P99Ms:      10,
			Min2xxRate: 0.999,
		},
	}
}

func RunGatewayRatelimit(ctx context.Context, cfg GatewayConfig) report.ScenarioResult {
	result := report.ScenarioResult{
		Name:       "gateway_ratelimit",
		Config:     map[string]any{},
		Stats:      map[string]any{},
		Thresholds: map[string]any{},
		Pass:       false,
	}

	result.Config["warmup_s"] = cfg.Warmup.Seconds()
	result.Config["duration_s"] = cfg.Duration.Seconds()
	result.Config["qps"] = cfg.QPS
	result.Config["workers"] = cfg.Workers
	result.Config["timeout_s"] = cfg.Timeout.Seconds()

	result.Thresholds["p99_ms"] = cfg.Threshold.P99Ms
	result.Thresholds["min_2xx_rate"] = cfg.Threshold.Min2xxRate

	if cfg.QPS <= 0 || cfg.Workers <= 0 {
		result.Errors = append(result.Errors, "config.invalid")
		return result
	}

	u, err := httpx.JoinURL(cfg.BaseURL, "/healthz")
	if err != nil {
		result.Errors = append(result.Errors, "config.invalid_base_url")
		return result
	}

	client := httpx.NewClient(cfg.Timeout)

	// warmup
	_, _ = runGatewayPhase(ctx, client, u, cfg.QPS, cfg.Workers, cfg.Warmup)

	// measure
	phase, errs := runGatewayPhase(ctx, client, u, cfg.QPS, cfg.Workers, cfg.Duration)
	if len(errs) > 0 {
		result.Errors = append(result.Errors, errs...)
	}

	latSummary, sumErr := stats.SummarizeMs(phase.LatenciesMs)
	if sumErr != nil {
		result.Errors = append(result.Errors, "gateway.latency.empty")
	}
	errLatSummary, _ := stats.SummarizeMs(phase.ErrorLatenciesMs)
	var rate2xx float64
	if phase.TotalResponses > 0 {
		rate2xx = float64(phase.Status2xx) / float64(phase.TotalResponses)
	}
	achieved := 0.0
	if cfg.Duration > 0 {
		achieved = float64(phase.TotalResponses) / cfg.Duration.Seconds()
	}
	successRPS := 0.0
	if cfg.Duration > 0 {
		successRPS = float64(phase.Status2xx) / cfg.Duration.Seconds()
	}

	result.Stats["latency_ms"] = latSummary
	if errLatSummary.Count > 0 {
		result.Stats["latency_ms_error"] = errLatSummary
	}
	result.Stats["status_codes"] = phase.StatusCounts
	result.Stats["achieved_rps"] = achieved
	result.Stats["success_rps"] = successRPS
	result.Stats["responses_total"] = phase.TotalResponses
	result.Stats["responses_2xx"] = phase.Status2xx
	result.Stats["rate_2xx"] = rate2xx
	result.Stats["dropped_jobs"] = phase.DroppedJobs
	result.Stats["attempted_jobs"] = phase.AttemptedJobs
	result.Stats["started_jobs"] = phase.AttemptedJobs - phase.DroppedJobs
	result.Stats["net_errors"] = phase.NetErrors
	if phase.NetErrorKinds != nil {
		result.Stats["net_error_kinds"] = phase.NetErrorKinds
	}

	pass := latSummary.Count > 0 &&
		latSummary.P99Ms > 0 &&
		latSummary.P99Ms < cfg.Threshold.P99Ms &&
		rate2xx >= cfg.Threshold.Min2xxRate

	result.Pass = pass
	return result
}

type gatewayPhaseStats struct {
	LatenciesMs      []float64
	ErrorLatenciesMs []float64
	NetErrorKinds    map[string]int64
	StatusCounts     map[string]int64
	TotalResponses   int64
	Status2xx        int64
	DroppedJobs      int64
	NetErrors        int64
	AttemptedJobs    int64
}

func runGatewayPhase(ctx context.Context, client *http.Client, url string, qps int, workers int, duration time.Duration) (gatewayPhaseStats, []string) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, duration+2*time.Second)
	defer cancel()

	jobs := make(chan struct{}, qps)
	var dropped int64
	var attempted int64

	stop := make(chan struct{})

	expected := int(float64(qps) * duration.Seconds())
	if expected < qps {
		expected = qps
	}
	if expected < 1 {
		expected = 1
	}
	latOKCh := make(chan float64, expected)
	latErrCh := make(chan float64, expected)
	statusCh := make(chan int, expected)
	errCh := make(chan string, 16)
	var netErrKinds sync.Map
	var netErrors int64

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				start := time.Now()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
				resp, err := client.Do(req)
				latMs := float64(time.Since(start).Nanoseconds()) / 1e6
				if err != nil {
					atomic.AddInt64(&netErrors, 1)
					addNetErrorKind(&netErrKinds, err)
					select {
					case latErrCh <- latMs:
					default:
					}
					select {
					case errCh <- "gateway.net.error":
					default:
					}
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				latOKCh <- latMs
				statusCh <- resp.StatusCode
			}
		}()
	}

	interval := time.Second / time.Duration(qps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	end := time.NewTimer(duration)
	defer end.Stop()

	go func() {
		defer close(stop)
		for {
			select {
			case <-ctx.Done():
				return
			case <-end.C:
				return
			case <-ticker.C:
				atomic.AddInt64(&attempted, 1)
				select {
				case jobs <- struct{}{}:
				default:
					atomic.AddInt64(&dropped, 1)
				}
			}
		}
	}()

	<-stop
	close(jobs)
	wg.Wait()
	close(latOKCh)
	close(latErrCh)
	close(statusCh)

	latencies := make([]float64, 0, expected)
	for v := range latOKCh {
		latencies = append(latencies, v)
	}
	errLatencies := make([]float64, 0, len(latErrCh))
	for v := range latErrCh {
		errLatencies = append(errLatencies, v)
	}

	statusCounts := map[string]int64{}
	var total int64
	var status2xx int64
	for code := range statusCh {
		total++
		if code >= 200 && code <= 299 {
			status2xx++
		}
		key := "http." + itoa(code)
		statusCounts[key]++
	}
	netErrN := atomic.LoadInt64(&netErrors)
	if netErrN > 0 {
		statusCounts["net.error"] = netErrN
		total += netErrN
	}

	errSet := map[string]struct{}{}
	for {
		select {
		case e := <-errCh:
			if e == "" {
				continue
			}
			errSet[e] = struct{}{}
		default:
			goto DONE
		}
	}
DONE:
	errors := make([]string, 0, len(errSet))
	for k := range errSet {
		errors = append(errors, k)
	}

	return gatewayPhaseStats{
		LatenciesMs:      latencies,
		ErrorLatenciesMs: errLatencies,
		NetErrorKinds:    snapshotNetErrorKinds(&netErrKinds),
		StatusCounts:     statusCounts,
		TotalResponses:   total,
		Status2xx:        status2xx,
		DroppedJobs:      atomic.LoadInt64(&dropped),
		NetErrors:        netErrN,
		AttemptedJobs:    atomic.LoadInt64(&attempted),
	}, errors
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := make([]byte, 0, 16)
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
