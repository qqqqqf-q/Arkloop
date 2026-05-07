package webhook

import (
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"unicode/utf8"

	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/google/uuid"
)

func TestIsPrivateIP(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")

	cases := []struct {
		name   string
		ip     string
		expect bool
	}{
		{"rfc1918_10_low", "10.0.0.1", true},
		{"rfc1918_10_high", "10.255.255.255", true},
		{"rfc1918_172_low", "172.16.0.1", true},
		{"rfc1918_172_high", "172.31.255.255", true},
		{"rfc1918_192_low", "192.168.0.1", true},
		{"rfc1918_192_high", "192.168.255.255", true},
		{"loopback_1", "127.0.0.1", true},
		{"loopback_2", "127.0.0.2", true},
		{"ipv6_loopback", "::1", true},
		{"link_local", "169.254.1.1", true},
		{"ipv6_link_local", "fe80::1", true},
		{"ipv6_unique_local", "fd00::1", true},
		{"carrier_grade_nat_low", "100.64.0.1", true},
		{"carrier_grade_nat_high", "100.127.255.255", true},
		{"rfc2544_benchmark", "198.18.0.1", true},

		{"public_google", "8.8.8.8", false},
		{"public_cloudflare", "1.1.1.1", false},
		{"public_doc", "203.0.113.1", false},
		{"public_ipv6_google", "2001:4860:4860::8888", false},
		{"edge_below_172_16", "172.15.255.255", false},
		{"edge_below_100_64", "100.63.255.255", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			got := isPrivateIP(ip)
			if got != tc.expect {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tc.ip, got, tc.expect)
			}
		})
	}
}

func TestSanitizeWebhookResponseBody(t *testing.T) {
	t.Run("escapes_html", func(t *testing.T) {
		raw := []byte(`<script>alert("x")</script>`)
		out := sanitizeWebhookResponseBody(raw)

		if strings.Contains(out, "<") || strings.Contains(out, ">") {
			t.Fatalf("expected html to be escaped, got: %q", out)
		}
		if !strings.Contains(out, "&lt;script&gt;") {
			t.Fatalf("expected escaped script tag, got: %q", out)
		}
	})

	t.Run("escapes_quotes_and_angle_brackets", func(t *testing.T) {
		raw := []byte(`"'<>`)
		out := sanitizeWebhookResponseBody(raw)

		if strings.ContainsAny(out, `"'<>`) {
			t.Fatalf("expected characters to be escaped, got: %q", out)
		}
	})

	t.Run("normalizes_invalid_utf8", func(t *testing.T) {
		raw := []byte{0xff, 0xfe, '<', 'a', '>'}
		out := sanitizeWebhookResponseBody(raw)

		if !utf8.ValidString(out) {
			t.Fatalf("expected valid utf-8 output, got: %q", out)
		}
		if strings.Contains(out, "<") || strings.Contains(out, ">") {
			t.Fatalf("expected html to be escaped, got: %q", out)
		}
	})
}

func TestComputeHMAC(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		a := computeHMAC(1000, []byte(`{"event":"test"}`), "secret")
		b := computeHMAC(1000, []byte(`{"event":"test"}`), "secret")
		if a != b {
			t.Fatalf("same inputs gave different results: %q vs %q", a, b)
		}
	})

	t.Run("different_timestamp", func(t *testing.T) {
		a := computeHMAC(1000, []byte("payload"), "secret")
		b := computeHMAC(1001, []byte("payload"), "secret")
		if a == b {
			t.Fatalf("different timestamps gave same HMAC")
		}
	})

	t.Run("different_payload", func(t *testing.T) {
		a := computeHMAC(1000, []byte("payload-a"), "secret")
		b := computeHMAC(1000, []byte("payload-b"), "secret")
		if a == b {
			t.Fatalf("different payloads gave same HMAC")
		}
	})

	t.Run("different_secret", func(t *testing.T) {
		a := computeHMAC(1000, []byte("payload"), "secret-1")
		b := computeHMAC(1000, []byte("payload"), "secret-2")
		if a == b {
			t.Fatalf("different secrets gave same HMAC")
		}
	})

	t.Run("valid_hex_length", func(t *testing.T) {
		result := computeHMAC(1000, []byte("payload"), "secret")
		if len(result) != 64 {
			t.Errorf("expected 64 hex chars, got %d", len(result))
		}
		if _, err := hex.DecodeString(result); err != nil {
			t.Errorf("result is not valid hex: %v", err)
		}
	})

	t.Run("empty_payload", func(t *testing.T) {
		result := computeHMAC(1000, []byte{}, "secret")
		if len(result) != 64 {
			t.Errorf("expected 64 hex chars for empty payload, got %d", len(result))
		}
	})
}

func TestParseDeliveryPayload(t *testing.T) {
	accountID := uuid.New()
	runID := uuid.New()
	endpointID := uuid.New()
	deliveryID := uuid.New()

	validRaw := func() map[string]any {
		return map[string]any{
			"account_id": accountID.String(),
			"run_id":     runID.String(),
			"trace_id":   "trace-123",
			"payload": map[string]any{
				"endpoint_id": endpointID.String(),
				"delivery_id": deliveryID.String(),
				"event_type":  "run.completed",
				"payload":     map[string]any{"key": "value"},
			},
		}
	}

	t.Run("valid_complete", func(t *testing.T) {
		p, err := parseDeliveryPayload(validRaw())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.AccountID != accountID {
			t.Errorf("AccountID: got %s, want %s", p.AccountID, accountID)
		}
		if p.RunID != runID {
			t.Errorf("RunID: got %s, want %s", p.RunID, runID)
		}
		if p.TraceID != "trace-123" {
			t.Errorf("TraceID: got %q, want %q", p.TraceID, "trace-123")
		}
		if p.EndpointID != endpointID {
			t.Errorf("EndpointID: got %s, want %s", p.EndpointID, endpointID)
		}
		if p.DeliveryID != deliveryID {
			t.Errorf("DeliveryID: got %s, want %s", p.DeliveryID, deliveryID)
		}
		if p.EventType != "run.completed" {
			t.Errorf("EventType: got %q, want %q", p.EventType, "run.completed")
		}
		if p.Payload == nil || p.Payload["key"] != "value" {
			t.Errorf("Payload: got %v, want map with key=value", p.Payload)
		}
	})

	t.Run("missing_account_id", func(t *testing.T) {
		raw := validRaw()
		delete(raw, "account_id")
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for missing account_id")
		}
	})

	t.Run("missing_run_id", func(t *testing.T) {
		raw := validRaw()
		delete(raw, "run_id")
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for missing run_id")
		}
	})

	t.Run("missing_payload_field", func(t *testing.T) {
		raw := validRaw()
		delete(raw, "payload")
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for missing payload field")
		}
	})

	t.Run("missing_endpoint_id", func(t *testing.T) {
		raw := validRaw()
		inner := raw["payload"].(map[string]any)
		delete(inner, "endpoint_id")
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for missing endpoint_id")
		}
	})

	t.Run("missing_delivery_id", func(t *testing.T) {
		raw := validRaw()
		inner := raw["payload"].(map[string]any)
		delete(inner, "delivery_id")
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for missing delivery_id")
		}
	})

	t.Run("missing_event_type", func(t *testing.T) {
		raw := validRaw()
		inner := raw["payload"].(map[string]any)
		delete(inner, "event_type")
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for missing event_type")
		}
	})

	t.Run("empty_event_type", func(t *testing.T) {
		raw := validRaw()
		inner := raw["payload"].(map[string]any)
		inner["event_type"] = ""
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for empty event_type")
		}
	})

	t.Run("invalid_uuid_format", func(t *testing.T) {
		raw := validRaw()
		raw["account_id"] = "not-a-uuid"
		if _, err := parseDeliveryPayload(raw); err == nil {
			t.Fatalf("expected error for invalid UUID")
		}
	})

	t.Run("trace_id_optional", func(t *testing.T) {
		raw := validRaw()
		delete(raw, "trace_id")
		p, err := parseDeliveryPayload(raw)
		if err != nil {
			t.Fatalf("trace_id should be optional: %v", err)
		}
		if p.TraceID != "" {
			t.Errorf("TraceID should be empty when not set, got %q", p.TraceID)
		}
	})

	t.Run("inner_payload_optional", func(t *testing.T) {
		raw := validRaw()
		inner := raw["payload"].(map[string]any)
		delete(inner, "payload")
		p, err := parseDeliveryPayload(raw)
		if err != nil {
			t.Fatalf("inner payload should be optional: %v", err)
		}
		if p.Payload != nil {
			t.Errorf("Payload should be nil when not set, got %v", p.Payload)
		}
	})
}

func TestRequiredUUID(t *testing.T) {
	validID := uuid.New()

	t.Run("valid", func(t *testing.T) {
		m := map[string]any{"id": validID.String()}
		got, err := requiredUUID(m, "id")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != validID {
			t.Errorf("got %s, want %s", got, validID)
		}
	})

	t.Run("missing_key", func(t *testing.T) {
		m := map[string]any{}
		if _, err := requiredUUID(m, "id"); err == nil {
			t.Fatalf("expected error for missing key")
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		m := map[string]any{"id": ""}
		if _, err := requiredUUID(m, "id"); err == nil {
			t.Fatalf("expected error for empty string")
		}
	})

	t.Run("non_string_value", func(t *testing.T) {
		m := map[string]any{"id": 12345}
		if _, err := requiredUUID(m, "id"); err == nil {
			t.Fatalf("expected error for non-string value")
		}
	})

	t.Run("invalid_uuid_format", func(t *testing.T) {
		m := map[string]any{"id": "not-a-uuid"}
		if _, err := requiredUUID(m, "id"); err == nil {
			t.Fatalf("expected error for invalid UUID format")
		}
	})
}
