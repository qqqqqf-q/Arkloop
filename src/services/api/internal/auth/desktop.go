//go:build desktop

package auth

import (
	"os"
	"strings"

	"github.com/google/uuid"
)

// 桌面模式固定标识：本机 local trust 会换取这个 owner 的正常 session。
var (
	DesktopUserID    = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	DesktopAccountID = uuid.MustParse("00000000-0000-4000-8000-000000000002")
	DesktopRole      = RolePlatformAdmin
)

func DesktopPreferredUsername() string {
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_OS_USERNAME")); v != "" {
		return v
	}
	return "desktop"
}

// DesktopToken 返回桌面模式使用的 Bearer token。
// 必须通过 ARKLOOP_DESKTOP_TOKEN 环境变量提供，未设置时 panic。
func DesktopToken() string {
	v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN"))
	if v == "" {
		panic("ARKLOOP_DESKTOP_TOKEN is required for desktop build")
	}
	return v
}

func DesktopTokenMatches(token string) bool {
	v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN"))
	return v != "" && strings.TrimSpace(token) == v
}
