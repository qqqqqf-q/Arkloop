//go:build linux

package mouse

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	evRel       = 2
	evKey       = 1
	btnLeft     = 0x110
	btnRight    = 0x111
	btnMiddle   = 0x112
	relWheel    = 8
	relHWheel   = 6
)

type inputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

func listenMouse(ctx context.Context, c *counters) error {
	devs, err := findMouseDevices()
	if err != nil || len(devs) == 0 {
		return fmt.Errorf("no mouse device found: %v", err)
	}

	f, err := os.Open(devs[0])
	if err != nil {
		return fmt.Errorf("open %s: %w (try adding user to input group)", devs[0], err)
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
			switch ev.Code {
			case btnLeft, btnRight, btnMiddle:
				c.clicks.Add(1)
			}
		}
		if ev.Type == evRel && (ev.Code == relWheel || ev.Code == relHWheel) {
			c.scrolls.Add(1)
		}
	}
}

func findMouseDevices() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/input")
	if err != nil {
		return nil, err
	}
	var devs []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		capPath := filepath.Join("/sys/class/input", e.Name(), "device", "capabilities", "rel")
		data, err := os.ReadFile(capPath)
		if err != nil {
			continue
		}
		caps := strings.TrimSpace(string(data))
		if caps != "0" && caps != "" {
			devs = append(devs, "/dev/input/"+e.Name())
		}
	}
	return devs, nil
}
