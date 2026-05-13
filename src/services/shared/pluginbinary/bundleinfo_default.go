//go:build !darwin

package pluginbinary

import "context"

func detectHelperAppInfo(context.Context, string) (string, string) {
	return "", ""
}
