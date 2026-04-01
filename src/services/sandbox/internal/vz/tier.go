//go:build darwin && cgo

package vz

import "arkloop/services/sandbox/internal/session"

// TierResources defines the vCPU and memory allocation for a Vz VM tier.
type TierResources struct {
	CPUCount  uint
	MemoryMiB uint64
}

var tierResources = map[string]TierResources{
	session.TierLite:    {CPUCount: 1, MemoryMiB: 256},
	session.TierPro:     {CPUCount: 1, MemoryMiB: 1024},
	session.TierBrowser: {CPUCount: 1, MemoryMiB: 512},
}

func resourcesFor(tier string) TierResources {
	if r, ok := tierResources[tier]; ok {
		return r
	}
	return tierResources[session.TierLite]
}
