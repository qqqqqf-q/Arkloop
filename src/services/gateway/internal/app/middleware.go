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

const (
	traceIDHeader          = "X-Trace-Id"
	cspHeaderValue         = "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'; object-src 'none'"
	frontendCSPHeaderValue = "default-src 'self'; base-uri 'self'; frame-ancestors 'self'; form-action 'self'; object-src 'none'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://challenges.cloudflare.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' https://challenges.cloudflare.com http://localhost:8003 http://127.0.0.1:8003 http://[::1]:8003; frame-src https://challenges.cloudflare.com"
	corsAllowMethodsValue  = "GET,POST,PUT,PATCH,DELETE,OPTIONS"
	corsAllowHeadersValue  = "Authorization,Content-Type,Accept,X-Client-App,X-Trace-Id"
	corsExposeHeadersValue = traceIDHeader
)

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

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func traceMiddleware(next http.Handler, logger *JSONLogger, geo geoip.Lookup, rdb *goredis.Client, redisTimeout time.Duration, jwtSecret []byte, trustIncomingTraceID bool) http.Handler {
	var logWriter *accesslog.Writer
	if rdb != nil {
		logWriter = accesslog.NewWriter(rdb)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		traceID := newTraceID()
		if trustIncomingTraceID {
			if incoming := normalizeTraceID(r.Header.Get(traceIDHeader)); incoming != "" {
				traceID = incoming
			}
		}
		r.Header.Set(traceIDHeader, traceID)

		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		recorder.Header().Set(traceIDHeader, traceID)

		next.ServeHTTP(recorder, r)

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

func securityHeadersMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("Content-Security-Policy", cspHeaderValue)
		} else {
			w.Header().Set("Content-Security-Policy", frontendCSPHeaderValue)
		}

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			appendVaryHeader(w.Header(), "Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", corsAllowMethodsValue)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeadersValue)
				w.Header().Set("Access-Control-Expose-Headers", corsExposeHeadersValue)
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func appendVaryHeader(header http.Header, value string) {
	if header == nil || value == "" {
		return
	}
	current := header.Values("Vary")
	for _, item := range current {
		for _, part := range strings.Split(item, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func recoverMiddleware(next http.Handler, logger *JSONLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
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

func limitRequestBodyMiddleware(maxBytes int64, next http.Handler) http.Handler {
	if maxBytes <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBytes {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(`{"code":"http.request_too_large","message":"request body too large"}`))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
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
