package scenarios

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"arkloop/tests/bench/internal/httpx"
)

func addNetErrorKind(counts *sync.Map, err error) {
	if counts == nil || err == nil {
		return
	}
	if _, ok := err.(*httpx.HTTPError); ok {
		return
	}

	kind := netErrorKind(err)
	if kind == "" {
		return
	}
	ptrAny, _ := counts.LoadOrStore(kind, new(int64))
	ptr, _ := ptrAny.(*int64)
	if ptr == nil {
		return
	}
	atomic.AddInt64(ptr, 1)
}

func snapshotNetErrorKinds(counts *sync.Map) map[string]int64 {
	if counts == nil {
		return nil
	}
	out := map[string]int64{}
	counts.Range(func(key, value any) bool {
		k, _ := key.(string)
		ptr, _ := value.(*int64)
		if k == "" || ptr == nil {
			return true
		}
		out[k] = atomic.LoadInt64(ptr)
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func netErrorKind(err error) string {
	if err == nil {
		return ""
	}

	// url.Error 通常包一层，先尽量解一解。
	for {
		var uerr *url.Error
		if errors.As(err, &uerr) && uerr != nil && uerr.Err != nil {
			err = uerr.Err
			continue
		}
		break
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, io.EOF):
		return "eof"
	}

	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "timeout"
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "connection refused"):
		return "conn_refused"
	case strings.Contains(msg, "connection reset"):
		return "conn_reset"
	case strings.Contains(msg, "broken pipe"):
		return "broken_pipe"
	case strings.Contains(msg, "too many open files"):
		return "too_many_open_files"
	case strings.Contains(msg, "cannot assign requested address"):
		return "no_ephemeral_ports"
	case strings.Contains(msg, "no such host"):
		return "dns_no_such_host"
	case strings.Contains(msg, "i/o timeout"):
		return "timeout"
	}

	// 类型信息比原始 message 稳定，便于聚合。
	return strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
}
