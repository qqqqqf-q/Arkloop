package workspaceblob

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	original := []byte("\x89PNG\r\n\x1a\nPNGDATA")
	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if bytes.Equal(encoded, original) {
		t.Fatal("expected encoded payload to differ from original")
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Fatalf("decoded payload mismatch: %q", decoded)
	}
}
