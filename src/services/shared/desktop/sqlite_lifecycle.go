package desktop

import (
	"sync"
	"sync/atomic"
)

var (
	sidecarProcess atomic.Bool

	sqliteCloseMu sync.Mutex
	sqliteCloseFn func() error
)

// SetSidecarProcess 由 desktop 侧car main 在 InitDesktopInfra 之后置为 true；
// 单独跑 api cmd 时不调用，SQLite 仍由 RunDesktop defer 关闭。
func SetSidecarProcess(v bool) {
	sidecarProcess.Store(v)
}

// SidecarProcess 为 true 时表示 API 与 Worker 同属侧car，SQLite 关闭由 main 在排空 Worker 后触发。
func SidecarProcess() bool {
	return sidecarProcess.Load()
}

// RegisterSQLiteCloser 在 sqlite AutoMigrate 成功后注册。
func RegisterSQLiteCloser(fn func() error) {
	sqliteCloseMu.Lock()
	defer sqliteCloseMu.Unlock()
	sqliteCloseFn = fn
}

// CloseRegisteredSQLite 执行注册的关闭函数并清空注册。
func CloseRegisteredSQLite() error {
	sqliteCloseMu.Lock()
	fn := sqliteCloseFn
	sqliteCloseFn = nil
	sqliteCloseMu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}
