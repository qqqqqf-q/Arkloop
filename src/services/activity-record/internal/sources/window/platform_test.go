//go:build darwin || linux || windows

package window

import (
	"fmt"
	"testing"
)

func TestActiveWindowReturnsData(t *testing.T) {
	info, err := ActiveWindow()
	if err != nil {
		t.Fatalf("ActiveWindow() error: %v", err)
	}
	fmt.Printf("ActiveWindow: app=%q title=%q pid=%d\n", info.App, info.WindowTitle, info.PID)
	if info.App == "" {
		t.Fatal("ActiveWindow() returned empty app name")
	}
}

func TestIdleSecondsReturnsPositive(t *testing.T) {
	idle, err := idleSeconds()
	if err != nil {
		t.Fatalf("idleSeconds() error: %v", err)
	}
	fmt.Printf("idleSeconds: %.2f\n", idle)
	if idle < 0 {
		t.Fatal("idleSeconds() returned negative value")
	}
}
