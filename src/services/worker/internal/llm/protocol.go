package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

type ProtocolKind string

const (
	ProtocolKindOpenAIChatCompletions ProtocolKind = "openai_chat_completions"
	ProtocolKindOpenAIResponses       ProtocolKind = "openai_responses"
	ProtocolKindAnthropicMessages     ProtocolKind = "anthropic_messages"
	ProtocolKindGeminiGenerateContent ProtocolKind = "gemini_generate_content"
)

type TransportConfig struct {
	APIKey           string
	BaseURL          string
	DefaultHeaders   map[string]string
	EmitDebugEvents  bool
	TotalTimeout     time.Duration
	MaxResponseBytes int
}

type OpenAIProtocolConfig struct {
	PrimaryKind         ProtocolKind
	FallbackKind        *ProtocolKind
	AdvancedPayloadJSON map[string]any
}

type AnthropicProtocolConfig struct {
	Version             string
	ExtraHeaders        map[string]string
	AdvancedPayloadJSON map[string]any
}

type GeminiProtocolConfig struct {
	APIVersion          string
	AdvancedPayloadJSON map[string]any
}

type ResolvedGatewayConfig struct {
	ProtocolKind ProtocolKind
	Model        string
	Transport    TransportConfig
	OpenAI       *OpenAIProtocolConfig
	Anthropic    *AnthropicProtocolConfig
	Gemini       *GeminiProtocolConfig
}

type ProtocolAdapter interface {
	ProtocolKind() ProtocolKind
	Stream(ctx context.Context, request Request, yield func(StreamEvent) error) error
}

type protocolTransport struct {
	cfg        TransportConfig
	client     *http.Client
	baseURLErr error
}

type protocolConfigError struct {
	Message string
	Details map[string]any
}

var errStreamIdleTimeout = errors.New("llm stream idle timeout")

type streamActivityMarkerKey struct{}

func (e protocolConfigError) Error() string {
	return e.Message
}

func newProtocolTransport(cfg TransportConfig, defaultBaseURL string, normalize func(string) string) protocolTransport {
	timeout := cfg.TotalTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if normalize != nil {
		baseURL = normalize(baseURL)
	}

	normalizedBaseURL, baseURLErr := sharedoutbound.DefaultPolicy().NormalizeBaseURL(baseURL)
	if baseURLErr == nil {
		baseURL = normalizedBaseURL
	}

	cfg.BaseURL = baseURL
	cfg.TotalTimeout = timeout
	if cfg.DefaultHeaders == nil {
		cfg.DefaultHeaders = map[string]string{}
	}

	return protocolTransport{
		cfg:        cfg,
		client:     newProtocolHTTPClient(sharedoutbound.DefaultPolicy(), timeout),
		baseURLErr: baseURLErr,
	}
}

func newProtocolHTTPClient(policy sharedoutbound.Policy, responseHeaderTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = policy.SafeDialContext(&net.Dialer{Timeout: 10 * time.Second})
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	if proxyURL := strings.TrimSpace(policy.ProxyURL); proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{
		Timeout:       0,
		Transport:     protocolValidatingTransport{base: transport, policy: policy},
		CheckRedirect: policy.CheckRedirect,
	}
}

type protocolValidatingTransport struct {
	base   http.RoundTripper
	policy sharedoutbound.Policy
}

func (t protocolValidatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.policy.ValidateURL(req.URL); err != nil {
		return nil, err
	}
	if userAgent := strings.TrimSpace(t.policy.UserAgent); userAgent != "" && req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", userAgent)
	}
	var (
		resp *http.Response
		err  error
	)
	attempts := t.policy.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err = t.base.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
	}
	return nil, err
}

func (t protocolTransport) endpoint(path string) string {
	base := strings.TrimRight(strings.TrimSpace(t.cfg.BaseURL), "/")
	cleanPath := "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	if base == "" {
		return cleanPath
	}
	return base + cleanPath
}

func (t protocolTransport) applyDefaultHeaders(req *http.Request) {
	for key, value := range t.cfg.DefaultHeaders {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}

func normalizeAnthropicBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return "https://api.anthropic.com"
	}
	if strings.HasSuffix(trimmed, "/v1") {
		return strings.TrimSuffix(trimmed, "/v1")
	}
	return trimmed
}

func normalizeGeminiBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, version := range []string{"/v1beta", "/v1beta1", "/v1", "/v1alpha"} {
		if strings.HasSuffix(trimmed, version) {
			return strings.TrimSuffix(trimmed, version)
		}
	}
	return trimmed
}

func geminiAPIVersionFromBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, version := range []string{"v1beta", "v1beta1", "v1", "v1alpha"} {
		if strings.HasSuffix(trimmed, "/"+version) {
			return version
		}
	}
	return ""
}

func geminiVersionedPath(baseURL string, version string, resourcePath string) string {
	cleanResourcePath := "/" + strings.TrimLeft(strings.TrimSpace(resourcePath), "/")
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/v1beta") || strings.HasSuffix(base, "/v1beta1") || strings.HasSuffix(base, "/v1") || strings.HasSuffix(base, "/v1alpha") {
		return cleanResourcePath
	}
	return "/" + strings.Trim(strings.TrimSpace(version), "/") + cleanResourcePath
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func copyAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func withStreamIdleTimeout(parent context.Context, timeout time.Duration) (context.Context, func(), func()) {
	if timeout <= 0 {
		return parent, func() {}, func() {}
	}

	baseCtx, cancel := context.WithCancelCause(parent)
	activityCh := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)

		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for {
			select {
			case <-baseCtx.Done():
				return
			case <-timer.C:
				cancel(errStreamIdleTimeout)
				return
			case <-activityCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(timeout)
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel(context.Canceled)
			<-done
		})
	}
	markActivity := func() {
		select {
		case activityCh <- struct{}{}:
		default:
		}
	}
	ctx := context.WithValue(baseCtx, streamActivityMarkerKey{}, markActivity)
	return ctx, stop, markActivity
}

func streamContextError(ctx context.Context, fallback error) error {
	if ctx == nil {
		return fallback
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fallback
}

func streamActivityMarker(ctx context.Context) func() {
	if ctx == nil {
		return nil
	}
	mark, _ := ctx.Value(streamActivityMarkerKey{}).(func())
	return mark
}

func parseOpenAIProtocolConfig(apiMode string, raw map[string]any) (OpenAIProtocolConfig, error) {
	mode := strings.TrimSpace(apiMode)
	if mode == "" {
		mode = "auto"
	}

	cfg := OpenAIProtocolConfig{
		AdvancedPayloadJSON: map[string]any{},
	}
	switch mode {
	case "auto":
		cfg.PrimaryKind = ProtocolKindOpenAIResponses
		fallback := ProtocolKindOpenAIChatCompletions
		cfg.FallbackKind = &fallback
	case "responses":
		cfg.PrimaryKind = ProtocolKindOpenAIResponses
	case "chat_completions":
		cfg.PrimaryKind = ProtocolKindOpenAIChatCompletions
	default:
		return OpenAIProtocolConfig{}, protocolConfigError{Message: fmt.Sprintf("invalid openai_api_mode: %s", mode)}
	}

	for k, v := range raw {
		if _, denied := openAIAdvancedJSONDenylist[k]; denied {
			return OpenAIProtocolConfig{}, protocolConfigError{
				Message: fmt.Sprintf("advanced_json must not set critical field: %s", k),
				Details: map[string]any{"denied_key": k},
			}
		}
		cfg.AdvancedPayloadJSON[k] = v
	}

	return cfg, nil
}

func ResolveOpenAIProtocolConfig(apiMode string, raw map[string]any) (OpenAIProtocolConfig, error) {
	return parseOpenAIProtocolConfig(apiMode, raw)
}

func parseAnthropicProtocolConfig(raw map[string]any) (AnthropicProtocolConfig, error) {
	advancedCfg, err := parseAnthropicAdvancedJSON(raw)
	if err != nil {
		return AnthropicProtocolConfig{}, err
	}

	version := defaultAnthropicVersion
	if advancedCfg.Version != nil {
		version = *advancedCfg.Version
	}

	return AnthropicProtocolConfig{
		Version:             version,
		ExtraHeaders:        copyStringMap(advancedCfg.ExtraHeaders),
		AdvancedPayloadJSON: copyAnyMap(advancedCfg.Payload),
	}, nil
}

func ResolveAnthropicProtocolConfig(raw map[string]any) (AnthropicProtocolConfig, error) {
	return parseAnthropicProtocolConfig(raw)
}

func parseGeminiProtocolConfig(raw map[string]any) (GeminiProtocolConfig, error) {
	cfg := GeminiProtocolConfig{
		APIVersion:          "v1beta",
		AdvancedPayloadJSON: map[string]any{},
	}
	for k, v := range raw {
		if _, denied := geminiAdvancedJSONDenylist[k]; denied {
			return GeminiProtocolConfig{}, protocolConfigError{
				Message: fmt.Sprintf("advanced_json must not set critical field: %s", k),
				Details: map[string]any{"denied_key": k},
			}
		}
		cfg.AdvancedPayloadJSON[k] = v
	}
	return cfg, nil
}

func ResolveGeminiProtocolConfig(raw map[string]any) (GeminiProtocolConfig, error) {
	return parseGeminiProtocolConfig(raw)
}

func NewGatewayFromResolvedConfig(cfg ResolvedGatewayConfig) (Gateway, error) {
	switch cfg.ProtocolKind {
	case ProtocolKindOpenAIChatCompletions, ProtocolKindOpenAIResponses:
		if cfg.OpenAI == nil {
			return nil, fmt.Errorf("missing openai protocol config")
		}
		gatewayCfg := OpenAIGatewayConfig{
			Transport: cfg.Transport,
			Protocol:  *cfg.OpenAI,
		}
		return NewOpenAIGatewaySDK(gatewayCfg), nil
	case ProtocolKindAnthropicMessages:
		if cfg.Anthropic == nil {
			return nil, fmt.Errorf("missing anthropic protocol config")
		}
		gatewayCfg := AnthropicGatewayConfig{
			Transport: cfg.Transport,
			Protocol:  *cfg.Anthropic,
		}
		return NewAnthropicGatewaySDK(gatewayCfg), nil
	case ProtocolKindGeminiGenerateContent:
		if cfg.Gemini == nil {
			return nil, fmt.Errorf("missing gemini protocol config")
		}
		gatewayCfg := GeminiGatewayConfig{
			Transport: cfg.Transport,
			Protocol:  *cfg.Gemini,
		}
		return NewGeminiGatewaySDK(gatewayCfg), nil
	default:
		return nil, fmt.Errorf("unsupported protocol kind: %s", cfg.ProtocolKind)
	}
}
