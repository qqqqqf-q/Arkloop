package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"arkloop/services/gateway/internal/clientip"
	"arkloop/services/gateway/internal/geoip"
	"arkloop/services/gateway/internal/identity"
	"arkloop/services/gateway/internal/ipfilter"
	"arkloop/services/gateway/internal/proxy"
	"arkloop/services/gateway/internal/ratelimit"
	"arkloop/services/gateway/internal/risk"
	"arkloop/services/gateway/internal/ua"
	sharedredis "arkloop/services/shared/redis"

	goredis "github.com/redis/go-redis/v9"
)

const gatewayConfigRedisKey = "arkloop:gateway:config"

// gatewayDynamicConfig 是可以从 Redis 动态加载的配置，覆盖 env 默认值。
type gatewayDynamicConfig struct {
	IPMode              string   `json:"ip_mode,omitempty"`
	TrustedCIDRs        []string `json:"trusted_cidrs,omitempty"`
	RiskRejectThreshold int      `json:"risk_reject_threshold,omitempty"`
	RateLimitCapacity   float64  `json:"rate_limit_capacity,omitempty"`
	RateLimitPerMinute  float64  `json:"rate_limit_per_minute,omitempty"`
}

type Application struct {
	config Config
	logger *JSONLogger
	// dynamicCfg 原子指针，30s 轮询刷新
	dynamicCfg unsafe.Pointer
}

func NewApplication(config Config, logger *JSONLogger) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	app := &Application{config: config, logger: logger}
	// 初始化为空配置（不覆盖 env 值）
	empty := &gatewayDynamicConfig{}
	atomic.StorePointer(&app.dynamicCfg, unsafe.Pointer(empty))
	return app, nil
}

func (a *Application) loadDynamicConfig(ctx context.Context, rdb *goredis.Client) {
	if rdb == nil {
		return
	}
	raw, err := rdb.Get(ctx, gatewayConfigRedisKey).Bytes()
	if err != nil {
		return
	}
	var cfg gatewayDynamicConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return
	}
	atomic.StorePointer(&a.dynamicCfg, unsafe.Pointer(&cfg))
}

func (a *Application) getDynamicConfig() *gatewayDynamicConfig {
	return (*gatewayDynamicConfig)(atomic.LoadPointer(&a.dynamicCfg))
}

// effectiveIPMode 动态配置优先，env 兜底。
func (a *Application) effectiveIPMode() IPMode {
	if dyn := a.getDynamicConfig(); dyn.IPMode != "" {
		return IPMode(dyn.IPMode)
	}
	if a.config.IPMode != "" {
		return a.config.IPMode
	}
	return IPModeDirect
}

func (a *Application) effectiveTrustedCIDRs() []string {
	if dyn := a.getDynamicConfig(); len(dyn.TrustedCIDRs) > 0 {
		return dyn.TrustedCIDRs
	}
	return a.config.TrustedCIDRs
}

func (a *Application) effectiveRiskThreshold() int {
	if dyn := a.getDynamicConfig(); dyn.RiskRejectThreshold > 0 {
		return dyn.RiskRejectThreshold
	}
	return a.config.RiskRejectThreshold
}

func (a *Application) effectiveRateLimit() ratelimit.Config {
	cfg := a.config.RateLimit
	dyn := a.getDynamicConfig()
	if dyn.RateLimitCapacity > 0 {
		cfg.Capacity = dyn.RateLimitCapacity
	}
	if dyn.RateLimitPerMinute > 0 {
		cfg.RatePerMinute = dyn.RateLimitPerMinute
	}
	return cfg
}

func (a *Application) buildResolver() clientip.Resolver {
	mode := a.effectiveIPMode()
	cidrs := clientip.ParseCIDRList(a.effectiveTrustedCIDRs())

	switch mode {
	case IPModeCloudflare:
		return &clientip.Cloudflare{TrustedCIDRs: cidrs}
	case IPModeTrustedProxy:
		return &clientip.TrustedProxy{TrustedCIDRs: cidrs}
	default:
		return clientip.Direct{}
	}
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p, err := proxy.New(proxy.Config{Upstream: a.config.Upstream})
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}

	// GeoIP 初始化
	var geo geoip.Lookup = geoip.Noop{}
	if a.config.GeoIPLicenseKey != "" {
		updater := geoip.NewUpdater(a.config.GeoIPDBPath, a.config.GeoIPLicenseKey, &geoipLogAdapter{logger: a.logger})
		if err := updater.Init(); err != nil {
			a.logger.Error("geoip init failed", LogFields{}, map[string]any{"error": err.Error()})
		} else {
			defer updater.Close()
			geo = updater
			a.logger.Info("geoip enabled (auto-update)", LogFields{}, map[string]any{"path": a.config.GeoIPDBPath})
			go updater.Run(ctx)
		}
	} else if a.config.GeoIPDBPath != "" {
		mm, err := geoip.NewMaxMind(a.config.GeoIPDBPath)
		if err != nil {
			a.logger.Error("geoip init failed", LogFields{}, map[string]any{"path": a.config.GeoIPDBPath, "error": err.Error()})
		} else {
			defer mm.Close()
			geo = mm
			a.logger.Info("geoip enabled", LogFields{}, map[string]any{"path": a.config.GeoIPDBPath})
		}
	}

	mux := http.NewServeMux()
	if a.config.EnableBenchz {
		mux.HandleFunc("/benchz", healthz)
	}
	mux.Handle("/", p)

	var (
		limiter  ratelimit.Limiter
		ipFilter *ipfilter.Filter
		rdb      *goredis.Client
	)
	if strings.TrimSpace(a.config.RedisURL) != "" {
		var redisErr error
		rdb, redisErr = sharedredis.NewClient(ctx, a.config.RedisURL)
		if redisErr != nil {
			return fmt.Errorf("redis: %w", redisErr)
		}
		defer rdb.Close()

		// 启动时加载动态配置
		a.loadDynamicConfig(ctx, rdb)

		bucket, err := ratelimit.NewTokenBucketWithProvider(rdb, a.config.RateLimit, a.effectiveRateLimit)
		if err != nil {
			return fmt.Errorf("ratelimit: %w", err)
		}
		limiter = bucket
		ipFilter = ipfilter.NewFilter(rdb, a.config.RedisTimeout)

		effectiveRL := a.effectiveRateLimit()
		a.logger.Info("ratelimit enabled", LogFields{}, map[string]any{
			"capacity":        effectiveRL.Capacity,
			"rate_per_minute": effectiveRL.RatePerMinute,
		})
		a.logger.Info("ipfilter enabled", LogFields{}, nil)

		// 后台 30s 轮询动态配置
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					a.loadDynamicConfig(ctx, rdb)
				}
			}
		}()
	}

	// 风险评分器（阈值在运行时通过 effectiveRiskThreshold 动态读取）
	scorer := &risk.Scorer{}

	// 构建请求处理链
	inner := http.Handler(mux)

	// risk 中间件：评分 + 可配置拒绝 + 透传 header
	inner = a.riskMiddleware(inner, geo, scorer, rdb)

	if limiter != nil {
		inner = ratelimit.NewRateLimitMiddleware(inner, limiter, a.config.JWTSecret, a.config.RedisTimeout, rdb)
	}
	if ipFilter != nil {
		inner = ipFilter.Middleware(inner)
	}

	// trace 在 clientip 内层，可以从 context 读到 IP
	inner = traceMiddleware(inner, a.logger, geo, rdb, a.config.RedisTimeout)

	// clientip 中间件：最外层（recover 之后），将真实 IP 写入 context
	inner = clientip.Middleware(a.buildResolver(), inner)

	handler := recoverMiddleware(inner, a.logger)
	root := http.NewServeMux()
	root.HandleFunc("/healthz", healthz)
	root.Handle("/", handler)

	listener, err := net.Listen("tcp", a.config.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	a.logger.Info("gateway started", LogFields{}, map[string]any{
		"addr":     a.config.Addr,
		"upstream": a.config.Upstream,
		"ip_mode":  string(a.effectiveIPMode()),
	})

	server := &http.Server{
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}

	err = <-errCh
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// riskMiddleware 计算风险分，注入 X-Risk-Score / X-UA-Type / X-Client-Country header。
func (a *Application) riskMiddleware(next http.Handler, geo geoip.Lookup, scorer *risk.Scorer, rdb *goredis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientip.FromContext(r.Context())
		geoResult := geo.LookupIP(ip)
		uaInfo := ua.Parse(r)

		// 判断是否匿名请求
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		identCtx := r.Context()
		cancel := func() {}
		if rdb != nil && a.config.RedisTimeout > 0 {
			if bearer, ok := strings.CutPrefix(auth, "Bearer "); ok && strings.HasPrefix(bearer, "ak-") {
				identCtx, cancel = context.WithTimeout(identCtx, a.config.RedisTimeout)
			}
		}
		ident := identity.ExtractInfo(identCtx, auth, rdb)
		cancel()
		anonymous := ident.Type == identity.IdentityAnonymous

		// 动态读取当前阈值
		scorer.RejectThreshold = a.effectiveRiskThreshold()
		score := scorer.Evaluate(r, geoResult, uaInfo, anonymous)

		if scorer.ShouldReject(score) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"risk.rejected","message":"Request rejected by risk policy"}`))
			return
		}

		// 透传给上游
		r.Header.Set("X-Risk-Score", fmt.Sprintf("%d", score.Value))
		r.Header.Set("X-UA-Type", string(uaInfo.Type))
		if geoResult.Country != "" {
			r.Header.Set("X-Client-Country", geoResult.Country)
		}

		next.ServeHTTP(w, r)
	})
}

var healthzPayload = []byte(`{"status":"ok"}`)
var healthzContentLength = strconv.Itoa(len(healthzPayload))

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"code":"http.method_not_allowed","message":"Method Not Allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", healthzContentLength)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(healthzPayload)
}

// geoipLogAdapter 将 JSONLogger 适配为 geoip.Logger 接口。
type geoipLogAdapter struct {
	logger *JSONLogger
}

func (a *geoipLogAdapter) Info(msg string, extra map[string]any) {
	a.logger.Info(msg, LogFields{}, extra)
}

func (a *geoipLogAdapter) Error(msg string, extra map[string]any) {
	a.logger.Error(msg, LogFields{}, extra)
}
