package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func encodeWAV(samples []int16, rate int) []byte {
	dataLen := len(samples) * 2
	var buf bytes.Buffer
	buf.Grow(44 + dataLen)

	buf.WriteString("RIFF")
	writeU32(&buf, uint32(36+dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	writeU32(&buf, 16) // PCM fmt chunk size
	writeU16(&buf, 1)  // audio format = PCM
	writeU16(&buf, uint16(channels))
	writeU32(&buf, uint32(rate))
	writeU32(&buf, uint32(rate*channels*2)) // byte rate
	writeU16(&buf, uint16(channels*2))      // block align
	writeU16(&buf, 16)                      // bits per sample
	buf.WriteString("data")
	writeU32(&buf, uint32(dataLen))

	pcm := make([]byte, dataLen)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(s))
	}
	buf.Write(pcm)
	return buf.Bytes()
}

func writeU16(buf *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	buf.Write(b[:])
}

func writeU32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

// transcribe posts WAV audio to an OpenRouter-style transcription endpoint:
// JSON body with base64-encoded audio under input_audio, not OpenAI multipart.
func (s *Source) transcribe(ctx context.Context, wav []byte) (string, error) {
	payload, err := json.Marshal(transcriptionRequest{
		Model: s.cfg.Model,
		InputAudio: inputAudio{
			Data:   base64.StdEncoding.EncodeToString(wav),
			Format: "wav",
		},
		Language: s.cfg.Language,
	})
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(s.cfg.APIBase, "/") + "/audio/transcriptions"
	reqCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("transcription api status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out struct {
		Text  string `json:"text"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("decode transcription response: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("transcription api error: %s", out.Error.Message)
	}
	return out.Text, nil
}

type transcriptionRequest struct {
	Model      string     `json:"model"`
	InputAudio inputAudio `json:"input_audio"`
	Language   string     `json:"language,omitempty"`
}

type inputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}
