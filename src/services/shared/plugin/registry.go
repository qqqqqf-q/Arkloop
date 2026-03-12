package plugin

import "sync"

var (
	mu sync.RWMutex

	billingProvider BillingProvider
	authProviders   = map[string]AuthProvider{}
	notifyChannels  = map[string]NotificationChannel{}
	auditSinks      []AuditSink
)

// RegisterBillingProvider 注册计费实现，覆盖 OSS 默认。
// 每个进程只允许一个 BillingProvider。
func RegisterBillingProvider(p BillingProvider) {
	mu.Lock()
	defer mu.Unlock()
	billingProvider = p
}

// RegisterAuthProvider 注册认证实现。name 为 provider ID (如 "oidc")。
// 可注册多个，运行时通过配置激活。
func RegisterAuthProvider(name string, p AuthProvider) {
	mu.Lock()
	defer mu.Unlock()
	authProviders[name] = p
}

// RegisterNotificationChannel 注册通知渠道。name 为渠道 ID (如 "slack")。
func RegisterNotificationChannel(name string, ch NotificationChannel) {
	mu.Lock()
	defer mu.Unlock()
	notifyChannels[name] = ch
}

// RegisterAuditSink 注册审计日志输出。可注册多个，事件写入所有 sink。
func RegisterAuditSink(s AuditSink) {
	mu.Lock()
	defer mu.Unlock()
	auditSinks = append(auditSinks, s)
}

// GetBillingProvider 返回当前 BillingProvider。
// 未注册时返回 nil（F3 中会提供 OSS 默认实现）。
func GetBillingProvider() BillingProvider {
	mu.RLock()
	defer mu.RUnlock()
	return billingProvider
}

// GetAuthProvider 按名称获取 AuthProvider。
func GetAuthProvider(name string) (AuthProvider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := authProviders[name]
	return p, ok
}

// ListNotificationChannels 返回所有已注册通知渠道的副本。
func ListNotificationChannels() map[string]NotificationChannel {
	mu.RLock()
	defer mu.RUnlock()
	result := make(map[string]NotificationChannel, len(notifyChannels))
	for k, v := range notifyChannels {
		result[k] = v
	}
	return result
}

// GetAuditSinks 返回所有已注册审计 sink 的副本。
func GetAuditSinks() []AuditSink {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]AuditSink, len(auditSinks))
	copy(result, auditSinks)
	return result
}

// resetForTesting 重置所有注册状态，仅供测试使用。
func resetForTesting() {
	mu.Lock()
	defer mu.Unlock()
	billingProvider = nil
	authProviders = map[string]AuthProvider{}
	notifyChannels = map[string]NotificationChannel{}
	auditSinks = nil
}
