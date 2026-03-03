package scenarios

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/tests/bench/internal/httpx"
	"arkloop/tests/bench/internal/report"
	"arkloop/tests/bench/internal/stats"
)

type APICRUDConfig struct {
	BaseURL     string
	Token       string
	DBDSN       string
	Warmup      time.Duration
	Duration    time.Duration
	Concurrency int
	Threshold   APICRUDThresholds
}

type APICRUDThresholds struct {
	P99Ms      float64
	Min2xxRate float64
}

func DefaultAPICRUDConfig(baseURL string, token string) APICRUDConfig {
	return APICRUDConfig{
		BaseURL:     baseURL,
		Token:       token,
		Warmup:      5 * time.Second,
		Duration:    30 * time.Second,
		Concurrency: 200,
		Threshold: APICRUDThresholds{
			P99Ms:      100,
			Min2xxRate: 0.995,
		},
	}
}

func RunAPICRUD(ctx context.Context, cfg APICRUDConfig) report.ScenarioResult {
	result := report.ScenarioResult{
		Name:       "api_crud",
		Config:     map[string]any{},
		Stats:      map[string]any{},
		Thresholds: map[string]any{},
		Pass:       false,
	}

	result.Config["warmup_s"] = cfg.Warmup.Seconds()
	result.Config["duration_s"] = cfg.Duration.Seconds()
	result.Config["concurrency"] = cfg.Concurrency

	result.Thresholds["p99_ms"] = cfg.Threshold.P99Ms
	result.Thresholds["min_2xx_rate"] = cfg.Threshold.Min2xxRate

	if cfg.Token == "" {
		result.Errors = append(result.Errors, "auth.missing_token")
		return result
	}
	if cfg.Concurrency <= 0 {
		result.Errors = append(result.Errors, "config.invalid")
		return result
	}

	baseCreate, err := httpx.JoinURL(cfg.BaseURL, "/v1/threads")
	if err != nil {
		result.Errors = append(result.Errors, "config.invalid_base_url")
		return result
	}

	client := httpx.NewClient(2 * time.Second)
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.Token,
	}

	mon := startPGStatActivityMonitor(ctx, cfg.DBDSN)
	if mon != nil {
		defer mon.Stop()
	}

	// warmup
	_, _ = runAPICRUDPhase(ctx, client, headers, baseCreate, cfg.Concurrency, cfg.Warmup)

	// measure
	phase, errs := runAPICRUDPhase(ctx, client, headers, baseCreate, cfg.Concurrency, cfg.Duration)
	if len(errs) > 0 {
		result.Errors = append(result.Errors, errs...)
	}

	overall, sumErr := stats.SummarizeMs(phase.OverallLatMs)
	if sumErr != nil {
		result.Errors = append(result.Errors, "api_crud.latency.empty")
	}
	createSum, _ := stats.SummarizeMs(phase.CreateLatMs)
	getSum, _ := stats.SummarizeMs(phase.GetLatMs)
	patchSum, _ := stats.SummarizeMs(phase.PatchLatMs)
	deleteSum, _ := stats.SummarizeMs(phase.DeleteLatMs)

	var rate2xx float64
	if phase.TotalResponses > 0 {
		rate2xx = float64(phase.Status2xx) / float64(phase.TotalResponses)
	}

	result.Stats["latency_ms_overall"] = overall
	result.Stats["latency_ms_create"] = createSum
	result.Stats["latency_ms_get"] = getSum
	result.Stats["latency_ms_patch"] = patchSum
	result.Stats["latency_ms_delete"] = deleteSum
	result.Stats["responses_total"] = phase.TotalResponses
	result.Stats["responses_2xx"] = phase.Status2xx
	result.Stats["rate_2xx"] = rate2xx
	result.Stats["status_codes"] = phase.StatusCounts
	if phase.NetErrorKinds != nil {
		result.Stats["net_error_kinds"] = phase.NetErrorKinds
	}
	if mon != nil {
		result.Stats["pg_stat_activity_max_total"] = mon.MaxTotal()
		result.Stats["pg_stat_activity_max_active"] = mon.MaxActive()
		if code := mon.ErrCode(); code != "" {
			result.Errors = append(result.Errors, "api_crud."+code)
		}
	} else {
		result.Stats["pg_stat_activity_max_total"] = int64(0)
		result.Stats["pg_stat_activity_max_active"] = int64(0)
	}

	pass := overall.Count > 0 &&
		overall.P99Ms > 0 &&
		overall.P99Ms < cfg.Threshold.P99Ms &&
		rate2xx >= cfg.Threshold.Min2xxRate
	result.Pass = pass
	return result
}

type apiCRUDPhaseStats struct {
	OverallLatMs []float64
	CreateLatMs  []float64
	GetLatMs     []float64
	PatchLatMs   []float64
	DeleteLatMs  []float64

	NetErrorKinds  map[string]int64
	StatusCounts   map[string]int64
	TotalResponses int64
	Status2xx      int64
}

type threadResponse struct {
	ID string `json:"id"`
}

func runAPICRUDPhase(
	ctx context.Context,
	client *http.Client,
	headers map[string]string,
	createThreadsURL string,
	concurrency int,
	duration time.Duration,
) (apiCRUDPhaseStats, []string) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, duration+2*time.Second)
	defer cancel()

	var total int64
	var ok2xx int64
	statusCounts := map[string]int64{}
	var statusMu sync.Mutex
	errSet := sync.Map{}
	var netErrKinds sync.Map

	type workerStats struct {
		overall []float64
		create  []float64
		get     []float64
		patch   []float64
		del     []float64
	}
	workers := make([]workerStats, concurrency)

	end := time.Now().Add(duration)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			for time.Now().Before(end) {
				title := "bench-" + RandHex(6)

				// 1. create
				start := time.Now()
				var created threadResponse
				err := httpx.DoJSON(ctx, client, http.MethodPost, createThreadsURL, headers, map[string]any{"title": title}, &created)
				addNetErrorKind(&netErrKinds, err)
				durCreate := float64(time.Since(start).Nanoseconds()) / 1e6
				atomic.AddInt64(&total, 1)
				recordResult(statusCounts, &statusMu, &errSet, "create", 201, err, &ok2xx)
				if err != nil || created.ID == "" {
					continue
				}
				workers[idx].create = append(workers[idx].create, durCreate)

				threadURL := createThreadsURL + "/" + created.ID

				// 2. get
				start = time.Now()
				var got threadResponse
				err = httpx.DoJSON(ctx, client, http.MethodGet, threadURL, headers, nil, &got)
				addNetErrorKind(&netErrKinds, err)
				durGet := float64(time.Since(start).Nanoseconds()) / 1e6
				atomic.AddInt64(&total, 1)
				recordResult(statusCounts, &statusMu, &errSet, "get", 200, err, &ok2xx)
				if err != nil {
					continue
				}
				workers[idx].get = append(workers[idx].get, durGet)

				// 3. patch
				start = time.Now()
				newTitle := "bench-upd-" + RandHex(6)
				var patched threadResponse
				err = httpx.DoJSON(ctx, client, http.MethodPatch, threadURL, headers, map[string]any{"title": newTitle}, &patched)
				addNetErrorKind(&netErrKinds, err)
				durPatch := float64(time.Since(start).Nanoseconds()) / 1e6
				atomic.AddInt64(&total, 1)
				recordResult(statusCounts, &statusMu, &errSet, "patch", 200, err, &ok2xx)
				if err != nil {
					continue
				}
				workers[idx].patch = append(workers[idx].patch, durPatch)

				// 4. delete
				start = time.Now()
				err = httpx.DoJSON(ctx, client, http.MethodDelete, threadURL, headers, nil, nil)
				addNetErrorKind(&netErrKinds, err)
				durDel := float64(time.Since(start).Nanoseconds()) / 1e6
				atomic.AddInt64(&total, 1)
				recordResult(statusCounts, &statusMu, &errSet, "delete", 204, err, &ok2xx)
				if err != nil {
					continue
				}
				workers[idx].del = append(workers[idx].del, durDel)

				workers[idx].overall = append(workers[idx].overall, durCreate+durGet+durPatch+durDel)
			}
		}(i)
	}

	wg.Wait()

	overallLat := make([]float64, 0, total)
	createLat := make([]float64, 0, total)
	getLat := make([]float64, 0, total)
	patchLat := make([]float64, 0, total)
	delLat := make([]float64, 0, total)
	for _, w := range workers {
		overallLat = append(overallLat, w.overall...)
		createLat = append(createLat, w.create...)
		getLat = append(getLat, w.get...)
		patchLat = append(patchLat, w.patch...)
		delLat = append(delLat, w.del...)
	}

	errors := make([]string, 0, 16)
	errSet.Range(func(key, value any) bool {
		k, _ := key.(string)
		errors = append(errors, k)
		return true
	})

	return apiCRUDPhaseStats{
		OverallLatMs:   overallLat,
		CreateLatMs:    createLat,
		GetLatMs:       getLat,
		PatchLatMs:     patchLat,
		DeleteLatMs:    delLat,
		NetErrorKinds:  snapshotNetErrorKinds(&netErrKinds),
		StatusCounts:   statusCounts,
		TotalResponses: atomic.LoadInt64(&total),
		Status2xx:      atomic.LoadInt64(&ok2xx),
	}, errors
}

func recordResult(statusCounts map[string]int64, statusMu *sync.Mutex, errSet *sync.Map, op string, successStatus int, err error, ok2xx *int64) {
	statusKey := "http.0"
	status := 0
	errKey := ""

	if err == nil {
		status = successStatus
		statusKey = "http." + itoa(status)
		atomic.AddInt64(ok2xx, 1)
	} else if httpErr, ok := err.(*httpx.HTTPError); ok {
		status = httpErr.Status
		statusKey = "http." + itoa(status)
		if httpErr.Code != "" {
			errKey = "api_crud." + op + ".code." + httpErr.Code
		} else {
			errKey = "api_crud." + op + ".http." + itoa(status)
		}
	} else {
		statusKey = "net.error"
		errKey = "api_crud." + op + ".net.error"
	}

	statusMu.Lock()
	statusCounts[statusKey]++
	statusMu.Unlock()

	if errKey != "" {
		_, _ = errSet.LoadOrStore(errKey, struct{}{})
	}
}
