//go:build darwin

package clipboard

import "os/exec"

func clipboardText() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
