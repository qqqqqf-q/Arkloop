package stablejson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func Encode(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	out := strings.TrimSuffix(buf.String(), "\n")
	return out, nil
}

func Sha256(value any) (string, error) {
	encoded, err := Encode(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(sum[:]), nil
}

func MustSha256(value any) string {
	hashed, err := Sha256(value)
	if err != nil {
		panic(fmt.Errorf("stablejson sha256 failed: %w", err))
	}
	return hashed
}

