package webfetch

import (
	"errors"
	"net/netip"
	"testing"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

// SSRF 拦截核心函数 EnsureURLAllowed 全路径测试

func TestEnsureURLAllowed(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")

	tests := []struct {
		name       string
		rawURL     string
		wantErr    bool
		wantReason string // 为空时仅检查 err != nil
	}{
		// -- 合法 URL --
		{name: "https 公网域名", rawURL: "https://example.com/path?q=1", wantErr: false},
		{name: "http 公网域名", rawURL: "http://example.com", wantErr: false},
		{name: "公网 IP", rawURL: "https://8.8.8.8/dns", wantErr: false},
		{name: "带端口公网域名", rawURL: "https://example.com:8443/api", wantErr: false},

		// -- scheme 校验 --
		{name: "ftp 协议拒绝", rawURL: "ftp://example.com/file", wantErr: true, wantReason: "unsupported_scheme"},
		{name: "file 协议拒绝", rawURL: "file:///etc/passwd", wantErr: true, wantReason: "unsupported_scheme"},
		{name: "javascript 协议拒绝", rawURL: "javascript:alert(1)", wantErr: true, wantReason: "unsupported_scheme"},
		{name: "空 scheme 拒绝", rawURL: "://example.com", wantErr: true, wantReason: "invalid_url"},
		{name: "data 协议拒绝", rawURL: "data:text/html,<h1>hi</h1>", wantErr: true, wantReason: "unsupported_scheme"},

		// -- hostname 校验 --
		{name: "无 hostname 拒绝", rawURL: "http:///path", wantErr: true, wantReason: "missing_hostname"},

		// -- localhost 拦截 --
		{name: "localhost 拒绝", rawURL: "http://localhost/admin", wantErr: true, wantReason: "localhost_denied"},
		{name: "LOCALHOST 大写拒绝", rawURL: "http://LOCALHOST/admin", wantErr: true, wantReason: "localhost_denied"},
		{name: ".localhost 后缀拒绝", rawURL: "http://evil.localhost/api", wantErr: true, wantReason: "localhost_denied"},
		{name: "末尾点 localhost. 拒绝", rawURL: "http://localhost./x", wantErr: true, wantReason: "localhost_denied"},

		// -- 私有 IP 拦截 --
		{name: "127.0.0.1 环回拒绝", rawURL: "http://127.0.0.1/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "10.x 私有拒绝", rawURL: "http://10.0.0.1/internal", wantErr: true, wantReason: "private_ip_denied"},
		{name: "172.16.x 私有拒绝", rawURL: "http://172.16.0.1/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "192.168.x 私有拒绝", rawURL: "http://192.168.1.1/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "::1 IPv6 环回拒绝", rawURL: "http://[::1]/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "fe80:: 链路本地拒绝", rawURL: "http://[fe80::1]/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "0.0.0.0 未指定拒绝", rawURL: "http://0.0.0.0/", wantErr: true, wantReason: "private_ip_denied"},
		{name: ":: IPv6 未指定拒绝", rawURL: "http://[::]/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "224.0.0.1 组播拒绝", rawURL: "http://224.0.0.1/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "ff02::1 IPv6 组播拒绝", rawURL: "http://[ff02::1]/", wantErr: true, wantReason: "private_ip_denied"},
		{name: "169.254.x 链路本地拒绝", rawURL: "http://169.254.169.254/latest/meta-data/", wantErr: true, wantReason: "private_ip_denied"},

		// -- 非 IP 域名（不可解析为 IP 的hostname）正常放行 --
		{name: "正常域名放行", rawURL: "https://api.openai.com/v1/chat", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureURLAllowed(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望错误但返回 nil")
				}
				var policyErr UrlPolicyDeniedError
				if !errors.As(err, &policyErr) {
					t.Fatalf("期望 UrlPolicyDeniedError 类型, 实际 %T: %v", err, err)
				}
				if tt.wantReason != "" && policyErr.Reason != tt.wantReason {
					t.Fatalf("Reason = %q, 期望 %q", policyErr.Reason, tt.wantReason)
				}
			} else {
				if err != nil {
					t.Fatalf("不期望错误但返回: %v", err)
				}
			}
		})
	}
}

func TestEnsureURLAllowedSkipsSafetyChecksByDefault(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "false")

	for _, rawURL := range []string{
		"http://localhost/admin",
		"http://127.0.0.1:8080/api",
		"https://10.0.0.1/internal",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := EnsureURLAllowed(rawURL); err != nil {
				t.Fatalf("EnsureURLAllowed() error = %v", err)
			}
		})
	}

	var policyErr UrlPolicyDeniedError
	err := EnsureURLAllowed("ftp://example.com/file")
	if !errors.As(err, &policyErr) || policyErr.Reason != "unsupported_scheme" {
		t.Fatalf("EnsureURLAllowed() error = %v, want unsupported_scheme", err)
	}
}

func TestUrlPolicyDeniedErrorMessage(t *testing.T) {
	e := UrlPolicyDeniedError{Reason: "test_reason", Details: map[string]any{"key": "val"}}
	msg := e.Error()
	if msg != "url denied: test_reason" {
		t.Fatalf("Error() = %q, 期望 %q", msg, "url denied: test_reason")
	}
}

func TestTryParseIP(t *testing.T) {
	tests := []struct {
		name      string
		hostname  string
		wantValid bool
		wantAddr  string // 仅 wantValid 时检查
	}{
		{name: "IPv4 正常", hostname: "1.2.3.4", wantValid: true, wantAddr: "1.2.3.4"},
		{name: "IPv6 正常", hostname: "::1", wantValid: true, wantAddr: "::1"},
		{name: "带 zone 标识的 IPv6", hostname: "fe80::1%eth0", wantValid: true, wantAddr: "fe80::1"},
		{name: "普通域名不可解析", hostname: "example.com", wantValid: false},
		{name: "空字符串", hostname: "", wantValid: false},
		{name: "带空格的 IP", hostname: " 10.0.0.1 ", wantValid: true, wantAddr: "10.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := tryParseIP(tt.hostname)
			if tt.wantValid {
				if !addr.IsValid() {
					t.Fatalf("期望有效 IP, 但解析失败")
				}
				if tt.wantAddr != "" && addr != netip.MustParseAddr(tt.wantAddr) {
					t.Fatalf("addr = %v, 期望 %s", addr, tt.wantAddr)
				}
			} else {
				if addr.IsValid() {
					t.Fatalf("期望无效 IP, 但得到 %v", addr)
				}
			}
		})
	}
}

func TestEnsureIPAllowed(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")

	tests := []struct {
		name    string
		ip      string
		wantErr bool
	}{
		{name: "公网 IPv4 放行", ip: "8.8.8.8", wantErr: false},
		{name: "公网 IPv4 放行 2", ip: "1.1.1.1", wantErr: false},
		{name: "127.0.0.1 环回拒绝", ip: "127.0.0.1", wantErr: true},
		{name: "10.x 私有拒绝", ip: "10.0.0.1", wantErr: true},
		{name: "172.16.x 私有拒绝", ip: "172.16.0.1", wantErr: true},
		{name: "192.168.x 私有拒绝", ip: "192.168.1.1", wantErr: true},
		{name: "::1 IPv6 环回拒绝", ip: "::1", wantErr: true},
		{name: "fe80:: 链路本地拒绝", ip: "fe80::1", wantErr: true},
		{name: "0.0.0.0 未指定拒绝", ip: "0.0.0.0", wantErr: true},
		{name: "169.254.169.254 链路本地拒绝", ip: "169.254.169.254", wantErr: true},
		{name: "224.0.0.1 组播拒绝", ip: "224.0.0.1", wantErr: true},
		{name: "ff02::1 IPv6 组播拒绝", ip: "ff02::1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := netip.MustParseAddr(tt.ip)
			err := EnsureIPAllowed(addr)
			if tt.wantErr && err == nil {
				t.Fatalf("期望错误但返回 nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("不期望错误但返回: %v", err)
			}
			if tt.wantErr {
				var policyErr UrlPolicyDeniedError
				if !errors.As(err, &policyErr) {
					t.Fatalf("期望 UrlPolicyDeniedError, 实际 %T", err)
				}
				if policyErr.Reason != "private_ip_denied" {
					t.Fatalf("Reason = %q, 期望 private_ip_denied", policyErr.Reason)
				}
			}
		})
	}
}
