//go:build linux

package keyboard

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

const evKey = 1

type inputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

func listenKeystrokes(ctx context.Context, count *atomic.Int64) error {
	devs, err := findKeyboardDevices()
	if err != nil || len(devs) == 0 {
		return fmt.Errorf("no keyboard device found: %v", err)
	}

	f, err := os.Open(devs[0])
	if err != nil {
		return fmt.Errorf("open %s: %w (try running as root or adding user to input group)", devs[0], err)
	}
	defer f.Close()

	go func() {
		<-ctx.Done()
		f.Close()
	}()

	var ev inputEvent
	for {
		if err := binary.Read(f, binary.LittleEndian, &ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ev.Type == evKey && ev.Value == 1 {
			count.Add(1)
		}
	}
}

func findKeyboardDevices() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/input")
	if err != nil {
		return nil, err
	}
	var devs []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		capPath := filepath.Join("/sys/class/input", e.Name(), "device", "capabilities", "key")
		data, err := os.ReadFile(capPath)
		if err != nil {
			continue
		}
		caps := strings.TrimSpace(string(data))
		if len(caps) > 20 {
			devs = append(devs, "/dev/input/"+e.Name())
		}
	}
	return devs, nil
}
