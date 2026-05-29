package audio

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/store"
)

const (
	sampleRate      = 16000
	channels        = 1
	segmentSamples  = sampleRate * 30
	overlapSamples  = sampleRate * 2
	vadFrameSamples = sampleRate / 1000 * 20 // 20ms = 320 samples
	rmsSilence      = 0.002
	vadMode         = 2 // aggressive
)

type Config struct {
	APIBase  string
	APIKey   string
	Model    string
	Language string
	Timeout  time.Duration
}

type Source struct {
	cfg Config
}

func New(cfg Config) *Source {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "qwen/qwen3-asr-flash-2026-02-10"
	}
	return &Source{cfg: cfg}
}

func (s *Source) Name() string { return "audio" }

func (s *Source) Sync(context.Context, *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	stream, err := openCapture()
	if err != nil {
		return fmt.Errorf("open audio capture: %w", err)
	}
	defer stream.Close()

	vad, err := newVAD(vadMode)
	if err != nil {
		log.Printf("audio: vad init failed, using rms-only gate: %v", err)
	}
	if vad != nil {
		defer vad.Close()
	}

	segments := make(chan []int16, 8)
	go segmenter(ctx, stream.Frames(), segments)

	device := stream.DeviceName()
	var prev string
	for {
		select {
		case <-ctx.Done():
			return nil
		case seg, ok := <-segments:
			if !ok {
				return stream.Err()
			}
			s.handleSegment(ctx, seg, device, vad, &prev, events)
		}
	}
}

// segmenter accumulates raw frames into fixed 30s segments with a 2s overlap
// retained between consecutive segments. Each emitted segment is an owned copy.
func segmenter(ctx context.Context, frames <-chan []int16, out chan<- []int16) {
	defer close(out)
	buf := make([]int16, 0, segmentSamples+overlapSamples)
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-frames:
			if !ok {
				return
			}
			buf = append(buf, chunk...)
			for len(buf) >= segmentSamples {
				seg := make([]int16, segmentSamples)
				copy(seg, buf[:segmentSamples])
				select {
				case out <- seg:
				case <-ctx.Done():
					return
				}
				buf = append(buf[:0], buf[segmentSamples-overlapSamples:]...)
			}
		}
	}
}

func (s *Source) handleSegment(ctx context.Context, seg []int16, device string, vad *vadGate, prev *string, events chan<- store.Event) {
	end := time.Now().UTC()
	start := end.Add(-time.Duration(len(seg)) * time.Second / sampleRate)

	if rmsInt16(seg) < rmsSilence {
		return
	}

	// 段内裁剪:只保留检测到语音的部分送转写,剔除段内静音,按实际语音时长计费。
	speech := seg
	if vad != nil {
		regions := vad.speechRegions(seg)
		if len(regions) == 0 {
			return
		}
		total := 0
		for _, r := range regions {
			total += r[1] - r[0]
		}
		if total < minSpeechSamples {
			return
		}
		speech = make([]int16, 0, total)
		for _, r := range regions {
			speech = append(speech, seg[r[0]:r[1]]...)
		}
	}

	if isMusic(speech) {
		return
	}

	wav := encodeWAV(normalize(speech), sampleRate)
	text, err := s.transcribe(ctx, wav)
	if err != nil {
		log.Printf("audio: transcribe: %v", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	out := dedup(*prev, text)
	*prev = text
	if out == "" {
		return
	}

	sum := sha256.Sum256([]byte(out))
	hash := fmt.Sprintf("%x", sum)
	events <- store.Event{
		Source:        "audio",
		SourceEventID: fmt.Sprintf("audio:%d:%s", start.UnixMilli(), hash[:12]),
		OccurredAt:    start,
		Action:        "audio_transcription",
		Title:         truncate(out, 200),
		Text:          out,
		Metadata: map[string]any{
			"duration_sec": len(speech) / sampleRate,
			"device":       device,
			"language":     s.cfg.Language,
			"model":        s.cfg.Model,
		},
	}
}

func truncate(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
