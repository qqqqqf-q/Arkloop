package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func DiscoverServerInfo(ctx context.Context, httpClient HTTPDoer, serverURL string, resourceMetadataURL string) (DiscoveryState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	parsedServerURL, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || parsedServerURL.Scheme == "" || parsedServerURL.Host == "" {
		return DiscoveryState{}, fmt.Errorf("mcp oauth: invalid server url")
	}

	state := DiscoveryState{ServerURL: parsedServerURL.String()}
	resourceMetadata, foundURL, err := discoverProtectedResourceMetadata(ctx, httpClient, parsedServerURL, resourceMetadataURL)
	if err != nil {
		return DiscoveryState{}, err
	}
	if resourceMetadata != nil {
		state.ResourceMetadata = resourceMetadata
		state.ResourceMetadataURL = foundURL
	}

	authorizationServerURL := firstAuthorizationServer(resourceMetadata)
	if authorizationServerURL == "" {
		authorizationServerURL = (&url.URL{Scheme: parsedServerURL.Scheme, Host: parsedServerURL.Host, Path: "/"}).String()
	}
	state.AuthorizationServerURL = authorizationServerURL

	authMetadata, err := discoverAuthorizationServerMetadata(ctx, httpClient, authorizationServerURL)
	if err != nil {
		return DiscoveryState{}, err
	}
	state.AuthorizationServerMetadata = authMetadata
	return state, nil
}

func discoverProtectedResourceMetadata(ctx context.Context, httpClient HTTPDoer, serverURL *url.URL, resourceMetadataURL string) (*ProtectedResourceMetadata, string, error) {
	candidates, err := protectedResourceMetadataURLs(serverURL, resourceMetadataURL)
	if err != nil {
		return nil, "", err
	}
	for _, candidate := range candidates {
		var metadata ProtectedResourceMetadata
		ok, err := fetchJSONMetadata(ctx, httpClient, candidate, &metadata)
		if err != nil {
			return nil, "", err
		}
		if ok {
			return &metadata, candidate, nil
		}
	}
	return nil, "", nil
}

func protectedResourceMetadataURLs(serverURL *url.URL, resourceMetadataURL string) ([]string, error) {
	candidates := make([]string, 0, 3)
	if strings.TrimSpace(resourceMetadataURL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(resourceMetadataURL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("mcp oauth: invalid resource metadata url")
		}
		candidates = append(candidates, parsed.String())
	}

	path := strings.TrimRight(serverURL.Path, "/")
	if path != "" && path != "/" {
		candidate := *serverURL
		candidate.RawQuery = serverURL.RawQuery
		candidate.Fragment = ""
		candidate.Path = "/.well-known/oauth-protected-resource" + path
		candidate.RawPath = ""
		candidates = append(candidates, candidate.String())
	}

	root := *serverURL
	root.Path = "/.well-known/oauth-protected-resource"
	root.RawPath = ""
	root.RawQuery = ""
	root.Fragment = ""
	candidates = append(candidates, root.String())
	return dedupeStrings(candidates), nil
}

func discoverAuthorizationServerMetadata(ctx context.Context, httpClient HTTPDoer, authorizationServerURL string) (*AuthorizationServerMetadata, error) {
	candidates, err := authorizationServerMetadataURLs(authorizationServerURL)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		var metadata AuthorizationServerMetadata
		ok, err := fetchJSONMetadata(ctx, httpClient, candidate, &metadata)
		if err != nil {
			return nil, err
		}
		if ok {
			return &metadata, nil
		}
	}
	return nil, nil
}

func authorizationServerMetadataURLs(authorizationServerURL string) ([]string, error) {
	parsed, err := url.Parse(strings.TrimSpace(authorizationServerURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("mcp oauth: invalid authorization server url")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if path == "" || path == "/" {
		return []string{
			parsed.ResolveReference(&url.URL{Path: "/.well-known/oauth-authorization-server"}).String(),
			parsed.ResolveReference(&url.URL{Path: "/.well-known/openid-configuration"}).String(),
		}, nil
	}
	return []string{
		parsed.ResolveReference(&url.URL{Path: "/.well-known/oauth-authorization-server" + path}).String(),
		parsed.ResolveReference(&url.URL{Path: "/.well-known/openid-configuration" + path}).String(),
		parsed.ResolveReference(&url.URL{Path: path + "/.well-known/openid-configuration"}).String(),
	}, nil
}

func fetchJSONMetadata(ctx context.Context, httpClient HTTPDoer, endpoint string, target any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", LatestProtocolVersion)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if !respOK(resp.StatusCode) {
		_, _ = io.Copy(io.Discard, resp.Body)
		if (resp.StatusCode >= 400 && resp.StatusCode < 500) || resp.StatusCode == http.StatusBadGateway {
			return false, nil
		}
		return false, fmt.Errorf("mcp oauth: metadata request failed with status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return false, fmt.Errorf("mcp oauth: decode metadata: %w", err)
	}
	return true, nil
}

func firstAuthorizationServer(metadata *ProtectedResourceMetadata) string {
	if metadata == nil {
		return ""
	}
	for _, candidate := range metadata.AuthorizationServers {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func respOK(status int) bool {
	return status >= 200 && status < 300
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
