package app

import (
	"net/http"
	"strings"
)

func routeRequest(apiProxy http.Handler, frontendProxy http.Handler) http.Handler {
	if frontendProxy == nil {
		return apiProxy
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldProxyToAPI(r.URL.Path) {
			apiProxy.ServeHTTP(w, r)
			return
		}
		frontendProxy.ServeHTTP(w, r)
	})
}

func shouldProxyToAPI(path string) bool {
	return path == "/v1" || strings.HasPrefix(path, "/v1/")
}
