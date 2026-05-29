package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestEncodeWAVHeader(t *testing.T) {
	samples := []int16{0, 100, -100, 32767, -32768}
	wav := encodeWAV(samples, sampleRate)

	if len(wav) != 44+len(samples)*2 {
		t.Fatalf("wav length = %d, want %d", len(wav), 44+len(samples)*2)
	}
	if !bytes.Equal(wav[0:4], []byte("RIFF")) || !bytes.Equal(wav[8:12], []byte("WAVE")) {
		t.Fatalf("missing RIFF/WAVE markers")
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != sampleRate {
		t.Fatalf("sample rate = %d, want %d", got, sampleRate)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != 16 {
		t.Fatalf("bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(samples)*2) {
		t.Fatalf("data chunk size = %d, want %d", got, len(samples)*2)
	}
	for i, want := range samples {
		off := 44 + i*2
		got := int16(binary.LittleEndian.Uint16(wav[off : off+2]))
		if got != want {
			t.Fatalf("sample %d = %d, want %d", i, got, want)
		}
	}
}

func TestDedup(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		want     string
	}{
		{"empty previous", "", "hello world", "hello world"},
		{"no overlap", "the cat sat", "on the mat now", "on the mat now"},
		{"trailing overlap", "I went to the store", "to the store and bought milk", "and bought milk"},
		{"full duplicate", "good morning everyone", "Good morning everyone", ""},
		{"punctuation preserved", "let us begin", "Begin, shall we?", "shall we?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dedup(tt.previous, tt.current); got != tt.want {
				t.Fatalf("dedup(%q, %q) = %q, want %q", tt.previous, tt.current, got, tt.want)
			}
		})
	}
}

func TestRMSInt16(t *testing.T) {
	silence := make([]int16, 1000)
	if r := rmsInt16(silence); r != 0 {
		t.Fatalf("silence rms = %v, want 0", r)
	}
	full := make([]int16, 1000)
	for i := range full {
		full[i] = 32767
	}
	if r := rmsInt16(full); math.Abs(r-0.999969) > 0.001 {
		t.Fatalf("full-scale rms = %v, want ~1.0", r)
	}
}

func TestNormalizeRaisesQuietSignal(t *testing.T) {
	seg := make([]int16, sampleRate)
	for i := range seg {
		seg[i] = int16(200 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}
	before := rmsInt16(seg)
	out := normalize(seg)
	after := rmsInt16(out)
	if after <= before {
		t.Fatalf("normalize did not raise rms: before=%v after=%v", before, after)
	}
	if after > peakLimit+0.01 {
		t.Fatalf("normalized rms %v exceeds peak limit", after)
	}
}

func TestNormalizeSilencePassthrough(t *testing.T) {
	seg := make([]int16, 100)
	out := normalize(seg)
	if len(out) != len(seg) {
		t.Fatalf("length changed: %d != %d", len(out), len(seg))
	}
	for i, v := range out {
		if v != 0 {
			t.Fatalf("silence sample %d = %d, want 0", i, v)
		}
	}
}

func TestIsMusicSilenceNotMusic(t *testing.T) {
	seg := make([]int16, segmentSamples)
	if isMusic(seg) {
		t.Fatalf("pure silence classified as music")
	}
}
