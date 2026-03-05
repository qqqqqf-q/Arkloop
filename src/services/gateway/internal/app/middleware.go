package app

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"arkloop/services/gateway/internal/accesslog"
	"arkloop/services/gateway/internal/clientip"
	"arkloop/services/gateway/internal/geoip"
	"arkloop/services/gateway/internal/identity"
	"arkloop/services/gateway/internal/ua"

	goredis "github.com/redis/go-redis/v9"
)

const traceIDHeader = "X-Trace-Id"

type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(payload []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(payload)
}

// Flush forwards to the underlying ResponseWriter; required for SSE/streaming.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func traceMiddleware(next http.Handler, logger *JSONLogger, geo geoip.Lookup, rdb *goredis.Client, redisTimeout time.Duration, jwtSecret []byte) http.Handler {
	var logWriter *accesslog.Writer
	if rdb != nil {
		logWriter = accesslog.NewWriter(rdb)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		traceID := newTraceID()
		if incoming := normalizeTraceID(r.Header.Get(traceIDHeader)); incoming != "" {
			traceID = incoming
		}

		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		recorder.Header().Set(traceIDHeader, traceID)

		next.ServeHTTP(recorder, r)

		// clientip 中间件在 next.ServeHTTP 之前已写入 context
		ip := clientip.FromContext(r.Context())
		uaInfo := ua.Parse(r)
		durationMs := time.Since(start).Milliseconds()

		var geoResult geoip.Result
		if geo != nil && ip != "" {
			geoResult = geo.LookupIP(ip)
		}

		riskScore := 0
		if s := r.Header.Get("X-Risk-Score"); s != "" {
			riskScore, _ = strconv.Atoi(s)
		}

		// 身份提取
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		identCtx := r.Context()
		cancel := func() {}
		if rdb != nil && redisTimeout > 0 {
			if bearer, ok := strings.CutPrefix(auth, "Bearer "); ok && strings.HasPrefix(bearer, "ak-") {
				identCtx, cancel = context.WithTimeout(identCtx, redisTimeout)
			}
		}
		ident := identity.ExtractInfo(identCtx, auth, rdb, jwtSecret)
		cancel()

		// 结构化日志
		if logger != nil {
			extra := map[string]any{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status_code": recorder.statusCode,
				"duration_ms": durationMs,
				"client_ip":   ip,
				"user_agent":  uaInfo.Raw,
				"ua_type":     string(uaInfo.Type),
			}
			if geoResult.Country != "" {
				extra["country"] = geoResult.Country
			}
			if riskScore > 0 {
				extra["risk_score"] = riskScore
			}
			tid := traceID
			logger.Info("request", LogFields{TraceID: &tid}, extra)
		}

		// 访问日志写入 Redis Stream（尽力而为，不阻塞请求）。
		if logWriter != nil {
			logWriter.Write(accesslog.Entry{
				Timestamp:    start.UTC().Format(time.RFC3339),
				TraceID:      traceID,
				Method:       r.Method,
				Path:         r.URL.Path,
				StatusCode:   recorder.statusCode,
				DurationMs:   durationMs,
				ClientIP:     ip,
				Country:      geoResult.Country,
				City:         geoResult.City,
				UserAgent:    uaInfo.Raw,
				UAType:       string(uaInfo.Type),
				RiskScore:    riskScore,
				IdentityType: string(ident.Type),
				OrgID:        ident.OrgID,
				UserID:       ident.UserID,
			})
		}
	})
}

func recoverMiddleware(next http.Handler, logger *JSONLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				// 复用 traceMiddleware 已设置在 response header 上的 traceID
				traceID := w.Header().Get(traceIDHeader)
				if traceID == "" {
					traceID = newTraceID()
				}
				if logger != nil {
					logger.Error("panic", LogFields{TraceID: &traceID}, map[string]any{
						"panic": fmt.Sprint(value),
						"stack": string(debug.Stack()),
					})
				}
				http.Error(w, `{"code":"internal.error","message":"internal error"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newTraceID() string {
	hi := nextTrace64()
	lo := nextTrace64()

	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], hi)
	binary.BigEndian.PutUint64(buf[8:], lo)
	return hex.EncodeToString(buf[:])
}

func normalizeTraceID(value string) string {
	candidate := strings.TrimSpace(value)
	if len(candidate) != 32 {
		return ""
	}
	for i := 0; i < len(candidate); i++ {
		ch := candidate[i]
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return ""
	}
	return strings.ToLower(candidate)
}

var traceState uint64 = initTraceSeed()

func initTraceSeed() uint64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(buf[:])
}

func nextTrace64() uint64 {
	x := atomic.AddUint64(&traceState, 0x9e3779b97f4a7c15)
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
