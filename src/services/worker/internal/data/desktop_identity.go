package data

import "github.com/google/uuid"

// DesktopUserID 是 desktop 单用户模式下 bot owner 的固定用户 ID。
// 与 api 侧 auth.DesktopUserID、desktop_seed.go 的种子行一致;
// worker 无法 import api/internal,在此单点定义,改动必须三处同步。
var DesktopUserID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
