package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type serviceStatus struct {
	Installed      bool   `json:"installed"`
	Loaded         bool   `json:"loaded"`
	Running        bool   `json:"running"`
	PID            int    `json:"pid,omitempty"`
	ConfigPath     string `json:"config_path,omitempty"`
	PlistPath      string `json:"plist_path,omitempty"`
	LogPath        string `json:"log_path,omitempty"`
	ExecutablePath string `json:"executable_path,omitempty"`
	AXPermission   *bool  `json:"ax_permission,omitempty"`
	MicPermission  *bool  `json:"mic_permission,omitempty"`
}

func runService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("service action is required: install, status, or uninstall")
	}
	action := args[0]
	var (
		status serviceStatus
		err    error
	)
	switch action {
	case "install", "update":
		config, configErr := daemonConfigFromArgs(args[1:])
		if configErr != nil {
			return configErr
		}
		status, err = installActivityRecordService(config, action == "update")
	case "status", "uninstall":
		flags := flag.NewFlagSet("service "+action, flag.ContinueOnError)
		dataDir := flags.String("data-dir", defaultDataDir(), "activity-record data directory")
		if parseErr := flags.Parse(args[1:]); parseErr != nil {
			return parseErr
		}
		if action == "status" {
			status, err = activityRecordServiceStatus(*dataDir)
		} else {
			status, err = uninstallActivityRecordService(*dataDir)
		}
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(status)
}
