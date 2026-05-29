//go:build darwin

package audio

/*
#cgo LDFLAGS: -framework AVFoundation -framework Foundation
int arkloopMicAuthorizationStatus(void);
*/
import "C"

// CheckMicPermission reports whether microphone access is authorized.
// AVAuthorizationStatus: 0=notDetermined, 1=restricted, 2=denied, 3=authorized.
func CheckMicPermission() bool {
	return int(C.arkloopMicAuthorizationStatus()) == 3
}
