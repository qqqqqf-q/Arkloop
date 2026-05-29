//go:build !darwin

package audio

// CheckMicPermission is a no-op on platforms without an OS-level microphone
// authorization gate; capture errors surface at device-open time instead.
func CheckMicPermission() bool { return true }
