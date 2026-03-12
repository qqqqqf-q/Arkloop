package platform

import (
	"encoding/json"
	"runtime"
	"testing"
)

func TestDetect(t *testing.T) {
	info := Detect()

	if info.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
	}

	// DockerAvailable is a bool — just verify it doesn't panic.
	_ = info.DockerAvailable

	if runtime.GOOS == "darwin" && info.KVMAvailable {
		t.Error("KVMAvailable should be false on macOS")
	}
}

func TestPlatformInfoJSON(t *testing.T) {
	info := PlatformInfo{
		OS:              "linux",
		DockerAvailable: true,
		KVMAvailable:    false,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for _, field := range []string{"os", "docker_available", "kvm_available"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected JSON field %q", field)
		}
	}
}
