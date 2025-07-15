package firecracker

import "fmt"

// TierResources 定义单个 tier 的 Firecracker microVM 资源配额。
type TierResources struct {
	VCPUCount  int64
	MemSizeMiB int64
}

// KernelArgs 是所有 tier 共用的内核启动参数。
const KernelArgs = "console=ttyS0 reboot=k panic=1 pci=off nomodules rw"

var tiers = map[string]TierResources{
	"lite": {VCPUCount: 1, MemSizeMiB: 256},
	"pro":  {VCPUCount: 1, MemSizeMiB: 1024},
}

// TierFor 返回指定 tier 的资源配额，未知 tier 返回 lite 配额。
func TierFor(tier string) TierResources {
	if cfg, ok := tiers[tier]; ok {
		return cfg
	}
	return tiers["lite"]
}

// ValidTier 验证 tier 值是否合法。
func ValidTier(tier string) error {
	switch tier {
	case "lite", "pro":
		return nil
	default:
		return fmt.Errorf("unknown tier %q: must be lite or pro", tier)
	}
}
