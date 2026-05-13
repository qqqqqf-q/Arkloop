package mcpoauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDiscoverServerInfoUsesProtectedResourceAndPathAwareASMetadata(t *testing.T) {
	requests := []string{}
	baseURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/explicit-prm":
			http.NotFound(w, r)
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(ProtectedResourceMetadata{
				Resource:             baseURL,
				AuthorizationServers: []string{baseURL + "/tenant1"},
				ScopesSupported:      []string{"read"},
			})
		case "/.well-known/oauth-authorization-server/tenant1":
			_ = json.NewEncoder(w).Encode(AuthorizationServerMetadata{
				Issuer:                        baseURL + "/tenant1",
				AuthorizationEndpoint:         baseURL + "/authorize",
				TokenEndpoint:                 baseURL + "/token",
				ResponseTypesSupported:        []string{"code"},
				CodeChallengeMethodsSupported: []string{"S256"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	state, err := DiscoverServerInfo(context.Background(), server.Client(), server.URL+"/mcp", server.URL+"/explicit-prm")
	if err != nil {
		t.Fatalf("discover server info: %v", err)
	}
	if state.ResourceMetadataURL != server.URL+"/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("unexpected resource metadata url: %q", state.ResourceMetadataURL)
	}
	if state.AuthorizationServerURL != server.URL+"/tenant1" {
		t.Fatalf("unexpected authorization server url: %q", state.AuthorizationServerURL)
	}
	if state.AuthorizationServerMetadata == nil || state.AuthorizationServerMetadata.TokenEndpoint != server.URL+"/token" {
		t.Fatalf("authorization server metadata not loaded: %#v", state.AuthorizationServerMetadata)
	}
	want := []string{
		"/explicit-prm",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server/tenant1",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected discovery order:\n%s", strings.Join(requests, "\n"))
	}
}

func TestStartAuthorizationBuildsPKCES256URL(t *testing.T) {
	authState, err := StartAuthorization(DiscoveryState{
		AuthorizationServerURL: "https://auth.example.test",
		ResourceMetadata:       &ProtectedResourceMetadata{Resource: "https://resource.example.test/mcp"},
		AuthorizationServerMetadata: &AuthorizationServerMetadata{
			AuthorizationEndpoint:         "https://auth.example.test/authorize",
			ResponseTypesSupported:        []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
	}, ClientInformation{ClientID: "client-1"}, "https://app.example.test/callback", "read offline_access", "state-1")
	if err != nil {
		t.Fatalf("start authorization: %v", err)
	}
	parsed := mustParseURL(t, authState.AuthorizationURL)
	query := parsed.Query()
	if query.Get("client_id") != "client-1" || query.Get("resource") != "https://resource.example.test/mcp" {
		t.Fatalf("authorization url missing required params: %s", authState.AuthorizationURL)
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected challenge method: %q", query.Get("code_challenge_method"))
	}
	hash := sha256.Sum256([]byte(authState.CodeVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(hash[:])
	if query.Get("code_challenge") != wantChallenge {
		t.Fatalf("pkce challenge mismatch")
	}
	if query.Get("prompt") != "consent" {
		t.Fatalf("offline_access should request consent prompt")
	}
}

func TestExchangeAndRefreshUseClientAuthAndResource(t *testing.T) {
	var seenAuthorization string
	var seenResource string
	var seenGrant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		seenAuthorization = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		seenResource = r.PostForm.Get("resource")
		seenGrant = r.PostForm.Get("grant_type")
		_ = json.NewEncoder(w).Encode(Tokens{AccessToken: "access", ExpiresIn: 3600})
	}))
	defer server.Close()

	discovery := DiscoveryState{
		AuthorizationServerURL: server.URL,
		ResourceMetadata:       &ProtectedResourceMetadata{Resource: server.URL + "/mcp"},
		AuthorizationServerMetadata: &AuthorizationServerMetadata{
			TokenEndpoint:                     server.URL + "/token",
			TokenEndpointAuthMethodsSupported: []string{ClientAuthBasic},
		},
	}
	client := ClientInformation{ClientID: "id", ClientSecret: "secret"}
	tokens, err := ExchangeAuthorization(context.Background(), server.Client(), discovery, client, "code", "verifier", "https://app.example.test/callback")
	if err != nil {
		t.Fatalf("exchange authorization: %v", err)
	}
	if tokens.AccessToken != "access" || tokens.ExpiresAt.IsZero() {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}
	if seenAuthorization != "Basic "+base64.StdEncoding.EncodeToString([]byte("id:secret")) {
		t.Fatalf("unexpected client auth: %q", seenAuthorization)
	}
	if seenResource != server.URL+"/mcp" || seenGrant != "authorization_code" {
		t.Fatalf("unexpected token request resource/grant: %q %q", seenResource, seenGrant)
	}

	refreshed, err := RefreshAuthorization(context.Background(), server.Client(), discovery, client, "refresh-1")
	if err != nil {
		t.Fatalf("refresh authorization: %v", err)
	}
	if refreshed.RefreshToken != "refresh-1" || seenGrant != "refresh_token" {
		t.Fatalf("refresh token not preserved: %#v grant=%q", refreshed, seenGrant)
	}
}

func TestParseWWWAuthenticateParams(t *testing.T) {
	params := ParseWWWAuthenticateParams(`Bearer realm="mcp", resource_metadata="https://example.test/.well-known/oauth-protected-resource/mcp", scope="read write", error=invalid_token`)
	if params["resource_metadata"] != "https://example.test/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("unexpected resource metadata: %q", params["resource_metadata"])
	}
	if params["scope"] != "read write" || params["error"] != "invalid_token" {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestTokenExpiredSoon(t *testing.T) {
	tokens := Tokens{AccessToken: "access", ExpiresAt: time.Now().Add(30 * time.Second)}
	if !TokenExpiredSoon(tokens, time.Minute) {
		t.Fatalf("expected token to expire soon")
	}
	tokens.ExpiresAt = time.Now().Add(time.Hour)
	if TokenExpiredSoon(tokens, time.Minute) {
		t.Fatalf("expected token to remain valid")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return parsed
}
