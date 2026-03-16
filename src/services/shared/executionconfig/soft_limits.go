package executionconfig

type ToolSoftLimit struct {
	MaxContinuations *int `json:"max_continuations,omitempty"`
	MaxYieldTimeMs   *int `json:"max_yield_time_ms,omitempty"`
	MaxOutputBytes   *int `json:"max_output_bytes,omitempty"`
}

type PerToolSoftLimits map[string]ToolSoftLimit

const (
	DefaultExecCommandMaxOutputBytes  = 16 * 1024
	DefaultWriteStdinMaxContinuations = 16
	DefaultWriteStdinMaxYieldTimeMs   = 5_000
	DefaultWriteStdinMaxOutputBytes   = 16 * 1024
	DefaultGenericMaxOutputBytes      = 32 * 1024
	HardMaxToolSoftLimitContinuations = 256
	HardMaxToolSoftLimitYieldTimeMs   = 30_000
	HardMaxToolSoftLimitOutputBytes   = 65_536
)

func DefaultPerToolSoftLimits() PerToolSoftLimits {
	return PerToolSoftLimits{
		"exec_command": {
			MaxOutputBytes: intPtr(DefaultExecCommandMaxOutputBytes),
		},
		"write_stdin": {
			MaxContinuations: intPtr(DefaultWriteStdinMaxContinuations),
			MaxYieldTimeMs:   intPtr(DefaultWriteStdinMaxYieldTimeMs),
			MaxOutputBytes:   intPtr(DefaultWriteStdinMaxOutputBytes),
		},
	}
}

func CopyPerToolSoftLimits(src PerToolSoftLimits) PerToolSoftLimits {
	if len(src) == 0 {
		return PerToolSoftLimits{}
	}
	out := make(PerToolSoftLimits, len(src))
	for toolName, limit := range src {
		out[toolName] = ToolSoftLimit{
			MaxContinuations: copyOptionalInt(limit.MaxContinuations),
			MaxYieldTimeMs:   copyOptionalInt(limit.MaxYieldTimeMs),
			MaxOutputBytes:   copyOptionalInt(limit.MaxOutputBytes),
		}
	}
	return out
}

func ResolveToolSoftLimit(limits PerToolSoftLimits, toolName string) ToolSoftLimit {
	if limits == nil {
		return ToolSoftLimit{}
	}
	return limits[toolName]
}

func MergePerToolSoftLimits(base, override PerToolSoftLimits) PerToolSoftLimits {
	out := CopyPerToolSoftLimits(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = PerToolSoftLimits{}
	}
	for toolName, limit := range override {
		merged := out[toolName]
		if limit.MaxContinuations != nil {
			merged.MaxContinuations = copyOptionalInt(limit.MaxContinuations)
		}
		if limit.MaxYieldTimeMs != nil {
			merged.MaxYieldTimeMs = copyOptionalInt(limit.MaxYieldTimeMs)
		}
		if limit.MaxOutputBytes != nil {
			merged.MaxOutputBytes = copyOptionalInt(limit.MaxOutputBytes)
		}
		out[toolName] = merged
	}
	return out
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func intPtr(value int) *int {
	return &value
}
