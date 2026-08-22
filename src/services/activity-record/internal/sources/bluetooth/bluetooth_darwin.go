//go:build darwin

package bluetooth

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type btDevice struct {
	Name      string
	Connected bool
}

func pollDevices() ([]btDevice, error) {
	out, err := exec.Command(
		"system_profiler", "SPBluetoothDataType", "-json",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("system_profiler SPBluetoothDataType: %w", err)
	}

	// Parse the nested JSON structure.
	var raw struct {
		SPBluetoothDataType []struct {
			DevicesInfo *struct {
				DeviceTitle      string `json:"device_title"`
				DeviceConnected  string `json:"device_connected"`
			} `json:"device_info"`
			ConnectedDevices []struct {
				DeviceTitle      string `json:"device_title"`
				DeviceConnected  string `json:"device_connected"`
			} `json:"device_connected"`
			NotConnectedDevices []struct {
				DeviceTitle      string `json:"device_title"`
				DeviceConnected  string `json:"device_connected"`
			} `json:"device_not_connected"`
		} `json:"SPBluetoothDataType"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse bluetooth json: %w", err)
	}

	var devices []btDevice
	for _, section := range raw.SPBluetoothDataType {
		for _, d := range section.ConnectedDevices {
			devices = append(devices, btDevice{
				Name:      d.DeviceTitle,
				Connected: true,
			})
		}
		for _, d := range section.NotConnectedDevices {
			devices = append(devices, btDevice{
				Name:      d.DeviceTitle,
				Connected: false,
			})
		}
	}
	return devices, nil
}
