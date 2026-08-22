//go:build linux

package keyboard

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const evKey = 1

type inputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

func listenKeystrokes(ctx context.Context, session *typingSession) error {
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

	// keymap lookup table: read once from the kernel.
	keymap := loadKeymap()

	var ev inputEvent
	for {
		if err := binary.Read(f, binary.LittleEndian, &ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ev.Type != evKey || ev.Value != 1 {
			continue
		}
		code := ev.Code

		isBackspace := code == 14
		isEnter := code == 28
		isTab := code == 15

		chars := ""
		if !isBackspace && !isEnter && !isTab && int(code) < len(keymap) {
			chars = keymap[code]
		}

		session.feed(keyEvent{
			timestampMs: uint64(ev.TimeSec*1000 + int64(ev.TimeUsec/1000)),
			chars:       chars,
			isBackspace: isBackspace,
			isEnter:     isEnter,
			isTab:       isTab,
		})
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

// loadKeymap returns a mapping from Linux keycode to UTF-8 string.
// This is a minimal US layout mapping for common keys.
// A full solution would read the keymap from the kernel; this covers
// alphanumeric and common punctuation.
func loadKeymap() []string {
	m := make([]string, 256)
	// Row: numbers
	assign(m, 2, "1"); assign(m, 3, "2"); assign(m, 4, "3")
	assign(m, 5, "4"); assign(m, 6, "5"); assign(m, 7, "6")
	assign(m, 8, "7"); assign(m, 9, "8"); assign(m, 10, "9"); assign(m, 11, "0")
	assign(m, 12, "-"); assign(m, 13, "=")
	// Row: qwerty
	assign(m, 16, "q"); assign(m, 17, "w"); assign(m, 18, "e"); assign(m, 19, "r")
	assign(m, 20, "t"); assign(m, 21, "y"); assign(m, 22, "u"); assign(m, 23, "i")
	assign(m, 24, "o"); assign(m, 25, "p"); assign(m, 26, "["); assign(m, 27, "]")
	// Row: asdf
	assign(m, 30, "a"); assign(m, 31, "s"); assign(m, 32, "d"); assign(m, 33, "f")
	assign(m, 34, "g"); assign(m, 35, "h"); assign(m, 36, "j"); assign(m, 37, "k")
	assign(m, 38, "l"); assign(m, 39, ";"); assign(m, 40, "'")
	// Row: zxcv
	assign(m, 44, "z"); assign(m, 45, "x"); assign(m, 46, "c"); assign(m, 47, "v")
	assign(m, 48, "b"); assign(m, 49, "n"); assign(m, 50, "m")
	assign(m, 51, ","); assign(m, 52, "."); assign(m, 53, "/")
	// Space
	assign(m, 57, " ")
	// Numpad
	assign(m, 71, "7"); assign(m, 72, "8"); assign(m, 73, "9")
	assign(m, 75, "4"); assign(m, 76, "5"); assign(m, 77, "6")
	assign(m, 79, "1"); assign(m, 80, "2"); assign(m, 81, "3")
	assign(m, 82, "0"); assign(m, 83, ".")
	// Symbols
	assign(m, 41, "`"); assign(m, 43, "\\")
	return m
}

func assign(m []string, code uint16, s string) {
	m[code] = s
}

