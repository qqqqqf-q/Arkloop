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
	"arkloop/tests/bench/internal/seed"
)

const (
	envAccessToken   = "ARKLOOP_BENCH_ACCESS_TOKEN"
	envDatabaseURL   = "DATABASE_URL"
	envWorkerPersona = "ARKLOOP_BENCH_WORKER_PERSONA"
)

func main() {
	if len(os.Args) < 2 {
		_, _ = os.Stderr.WriteString("usage: bench <baseline|gateway|api-crud|sse|worker>\n")
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
	default:
		_, _ = os.Stderr.WriteString("unknown command\n")
		os.Exit(2)
	}
}

func commonFlags(fs *flag.FlagSet) (gateway, api, accessToken, dbDSN *string, forceOpen *bool, out *string) {
	gateway = fs.String("gateway", "http://127.0.0.1:8005", "gateway base url")
	api = fs.String("api", "http://127.0.0.1:8006", "api base url")
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

func resolveWorkerPersona(flagValue string) string {
	cleaned := strings.TrimSpace(flagValue)
	if cleaned != "" {
		return cleaned
	}
	cleaned = strings.TrimSpace(os.Getenv(envWorkerPersona))
	if cleaned != "" {
		return cleaned
	}
	return "normal"
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
	gateway, api, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
	workerPersona := fs.String("worker-persona", "", "worker scenario persona id")
	fs.Parse(args)

	ctx := context.Background()
	effectiveDBDSN := resolveDBDSN(*dbDSN)

	targets := report.Targets{
		GatewayBaseURL: strings.TrimSpace(*gateway),
		APIBaseURL:     strings.TrimSpace(*api),
	}
	rep := report.Report{
		Meta: report.BuildMeta(ctx, targets),
	}

	gatewayReadyErr := waitServiceReady(ctx, targets.GatewayBaseURL, "/healthz", "gateway.not_ready")
	apiReadyErr := waitServiceReady(ctx, targets.APIBaseURL, "/healthz", "api.not_ready")

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
			workerCfg.PersonaID = resolveWorkerPersona(*workerPersona)
			rep.Results = append(rep.Results, scenarios.RunWorkerRuns(ctx, workerCfg))
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
	gateway, _, _, _, _, out := commonFlags(fs)
	jwtSecret := fs.String("jwt-secret", "", "JWT signing secret (enables gateway_jwt scenario)")
	redisURL := fs.String("redis-url", "", "gateway Redis URL (enables gateway_apikey scenario)")
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

	// 无认证场景（始终运行）
	rep.Results = append(rep.Results, scenarios.RunGatewayRatelimit(ctx, scenarios.DefaultGatewayConfig(targets.GatewayBaseURL)))

	secret := strings.TrimSpace(*jwtSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("ARKLOOP_AUTH_JWT_SECRET"))
	}

	// JWT 认证场景
	if secret != "" {
		const benchAccountID = "00000000-0000-4000-8000-000000000001"
		const benchUserID = "00000000-0000-4000-8000-000000000002"
		token, err := seed.MakeJWT(secret, benchAccountID, benchUserID, 10*time.Minute)
		if err != nil {
			rep.Results = append(rep.Results, tokenRequiredResult("gateway_jwt", "jwt.sign.error"))
		} else {
			rep.Results = append(rep.Results, scenarios.RunGatewayAuth(ctx, scenarios.DefaultGatewayJWTConfig(targets.GatewayBaseURL, token)))
		}
	}

	// API Key 认证场景
	rURL := strings.TrimSpace(*redisURL)
	if rURL == "" {
		rURL = strings.TrimSpace(os.Getenv("ARKLOOP_GATEWAY_REDIS_URL"))
	}
	if rURL != "" && secret != "" {
		const benchAPIKey = "ak-bench-00000000000000000000000000000001"
		const benchAccountID = "00000000-0000-4000-8000-000000000001"

		rdb, err := seed.ConnectRedis(ctx, rURL)
		if err != nil {
			rep.Results = append(rep.Results, tokenRequiredResult("gateway_apikey", "redis.connect.error"))
		} else {
			defer rdb.Close()
			if err := seed.SeedAPIKey(ctx, rdb, benchAPIKey, benchAccountID); err != nil {
				rep.Results = append(rep.Results, tokenRequiredResult("gateway_apikey", "redis.seed.error"))
			} else {
				defer seed.CleanupAPIKey(ctx, rdb, benchAPIKey)
				rep.Results = append(rep.Results, scenarios.RunGatewayAuth(ctx, scenarios.DefaultGatewayAPIKeyConfig(targets.GatewayBaseURL, benchAPIKey)))
			}
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

func runAPICRUD(args []string) {
	fs := flag.NewFlagSet("api-crud", flag.ExitOnError)
	_, api, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
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
	_, api, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
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
	_, api, accessToken, dbDSN, forceOpen, out := commonFlags(fs)
	workerPersona := fs.String("worker-persona", "", "worker scenario persona id")
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
	cfg.PersonaID = resolveWorkerPersona(*workerPersona)
	rep.Results = append(rep.Results, scenarios.RunWorkerRuns(ctx, cfg))
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
