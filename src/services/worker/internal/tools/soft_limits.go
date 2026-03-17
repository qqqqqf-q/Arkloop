package tools

import sharedexec "arkloop/services/shared/executionconfig"

type ToolSoftLimit = sharedexec.ToolSoftLimit

type PerToolSoftLimits = sharedexec.PerToolSoftLimits

const (
	DefaultExecCommandMaxOutputBytes  = sharedexec.DefaultExecCommandMaxOutputBytes
	DefaultWriteStdinMaxContinuations = sharedexec.DefaultWriteStdinMaxContinuations
	DefaultWriteStdinMaxYieldTimeMs   = sharedexec.DefaultWriteStdinMaxYieldTimeMs
	DefaultWriteStdinMaxOutputBytes   = sharedexec.DefaultWriteStdinMaxOutputBytes
	DefaultGenericMaxOutputBytes      = sharedexec.DefaultGenericMaxOutputBytes
	HardMaxToolSoftLimitContinuations = sharedexec.HardMaxToolSoftLimitContinuations
	HardMaxToolSoftLimitYieldTimeMs   = sharedexec.HardMaxToolSoftLimitYieldTimeMs
	HardMaxToolSoftLimitOutputBytes   = sharedexec.HardMaxToolSoftLimitOutputBytes

	// CompressTargetBytes is the maximum ResultJSON size (in bytes) we want to
	// send to the LLM. Kept separate from MaxOutputBytes (raw truncation limit)
	// so that CompressResult triggers independently of executor-level limits.
	CompressTargetBytes = 4 * 1024
)

func resolveOutputLimit(limits PerToolSoftLimits, toolName string) int {
	if limits != nil {
		if l, ok := limits[toolName]; ok && l.MaxOutputBytes != nil {
			return *l.MaxOutputBytes
		}
	}
	return DefaultGenericMaxOutputBytes
}

func DefaultPerToolSoftLimits() PerToolSoftLimits {
	return sharedexec.DefaultPerToolSoftLimits()
}

func CopyPerToolSoftLimits(src PerToolSoftLimits) PerToolSoftLimits {
	return sharedexec.CopyPerToolSoftLimits(src)
}

func ResolveToolSoftLimit(limits PerToolSoftLimits, toolName string) ToolSoftLimit {
	return sharedexec.ResolveToolSoftLimit(limits, toolName)
}

func MergePerToolSoftLimits(base, override PerToolSoftLimits) PerToolSoftLimits {
	return sharedexec.MergePerToolSoftLimits(base, override)
}
