//go:build linux

package mouse

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	evRel     = 2
	evKey     = 1
	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
	relWheel  = 8
	relHWheel = 6
	relX      = 0
	relY      = 1
)

type inputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

func listenMouse(ctx context.Context, agg *mouseAgg) error {
	devs, err := findMouseDevices()
	if err != nil || len(devs) == 0 {
		return fmt.Errorf("no mouse device found: %v", err)
	}

	ch := make(chan inputEvent, 128)
	for _, dev := range devs {
		go readDev(ctx, dev, ch)
	}

	var curX, curY float64
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			switch ev.Type {
			case evRel:
				switch ev.Code {
				case relX:
					curX += float64(ev.Value)
				case relY:
					curY += float64(ev.Value)
				case relWheel, relHWheel:
					agg.scrolls++
				}
			case evKey:
				if ev.Value == 1 {
					switch ev.Code {
					case btnLeft, btnRight, btnMiddle:
						agg.clicks++
						agg.pathEvents = append(agg.pathEvents, mousePathEvent{
							at: time.Now(),
							x:  curX, y: curY,
						})
					}
				}
			}
		}
	}
}

func readDev(ctx context.Context, path string, ch chan<- inputEvent) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	go func() {
		<-ctx.Done()
		f.Close()
	}()

	var ev inputEvent
	for {
		if err := binary.Read(f, binary.LittleEndian, &ev); err != nil {
			return
		}
		select {
		case ch <- ev:
		case <-ctx.Done():
			return
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
