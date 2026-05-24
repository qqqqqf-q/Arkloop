//go:build linux

package clipboard

import (
	"os/exec"
	"strings"
)

func clipboardText() (string, error) {
	// Try xclip first (X11), then xsel, then wl-paste (Wayland)
	for _, cmd := range [][]string{
		{"xclip", "-selection", "clipboard", "-o"},
		{"xsel", "--clipboard", "--output"},
		{"wl-paste", "--no-newline"},
	} {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		out, err := exec.Command(cmd[0], cmd[1:]...).Output()
		if err != nil {
			continue
		}
		return strings.TrimRight(string(out), "\n"), nil
	}
	return "", nil
}
