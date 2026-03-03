package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"arkloop/tests/bench/internal/bootstrap"
	"arkloop/tests/bench/internal/httpx"
	"arkloop/tests/bench/internal/report"
	"arkloop/tests/bench/internal/scenarios"
)

const (
	envAccessToken       = "ARKLOOP_BENCH_ACCESS_TOKEN"
	envDatabaseURL       = "DATABASE_URL"
	envOpenVikingRootKey = "ARKLOOP_OPENVIKING_ROOT_API_KEY"
)

func main() {
	if len(os.Args) < 2 {
		_, _ = os.Stderr.WriteString("usage: bench <baseline|gateway|api-crud|sse|worker|browser|openviking>\n")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "baseline":
		runBaseline(os.Args[2:])
	case "gateway":
		runGateway(os.Args[2:])
	case "api-crud":
		runAPICRUD(os.Args[2:])
	case "sse":
		runSSE(os.Args[2:])
	case "worker":
		runWorker(os.Args[2:])
	case "browser":
		runBrowser(os.Args[2:])
	case "openviking":
		runOpenViking(os.Args[2:])
	default:
		_, _ = os.Stderr.WriteString("unknown command\n")
		os.Exit(2)
	}
}

func commonFlags(fs *flag.FlagSet) (gateway, api, browser, openviking, accessToken, dbDSN *string, forceOpen *bool, out *string) {
	gateway = fs.String("gateway", "http://127.0.0.1:8005", "gateway base url")
	api = fs.String("api", "http://127.0.0.1:8006", "api base url")
	browser = fs.String("browser", "http://127.0.0.1:3105", "browser base url")
	openviking = fs.String("openviking", "http://127.0.0.1:1938", "openviking base url")
	accessToken = fs.String("access-token", "", "access token")
	dbDSN = fs.String("db-dsn", "", "database dsn")
	forceOpen = fs.Bool("force-open-registration", false, "force set registration.open=true")
	out = fs.String("out", "", "write report to file")
	return
}

func resolveToken(ctx context.Context, apiBaseURL string, accessToken string, dbDSN string, forceOpen bool) (string, string) {
	token := strings.TrimSpace(accessToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv(envAccessToken))
	}
	if token != "" {
		return token, ""
	}

	if strings.TrimSpace(dbDSN) == "" {
		dbDSN = strings.TrimSpace(os.Getenv(envDatabaseURL))
	}
	if strings.TrimSpace(dbDSN) != "" {
		if err := bootstrap.EnsureRegistrationOpen(ctx, dbDSN, forceOpen); err != nil {
			return "", "bootstrap.feature_flags.error"
		}
	}

	u, err := httpx.JoinURL(apiBaseURL, "/v1/auth/register")
	if err != nil {
		return "", "config.invalid_base_url"
	}

	body := map[string]any{
		"login":              "bench_" + scenarios.RandHex(6),
		"password":           "bench_pwd_123456",
		"email":              "bench+" + scenarios.RandHex(4) + "@example.com",
		"locale":             "zh-CN",
		"cf_turnstile_token": "",
	}

	var resp struct {
		AccessToken string `json:"access_token"`
	}
	client := httpx.NewClient(2 * time.Second)
	err = httpx.DoJSON(ctx, client, "POST", u, nil, body, &resp)
	if err == nil && strings.TrimSpace(resp.AccessToken) != "" {
		return strings.TrimSpace(resp.AccessToken), ""
	}
	if httpErr, ok := err.(*httpx.HTTPError); ok {
		if httpErr.Code != "" {
			return "", "auth.register.code." + httpErr.Code
		}
		return "", "auth.register.http." + itoa(httpErr.Status)
	}
	return "", "auth.register.net.error"
}

func resolveDBDSN(flagValue string) string {
	cleaned := strings.TrimSpace(flagValue)
	if cleaned != "" {
		return cleaned
	}
	return strings.TrimSpace(os.Getenv(envDatabaseURL))
}

func waitHealth(ctx context.Context, url string) error {
	client := httpx.NewClient(2 * time.Second)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("not ready")
}

func waitServiceReady(ctx context.Context, baseURL string, healthPath string, notReadyCode string) string {
	u, err := httpx.JoinURL(baseURL, healthPath)
	if err != nil {
		return "config.invalid_base_url"
	}
	if err := waitHealth(ctx, u); err != nil {
		return notReadyCode
	}
	return ""
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

func runBaseline(args []string) {
	fs := flag.NewFlagSet("baseline", flag.ExitOnError)
	gateway, api, browser, openviking, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
	includeOpenViking := fs.Bool("include-openviking", false, "include openviking scenario")
	openvikingRootKey := fs.String("openviking-root-key", "", "openviking root api key")
	fs.Parse(args)

	ctx := context.Background()
	effectiveDBDSN := resolveDBDSN(*dbDSN)

	targets := report.Targets{
		GatewayBaseURL:    strings.TrimSpace(*gateway),
		APIBaseURL:        strings.TrimSpace(*api),
		BrowserBaseURL:    strings.TrimSpace(*browser),
		OpenVikingBaseURL: strings.TrimSpace(*openviking),
	}
	rep := report.Report{
		Meta: report.BuildMeta(ctx, targets),
	}

	gatewayReadyErr := waitServiceReady(ctx, targets.GatewayBaseURL, "/healthz", "gateway.not_ready")
	apiReadyErr := waitServiceReady(ctx, targets.APIBaseURL, "/healthz", "api.not_ready")
	openVikingReadyErr := ""
	if *includeOpenViking {
		openVikingReadyErr = waitServiceReady(ctx, targets.OpenVikingBaseURL, "/health", "openviking.not_ready")
	}

	if gatewayReadyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("gateway_ratelimit", gatewayReadyErr))
	} else {
		rep.Results = append(rep.Results, scenarios.RunGatewayRatelimit(ctx, scenarios.DefaultGatewayConfig(targets.GatewayBaseURL)))
	}

	if apiReadyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("api_crud", apiReadyErr))
		rep.Results = append(rep.Results, tokenRequiredResult("sse_hold", apiReadyErr))
		rep.Results = append(rep.Results, tokenRequiredResult("worker_runs", apiReadyErr))
	} else {
		token, tokenErr := resolveToken(ctx, targets.APIBaseURL, *accessToken, effectiveDBDSN, *forceOpen)
		if tokenErr != "" {
			rep.Results = append(rep.Results, tokenRequiredResult("api_crud", tokenErr))
			rep.Results = append(rep.Results, tokenRequiredResult("sse_hold", tokenErr))
			rep.Results = append(rep.Results, tokenRequiredResult("worker_runs", tokenErr))
		} else {
			apiCfg := scenarios.DefaultAPICRUDConfig(targets.APIBaseURL, token)
			apiCfg.DBDSN = effectiveDBDSN
			rep.Results = append(rep.Results, scenarios.RunAPICRUD(ctx, apiCfg))
			rep.Results = append(rep.Results, scenarios.RunSSEHold(ctx, scenarios.DefaultSSEHoldConfig(targets.APIBaseURL, token)))
			workerCfg := scenarios.DefaultWorkerRunsConfig(targets.APIBaseURL, token)
			workerCfg.DBDSN = effectiveDBDSN
			rep.Results = append(rep.Results, scenarios.RunWorkerRuns(ctx, workerCfg))
		}
	}

	if *includeOpenViking {
		if openVikingReadyErr != "" {
			rep.Results = append(rep.Results, tokenRequiredResult("openviking_find", openVikingReadyErr))
		} else {
			rootKey := strings.TrimSpace(*openvikingRootKey)
			if rootKey == "" {
				rootKey = strings.TrimSpace(os.Getenv(envOpenVikingRootKey))
			}
			rep.Results = append(rep.Results, scenarios.RunOpenVikingFind(ctx, scenarios.DefaultOpenVikingFindConfig(targets.OpenVikingBaseURL, rootKey)))
		}
	}

	rep.OverallPass = true
	for _, r := range rep.Results {
		if !r.Pass {
			rep.OverallPass = false
		}
	}

	writeReportAndExit(rep, *out)
}

func runGateway(args []string) {
	fs := flag.NewFlagSet("gateway", flag.ExitOnError)
	gateway, _, _, _, _, _, _, out := commonFlags(fs)
	fs.Parse(args)

	ctx := context.Background()
	targets := report.Targets{GatewayBaseURL: strings.TrimSpace(*gateway)}
	rep := report.Report{
		Meta: report.BuildMeta(ctx, targets),
	}
	readyErr := waitServiceReady(ctx, targets.GatewayBaseURL, "/healthz", "gateway.not_ready")
	if readyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("gateway_ratelimit", readyErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	rep.Results = append(rep.Results, scenarios.RunGatewayRatelimit(ctx, scenarios.DefaultGatewayConfig(targets.GatewayBaseURL)))
	rep.OverallPass = rep.Results[0].Pass
	writeReportAndExit(rep, *out)
}

func runAPICRUD(args []string) {
	fs := flag.NewFlagSet("api-crud", flag.ExitOnError)
	_, api, _, _, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
	fs.Parse(args)

	ctx := context.Background()
	effectiveDBDSN := resolveDBDSN(*dbDSN)
	rep := report.Report{
		Meta: report.BuildMeta(ctx, report.Targets{APIBaseURL: strings.TrimSpace(*api)}),
	}
	readyErr := waitServiceReady(ctx, strings.TrimSpace(*api), "/healthz", "api.not_ready")
	if readyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("api_crud", readyErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	token, tokenErr := resolveToken(ctx, strings.TrimSpace(*api), *accessToken, effectiveDBDSN, *forceOpen)
	if tokenErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("api_crud", tokenErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}
	cfg := scenarios.DefaultAPICRUDConfig(strings.TrimSpace(*api), token)
	cfg.DBDSN = effectiveDBDSN
	rep.Results = append(rep.Results, scenarios.RunAPICRUD(ctx, cfg))
	rep.OverallPass = rep.Results[0].Pass
	writeReportAndExit(rep, *out)
}

func runSSE(args []string) {
	fs := flag.NewFlagSet("sse", flag.ExitOnError)
	_, api, _, _, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
	fs.Parse(args)

	ctx := context.Background()
	effectiveDBDSN := resolveDBDSN(*dbDSN)
	rep := report.Report{
		Meta: report.BuildMeta(ctx, report.Targets{APIBaseURL: strings.TrimSpace(*api)}),
	}
	readyErr := waitServiceReady(ctx, strings.TrimSpace(*api), "/healthz", "api.not_ready")
	if readyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("sse_hold", readyErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	token, tokenErr := resolveToken(ctx, strings.TrimSpace(*api), *accessToken, effectiveDBDSN, *forceOpen)
	if tokenErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("sse_hold", tokenErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}
	rep.Results = append(rep.Results, scenarios.RunSSEHold(ctx, scenarios.DefaultSSEHoldConfig(strings.TrimSpace(*api), token)))
	rep.OverallPass = rep.Results[0].Pass
	writeReportAndExit(rep, *out)
}

func runWorker(args []string) {
	fs := flag.NewFlagSet("worker", flag.ExitOnError)
	_, api, _, _, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
	fs.Parse(args)

	ctx := context.Background()
	effectiveDBDSN := resolveDBDSN(*dbDSN)
	rep := report.Report{
		Meta: report.BuildMeta(ctx, report.Targets{APIBaseURL: strings.TrimSpace(*api)}),
	}
	readyErr := waitServiceReady(ctx, strings.TrimSpace(*api), "/healthz", "api.not_ready")
	if readyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("worker_runs", readyErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	token, tokenErr := resolveToken(ctx, strings.TrimSpace(*api), *accessToken, effectiveDBDSN, *forceOpen)
	if tokenErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("worker_runs", tokenErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	cfg := scenarios.DefaultWorkerRunsConfig(strings.TrimSpace(*api), token)
	cfg.DBDSN = effectiveDBDSN
	rep.Results = append(rep.Results, scenarios.RunWorkerRuns(ctx, cfg))
	rep.OverallPass = rep.Results[0].Pass
	writeReportAndExit(rep, *out)
}

func runBrowser(args []string) {
	fs := flag.NewFlagSet("browser", flag.ExitOnError)
	_, _, browser, _, _, _, _, out := commonFlags(fs)
	fs.Parse(args)

	ctx := context.Background()
	targets := report.Targets{BrowserBaseURL: strings.TrimSpace(*browser)}
	rep := report.Report{
		Meta: report.BuildMeta(ctx, targets),
	}
	readyErr := waitServiceReady(ctx, targets.BrowserBaseURL, "/healthz", "browser.not_ready")
	if readyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("browser_navigate", readyErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	rep.Results = append(rep.Results, scenarios.RunBrowserNavigate(ctx, scenarios.DefaultBrowserNavigateConfig(targets.BrowserBaseURL)))
	rep.OverallPass = rep.Results[0].Pass
	writeReportAndExit(rep, *out)
}

func runOpenViking(args []string) {
	fs := flag.NewFlagSet("openviking", flag.ExitOnError)
	_, _, _, openviking, _, _, _, out := commonFlags(fs)
	rootKey := fs.String("openviking-root-key", "", "openviking root api key")
	fs.Parse(args)

	key := strings.TrimSpace(*rootKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv(envOpenVikingRootKey))
	}

	ctx := context.Background()
	targets := report.Targets{OpenVikingBaseURL: strings.TrimSpace(*openviking)}
	rep := report.Report{
		Meta: report.BuildMeta(ctx, targets),
	}
	readyErr := waitServiceReady(ctx, targets.OpenVikingBaseURL, "/health", "openviking.not_ready")
	if readyErr != "" {
		rep.Results = append(rep.Results, tokenRequiredResult("openviking_find", readyErr))
		rep.OverallPass = false
		writeReportAndExit(rep, *out)
	}

	rep.Results = append(rep.Results, scenarios.RunOpenVikingFind(ctx, scenarios.DefaultOpenVikingFindConfig(targets.OpenVikingBaseURL, key)))
	rep.OverallPass = rep.Results[0].Pass
	writeReportAndExit(rep, *out)
}

func tokenRequiredResult(name string, errCode string) report.ScenarioResult {
	return report.ScenarioResult{
		Name:       name,
		Config:     map[string]any{},
		Stats:      map[string]any{},
		Thresholds: map[string]any{},
		Pass:       false,
		Errors:     []string{errCode},
	}
}

func writeReportAndExit(rep report.Report, outPath string) {
	data, err := report.Encode(rep)
	if err != nil {
		_, _ = os.Stderr.WriteString("encode error\n")
		os.Exit(1)
	}

	_, _ = os.Stdout.Write(data)
	_, _ = os.Stdout.WriteString("\n")

	if strings.TrimSpace(outPath) != "" {
		_ = report.WriteFile(strings.TrimSpace(outPath), data)
	}

	if !rep.OverallPass {
		os.Exit(1)
	}
}
