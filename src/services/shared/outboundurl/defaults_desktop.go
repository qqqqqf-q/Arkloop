//go:build desktop

package outboundurl

func defaultProtectionEnabled() bool {
	return false
}

func defaultTrustFakeIP() bool {
	return false
}

func defaultAllowLoopbackHTTP() bool {
	return false
}
