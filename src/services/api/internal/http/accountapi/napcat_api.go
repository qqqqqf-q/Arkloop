package accountapi

import (
	"encoding/json"
	nethttp "net/http"
	"os"
	"strings"
	"sync"

	"arkloop/services/api/internal/auth"
	"arkloop/services/shared/napcat"
)

var (
	napCatOnce    sync.Once
	napCatManager *napcat.Manager
)

func getOrCreateNapCatManager(dataDir string) *napcat.Manager {
	napCatOnce.Do(func() {
		napCatManager = napcat.NewManager(dataDir, nil)
	})
	return napCatManager
}

// NapCatDeps holds dependencies for NapCat API handlers.
type NapCatDeps struct {
	AuthService *auth.Service
	DataDir     string
}

// RegisterNapCatRoutes adds /v1/napcat/* endpoints to the mux.
func RegisterNapCatRoutes(mux *nethttp.ServeMux, deps NapCatDeps) {
	mgr := getOrCreateNapCatManager(deps.DataDir)

	mux.HandleFunc("GET /v1/napcat/status", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		writeNapCatJSON(w, nethttp.StatusOK, mgr.Status())
	}))

	mux.HandleFunc("POST /v1/napcat/download", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if err := mgr.Setup(); err != nil {
			writeNapCatJSON(w, nethttp.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeNapCatJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("POST /v1/napcat/start", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if err := mgr.Start(r.Context()); err != nil {
			writeNapCatJSON(w, nethttp.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeNapCatJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("POST /v1/napcat/stop", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if err := mgr.Stop(); err != nil {
			writeNapCatJSON(w, nethttp.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeNapCatJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("POST /v1/napcat/refresh-qr", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if err := mgr.RefreshQRCode(); err != nil {
			writeNapCatJSON(w, nethttp.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeNapCatJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("GET /v1/napcat/qrcode.png", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		path := mgr.QRCodeImagePath()
		data, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(nethttp.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(nethttp.StatusOK)
		w.Write(data)
	}))

	mux.HandleFunc("POST /v1/napcat/quick-login", napCatHandler(deps.AuthService, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var req struct {
			Uin string `json:"uin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Uin == "" {
			writeNapCatJSON(w, nethttp.StatusBadRequest, map[string]string{"error": "uin is required"})
			return
		}
		if err := mgr.QuickLogin(req.Uin); err != nil {
			writeNapCatJSON(w, nethttp.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeNapCatJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}))
}

func napCatHandler(authService *auth.Service, handler nethttp.HandlerFunc) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if authService != nil {
			token := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
			if _, err := authService.VerifyAccessTokenForActor(r.Context(), token); err != nil {
				w.WriteHeader(nethttp.StatusUnauthorized)
				return
			}
		}
		handler(w, r)
	}
}

func writeNapCatJSON(w nethttp.ResponseWriter, code int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(raw)
}
