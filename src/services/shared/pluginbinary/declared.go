package pluginbinary

import (
	"arkloop/services/shared/pluginmanifest"
)

func ResolveDeclaredRuntime(runtime pluginmanifest.RuntimeConfig, opts DetectOptions) (DetectResult, bool) {
	if runtime.Path == "" {
		return DetectResult{}, false
	}
	resolvedPath, err := pluginmanifest.ResolveString(runtime.Path, opts.Resolver)
	if err != nil {
		return DetectResult{Status: StatusError, Error: err.Error()}, true
	}
	resolvedPath = resolveInstallPath(opts.InstallRoot, resolvedPath)
	return DetectResult{
		Status:        StatusMissing,
		Path:          resolvedPath,
		HelperAppPath: detectHelperAppPath(resolvedPath),
	}, true
}
