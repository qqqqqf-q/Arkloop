package observability

import "context"

type clientIPKey struct{}
type userAgentKey struct{}
type requestHTTPSKey struct{}

func WithClientIP(ctx context.Context, ip string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ip == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIPKey{}, ip)
}

func ClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(clientIPKey{}).(string)
	return v
}

func WithUserAgent(ctx context.Context, ua string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ua == "" {
		return ctx
	}
	return context.WithValue(ctx, userAgentKey{}, ua)
}

func UserAgentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(userAgentKey{}).(string)
	return v
}

func WithRequestHTTPS(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestHTTPSKey{}, enabled)
}

func RequestHTTPSFromContext(ctx context.Context) (bool, bool) {
	if ctx == nil {
		return false, false
	}
	v, ok := ctx.Value(requestHTTPSKey{}).(bool)
	return v, ok
}
