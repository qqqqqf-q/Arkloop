//go:build darwin

package wifi

import (
	"fmt"
	"os/exec"
	"strings"
)

type wifiSample struct {
	SSID  string
	BSSID string
	RSSI  int
}

func sampleWiFi() (wifiSample, error) {
	out, err := exec.Command(
		"/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport",
		"-I",
	).Output()
	if err != nil {
		return wifiSample{}, fmt.Errorf("airport -I: %w", err)
	}

	ws := wifiSample{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID: ") {
			ws.SSID = strings.TrimPrefix(line, "SSID: ")
		}
		if strings.HasPrefix(line, "BSSID: ") {
			ws.BSSID = strings.TrimPrefix(line, "BSSID: ")
		}
		if strings.HasPrefix(line, "agrCtlRSSI: ") {
			// Convert RSSI from string to int.
			rssiStr := strings.TrimPrefix(line, "agrCtlRSSI: ")
			var rssi int
			fmt.Sscanf(rssiStr, "%d", &rssi)
			ws.RSSI = rssi
		}
	}
	if ws.SSID == "" {
		return wifiSample{}, fmt.Errorf("no wifi info available")
	}
	return ws, nil
}
