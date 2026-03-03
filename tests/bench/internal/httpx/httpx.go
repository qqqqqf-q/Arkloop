package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ErrorEnvelope struct {
	Code string `json:"code"`
}

type HTTPError struct {
	Status int
	Code   string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code != "" {
		return fmt.Sprintf("http status=%d code=%s", e.Status, e.Code)
	}
	return fmt.Sprintf("http status=%d", e.Status)
}

func NewClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 1024,
		IdleConnTimeout:     90 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func NewNoTimeoutClient() *http.Client {
	return NewNoTimeoutClientWithHeaderTimeout(0)
}

func NewNoTimeoutClientWithHeaderTimeout(headerTimeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          1024,
		MaxIdleConnsPerHost:   1024,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
	}
	return &http.Client{
		Transport: transport,
	}
}

func JoinURL(base string, path string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("base url is empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return u.String(), nil
	}
	ref, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

func DoJSON(ctx context.Context, client *http.Client, method string, url string, headers map[string]string, body any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = NewClient(2 * time.Second)
	}

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		code := ""
		var env ErrorEnvelope
		if json.Unmarshal(raw, &env) == nil {
			code = strings.TrimSpace(env.Code)
		}
		return &HTTPError{Status: resp.StatusCode, Code: code}
	}

	if out != nil {
		if len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}
