package mcp

import (
	"net/http"
	"net/url"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	sharedoutbound "arkloop/services/shared/outboundurl"
)

func newSafeHTTPClient() *http.Client {
	return sharedmcpinstall.NewSafeHTTPClient()
}

func validateURL(u *url.URL, policy sharedoutbound.Policy) error {
	return sharedmcpinstall.ValidateURL(u, policy)
}
