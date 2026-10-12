//go:build !darwin

package main

import "fmt"

func installActivityRecordService(config daemonConfig, replaceExecutable bool) (serviceStatus, error) {
	_ = config
	_ = replaceExecutable
	return serviceStatus{}, fmt.Errorf("activity-record service management is currently supported on macOS only")
}

func activityRecordServiceStatus(dataDir string) (serviceStatus, error) {
	_ = dataDir
	return serviceStatus{}, fmt.Errorf("activity-record service management is currently supported on macOS only")
}

func uninstallActivityRecordService(dataDir string) (serviceStatus, error) {
	_ = dataDir
	return serviceStatus{}, fmt.Errorf("activity-record service management is currently supported on macOS only")
}
