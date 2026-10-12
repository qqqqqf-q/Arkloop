package audio

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/activity-record/internal/store"

	"github.com/gen2brain/malgo"
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
	// Devices 选择采集设备:空或 ["default"] 用系统默认设备;["all"] 枚举全部;
	// 否则按设备名精确匹配(区分大小写),匹配不到在启动时报错并列出可用设备。
	Devices []string
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

// deviceRef 是一路采集的打开参数。devID 为 nil 表示系统默认设备。
// name 是用户配置的名字(原始大小写),slug 进 SourceEventID,log 进 Title。
type deviceRef struct {
	devID *malgo.DeviceID
	name  string
	slug  string
	log   string
}

// resolveDevices 把 Devices 配置解析成要打开的设备列表。枚举失败只在需要
// 枚举时发生;默认路径不触发枚举,保持零配置即原行为。
func resolveDevices(want []string, available []CaptureDeviceInfo) ([]deviceRef, error) {
	if len(want) == 0 {
		want = []string{"default"}
	}
	if len(want) == 1 && want[0] == "default" {
		return []deviceRef{{name: "default", slug: "default", log: "default"}}, nil
	}
	var refs []deviceRef
	if len(want) == 1 && want[0] == "all" {
		for _, info := range available {
			refs = append(refs, namedDeviceRef(info))
		}
	} else {
		for _, w := range want {
			var found *CaptureDeviceInfo
			for i := range available {
				if available[i].Name == w {
					found = &available[i]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("audio device %q not found (available: %s)", w, deviceNames(available))
			}
			refs = append(refs, namedDeviceRef(*found))
		}
	}
	seen := map[string]bool{}
	out := refs[:0]
	for _, r := range refs {
		if seen[r.slug] {
			continue
		}
		seen[r.slug] = true
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no capture devices matched %v", want)
	}
	return out, nil
}

func namedDeviceRef(info CaptureDeviceInfo) deviceRef {
	name := info.Name
	slug := deviceSlug(name)
	ref := deviceRef{name: name, slug: slug, log: name}
	if info.IsDefault {
		ref.slug = "default"
	}
	return ref
}

func deviceNames(infos []CaptureDeviceInfo) string {
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = strconv.Quote(info.Name)
	}
	return strings.Join(names, ", ")
}

// deviceSlug 把设备名压成 SourceEventID 可用的稳定标识:小写、非字母数字折叠成 -。
// 非 ASCII(如中文设备名)折叠后可能为空,此时退化为 sha1 前 8 位保证唯一。
func deviceSlug(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.TrimSuffix(b.String(), "-")
	if len(slug) > 24 {
		slug = strings.TrimRight(slug[:24], "-")
	}
	if slug == "" {
		sum := sha256.Sum256([]byte(name))
		slug = fmt.Sprintf("dev-%x", sum[:4])
	}
	return slug
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	refs, err := s.selectDevices()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.runDevice(ctx, ref, events); err != nil && ctx.Err() == nil {
				log.Printf("audio: device %q exited: %v", ref.name, err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// selectDevices 需要枚举时才枚举;每次 Run 重新枚举,新插入的设备在 daemon
// 重启前不会加入(热插拔不在本期范围)。
func (s *Source) selectDevices() ([]deviceRef, error) {
	want := s.cfg.Devices
	if len(want) == 0 || (len(want) == 1 && want[0] == "default") {
		return resolveDevices(want, nil)
	}
	available, err := ListCaptureDevices()
	if err != nil {
		return nil, err
	}
	return resolveDevices(want, available)
}

// runDevice 是一路设备的完整流水线:capture → segmenter → 逐段处理。
// VAD 实例与 dedup 链都是每路独立的,两路同时说话不会互相吃掉前缀。
func (s *Source) runDevice(ctx context.Context, ref deviceRef, events chan<- store.Event) error {
	var devID *malgo.DeviceID
	if ref.devID != nil {
		devID = ref.devID
	} else if ref.slug != "default" {
		// 非默认设备按名字回查 DeviceID(枚举快照里的字符串 ID 不能反向用于打开)。
		id, err := findDeviceIDByName(ref.name)
		if err != nil {
			return err
		}
		devID = id
	}
	stream, err := openCapture(devID, ref.name)
	if err != nil {
		return fmt.Errorf("open audio capture: %w", err)
	}
	defer stream.Close()
	log.Printf("audio[%s]: capturing %q", ref.slug, ref.name)

	vad, err := newVAD(vadMode)
	if err != nil {
		log.Printf("audio[%s]: vad init failed, using rms-only gate: %v", ref.slug, err)
	}
	if vad != nil {
		defer vad.Close()
	}

	segments := make(chan []int16, 8)
	go segmenter(ctx, stream.Frames(), segments)

	var prev string
	for {
		select {
		case <-ctx.Done():
			return nil
		case seg, ok := <-segments:
			if !ok {
				return stream.Err()
			}
			s.handleSegment(ctx, seg, ref, vad, &prev, events)
		}
	}
}

// findDeviceIDByName 枚举当前设备并按名字精确匹配,返回可打开的 DeviceID。
// 名字来自启动时的枚举快照,两次枚举之间设备被拔出会在这里报错。
func findDeviceIDByName(name string) (*malgo.DeviceID, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init audio context: %w", err)
	}
	defer func() {
		_ = mctx.Uninit()
		mctx.Free()
	}()
	infos, err := mctx.Context.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("list capture devices: %w", err)
	}
	for i := range infos {
		if infos[i].Name() == name {
			id := infos[i].ID
			return &id, nil
		}
	}
	return nil, fmt.Errorf("audio device %q disappeared", name)
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

func (s *Source) handleSegment(ctx context.Context, seg []int16, ref deviceRef, vad *vadGate, prev *string, events chan<- store.Event) {
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
		log.Printf("audio[%s]: transcribe: %v", ref.slug, err)
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
		Source: "audio",
		// 事件 ID 带设备 slug:多路麦克风同时收到同一说话人时文本与毫秒都相同,
		// 不带设备会撞 UNIQUE(source, source_event_id) 互相覆盖。
		SourceEventID: fmt.Sprintf("audio:%s:%d:%s", ref.slug, start.UnixMilli(), hash[:12]),
		OccurredAt:    start,
		Action:        "audio_transcription",
		Title:         "[" + ref.log + "] " + truncate(out, 200),
		Text:          out,
		Metadata: map[string]any{
			"duration_sec": len(speech) / sampleRate,
			"device":       ref.name,
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
