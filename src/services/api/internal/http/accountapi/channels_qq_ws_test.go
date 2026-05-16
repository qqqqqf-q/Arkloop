//go:build desktop

package accountapi

import (
	"errors"
	"testing"
	"time"
)

type fakeQQOneBotRuntime struct {
	wsURL      string
	token      string
	uins       []string
	loginCalls []string
	loginErr   error
}

func (f *fakeQQOneBotRuntime) WSEndpoint() (string, string) {
	return f.wsURL, f.token
}

func (f *fakeQQOneBotRuntime) QuickLoginUins() []string {
	return append([]string(nil), f.uins...)
}

func (f *fakeQQOneBotRuntime) QuickLogin(uin string) error {
	f.loginCalls = append(f.loginCalls, uin)
	return f.loginErr
}

func TestResolveQQWSListenerEndpointUsesManagedEndpoint(t *testing.T) {
	runtime := &fakeQQOneBotRuntime{wsURL: " ws://127.0.0.1:6098 ", token: " token "}

	wsURL, token := resolveQQWSListenerEndpoint(qqChannelConfig{}, runtime)

	if wsURL != "ws://127.0.0.1:6098" || token != "token" {
		t.Fatalf("unexpected endpoint: ws=%q token=%q", wsURL, token)
	}
}

func TestResolveQQWSListenerEndpointKeepsExplicitConfig(t *testing.T) {
	runtime := &fakeQQOneBotRuntime{wsURL: "ws://127.0.0.1:6098", token: "runtime-token"}
	cfg := qqChannelConfig{OneBotWSURL: " ws://10.0.0.2:6098 ", OneBotToken: " configured-token "}

	wsURL, token := resolveQQWSListenerEndpoint(cfg, runtime)

	if wsURL != "ws://10.0.0.2:6098" || token != "configured-token" {
		t.Fatalf("unexpected endpoint: ws=%q token=%q", wsURL, token)
	}
}

func TestQQAutoQuickLoginAttemptsConfiguredAvailableUin(t *testing.T) {
	runtime := &fakeQQOneBotRuntime{uins: []string{"10001", "10002"}}
	lastAttempt := time.Now().Add(-time.Minute)

	qqAutoQuickLogin(runtime, qqChannelConfig{AutoLoginUin: "10002"}, &lastAttempt)

	if len(runtime.loginCalls) != 1 || runtime.loginCalls[0] != "10002" {
		t.Fatalf("unexpected quick login calls: %#v", runtime.loginCalls)
	}
}

func TestQQAutoQuickLoginDoesNotCooldownUnavailableUin(t *testing.T) {
	runtime := &fakeQQOneBotRuntime{uins: []string{"10001"}}
	var lastAttempt time.Time

	qqAutoQuickLogin(runtime, qqChannelConfig{AutoLoginUin: "10002"}, &lastAttempt)

	if len(runtime.loginCalls) != 0 {
		t.Fatalf("unexpected quick login calls: %#v", runtime.loginCalls)
	}
	if !lastAttempt.IsZero() {
		t.Fatalf("lastAttempt should stay zero, got %s", lastAttempt)
	}
}

func TestQQAutoQuickLoginCooldownAfterAttempt(t *testing.T) {
	runtime := &fakeQQOneBotRuntime{uins: []string{"10001"}, loginErr: errors.New("failed")}
	lastAttempt := time.Now().Add(-time.Minute)

	qqAutoQuickLogin(runtime, qqChannelConfig{AutoLoginUin: "10001"}, &lastAttempt)
	qqAutoQuickLogin(runtime, qqChannelConfig{AutoLoginUin: "10001"}, &lastAttempt)

	if len(runtime.loginCalls) != 1 {
		t.Fatalf("expected one login call during cooldown, got %#v", runtime.loginCalls)
	}
}
