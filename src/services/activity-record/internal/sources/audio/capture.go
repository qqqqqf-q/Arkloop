package audio

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/gen2brain/malgo"
)

// CaptureDeviceInfo 是枚举到的采集设备。ID 是 malgo DeviceID 的字符串形式,
// 仅用于日志;打开设备时重新按名字匹配回 DeviceID。
type CaptureDeviceInfo struct {
	ID        string
	Name      string
	IsDefault bool
}

// ListCaptureDevices 枚举当前可用的采集设备。返回的是快照,热插拔后需重新枚举。
func ListCaptureDevices() ([]CaptureDeviceInfo, error) {
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
	out := make([]CaptureDeviceInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, CaptureDeviceInfo{
			ID:        info.ID.String(),
			Name:      info.Name(),
			IsDefault: info.IsDefault != 0,
		})
	}
	return out, nil
}

type captureStream struct {
	mctx   *malgo.AllocatedContext
	device *malgo.Device
	frames chan []int16
	name   string

	mu     sync.Mutex
	err    error
	closed bool
}

// openCapture 打开指定采集设备;deviceID 为 nil 时打开系统默认设备。
// deviceID 只需存活到 InitDevice 返回(miniaudio 按值复制)。
func openCapture(deviceID *malgo.DeviceID, name string) (*captureStream, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init audio context: %w", err)
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = channels
	cfg.SampleRate = sampleRate
	cfg.Alsa.NoMMap = 1
	if deviceID != nil {
		cfg.Capture.DeviceID = deviceID.Pointer()
	}

	cs := &captureStream{
		mctx:   mctx,
		frames: make(chan []int16, 64),
		name:   name,
	}

	onData := func(_, in []byte, frameCount uint32) {
		n := int(frameCount) * channels
		if n*2 > len(in) {
			n = len(in) / 2
		}
		if n <= 0 {
			return
		}
		samples := make([]int16, n)
		for i := 0; i < n; i++ {
			samples[i] = int16(binary.LittleEndian.Uint16(in[i*2:]))
		}
		select {
		case cs.frames <- samples:
		default:
			// Drop on overload: the capture thread must never block.
		}
	}

	device, err := malgo.InitDevice(mctx.Context, cfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		_ = mctx.Uninit()
		mctx.Free()
		return nil, fmt.Errorf("init audio device %q: %w", name, err)
	}
	cs.device = device

	if err := device.Start(); err != nil {
		device.Uninit()
		_ = mctx.Uninit()
		mctx.Free()
		return nil, fmt.Errorf("start audio device %q: %w", name, err)
	}
	return cs, nil
}

func (c *captureStream) Frames() <-chan []int16 { return c.frames }

func (c *captureStream) DeviceName() string { return c.name }

func (c *captureStream) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *captureStream) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	if c.device != nil {
		c.device.Uninit() // stops the audio thread; no callbacks fire after this returns
	}
	if c.mctx != nil {
		_ = c.mctx.Uninit()
		c.mctx.Free()
	}
	close(c.frames)
	return nil
}
