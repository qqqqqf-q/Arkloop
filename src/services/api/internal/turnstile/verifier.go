package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const siteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// TurnstileError is returned when siteverify rejects the token.
type TurnstileError struct {
	ErrorCodes []string
}

func (e TurnstileError) Error() string {
	if len(e.ErrorCodes) > 0 {
		return "turnstile: captcha invalid: " + strings.Join(e.ErrorCodes, ", ")
	}
	return "turnstile: captcha invalid"
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes"`
}

// VerifyRequest contains all inputs for a siteverify call.
type VerifyRequest struct {
	SecretKey   string
	Token       string
	RemoteIP    string // optional, forwarded to Cloudflare for risk scoring
	AllowedHost string // optional, if set the response hostname must match
}

// Verify calls the Cloudflare Turnstile siteverify API.
// Returns TurnstileError when the token is invalid or expired.
// Returns nil when SecretKey is empty (allows skipping in dev).
func Verify(ctx context.Context, client *http.Client, req VerifyRequest) error {
	if req.SecretKey == "" {
		return nil
	}
	if req.Token == "" {
		return TurnstileError{ErrorCodes: []string{"missing-input-response"}}
	}

	form := url.Values{
		"secret":   {req.SecretKey},
		"response": {req.Token},
	}
	if req.RemoteIP != "" {
		form.Set("remoteip", req.RemoteIP)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, siteverifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("turnstile: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("turnstile: siteverify: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result siteverifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("turnstile: decode response: %w", err)
	}

	if !result.Success {
		return TurnstileError{ErrorCodes: result.ErrorCodes}
	}

	if req.AllowedHost != "" && result.Hostname != req.AllowedHost {
		return TurnstileError{ErrorCodes: []string{"hostname-mismatch"}}
	}

	return nil
}
