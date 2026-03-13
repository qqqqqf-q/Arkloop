//go:build desktop

package auth

import (
	"os"
	"strings"

	"github.com/google/uuid"
)

// 桌面模式固定标识：单用户场景下跳过完整 JWT 流程，使用确定性 UUID 和固定 token。
var (
	DesktopUserID    = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	DesktopAccountID = uuid.MustParse("00000000-0000-4000-8000-000000000002")
	DesktopRole      = RoleAccountAdmin
)

const desktopTokenDefault = "arkloop-desktop-local-token"

// DesktopToken 返回桌面模式使用的固定 Bearer token。
// 优先读取 ARKLOOP_DESKTOP_TOKEN 环境变量，未设置时使用默认值。
func DesktopToken() string {
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN")); v != "" {
		return v
	}
	return desktopTokenDefault
}

// DesktopVerifiedAccessToken 返回桌面模式的固定验证结果，角色为 account_admin（等同 owner）。
func DesktopVerifiedAccessToken() VerifiedAccessToken {
	return VerifiedAccessToken{
		UserID:      DesktopUserID,
		AccountID:   DesktopAccountID,
		AccountRole: DesktopRole,
	}
}
