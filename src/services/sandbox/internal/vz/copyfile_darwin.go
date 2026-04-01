//go:build darwin && cgo

package vz

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// copyFile copies src to dst.
// On macOS/APFS it uses clonefileat(2) for a near-instant copy-on-write clone.
// If clonefileat fails (e.g. non-APFS volume), it falls back to a regular io.Copy.
func copyFile(src, dst string) error {
	// Remove dst if it already exists; clonefileat requires the destination to
	// be absent (EEXIST otherwise).
	_ = os.Remove(dst)

	if err := cloneFile(src, dst); err == nil {
		return nil
	}

	// Fallback: byte-for-byte copy.
	return copyFileIO(src, dst)
}

// cloneFile attempts an APFS COW clone via clonefileat(2).
// syscall number 462 is SYS_CLONEFILEAT on macOS arm64 & amd64.
const sysClonefileat = 462

// CLONE_NOOWNERCOPY prevents ownership from being copied (we want the
// current process to own the clone).
const cloneNoownerCopy = 0x0002

func cloneFile(src, dst string) error {
	srcCS, err := syscall.BytePtrFromString(src)
	if err != nil {
		return err
	}
	dstCS, err := syscall.BytePtrFromString(dst)
	if err != nil {
		return err
	}

	// AT_FDCWD on macOS is -2; express as two's-complement via ^uintptr(1).
	const atFdCwd = ^uintptr(1) // = 0xFFFFFFFFFFFFFFFE = (uintptr)(-2)

	// clonefileat(AT_FDCWD, src, AT_FDCWD, dst, CLONE_NOOWNERCOPY)
	r1, _, errno := syscall.Syscall6(
		sysClonefileat,
		atFdCwd,
		uintptr(unsafe.Pointer(srcCS)),
		atFdCwd,
		uintptr(unsafe.Pointer(dstCS)),
		cloneNoownerCopy,
		0,
	)
	if r1 != 0 || errno != 0 {
		return fmt.Errorf("clonefileat: %w", errno)
	}
	return nil
}

func copyFileIO(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
