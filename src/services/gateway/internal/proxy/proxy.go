package proxy

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"arkloop/services/gateway/internal/clientip"
)

// Config holds the reverse proxy configuration.
type Config struct {
	Upstream string
}

// Proxy is a stateless reverse proxy for the API service.
type Proxy struct {
	handler http.Handler
}

// New creates a reverse proxy targeting the given upstream URL.
// FlushInterval=-1 enables immediate flushing, required for SSE streams.
//
// X-Forwarded-For is handled by httputil.ReverseProxy.ServeHTTP automatically
// from req.RemoteAddr. We clear any client-provided XFF in the Director to
// prevent spoofing — ServeHTTP will then set it fresh.
func New(cfg Config) (*Proxy, error) {
	target, err := url.Parse(cfg.Upstream)
	if err != nil || strings.TrimSpace(target.Host) == "" {
		return nil, fmt.Errorf("invalid upstream url: %q", cfg.Upstream)
	}

	rp := &httputil.ReverseProxy{
		FlushInterval: -1,
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(`{"code":"http.request_too_large","message":"request body too large"}`))
			return
		}
		slog.Error("proxy error", "error", err)
		w.WriteHeader(http.StatusBadGateway)
	}
	rp.Rewrite = func(req *httputil.ProxyRequest) {
		req.SetURL(target)
		req.Out.Host = req.In.Host
		req.Out.Header.Del("X-Forwarded-For")
		req.SetXForwarded()

		if realIP := clientip.FromContext(req.In.Context()); realIP != "" {
			req.Out.Header.Set("X-Real-IP", realIP)
		}
	}

	return &Proxy{handler: rp}, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.handler.ServeHTTP(w, r)
}
