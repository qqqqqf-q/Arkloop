package workspaceblob

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

func Encode(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	encoder, err := zstd.NewWriter(&buffer)
	if err != nil {
		return nil, fmt.Errorf("create zstd writer: %w", err)
	}
	defer func() { _ = encoder.Close() }()
	if _, err := encoder.Write(data); err != nil {
		return nil, fmt.Errorf("compress workspace blob: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close zstd writer: %w", err)
	}
	return buffer.Bytes(), nil
}

func Decode(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open zstd blob: %w", err)
	}
	defer decoder.Close()
	decoded, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("decompress workspace blob: %w", err)
	}
	return decoded, nil
}
