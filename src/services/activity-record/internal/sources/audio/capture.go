package audio

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/gen2brain/malgo"
)

type captureStream struct {
	mctx   *malgo.AllocatedContext
	device *malgo.Device
	frames chan []int16
	name   string

	mu     sync.Mutex
	err    error
	closed bool
}

func openCapture() (*captureStream, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init audio context: %w", err)
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = channels
	cfg.SampleRate = sampleRate
	cfg.Alsa.NoMMap = 1

	cs := &captureStream{
		mctx:   mctx,
		frames: make(chan []int16, 64),
		name:   "default",
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
		return nil, fmt.Errorf("init audio device: %w", err)
	}
	cs.device = device

	if err := device.Start(); err != nil {
		device.Uninit()
		_ = mctx.Uninit()
		mctx.Free()
		return nil, fmt.Errorf("start audio device: %w", err)
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
