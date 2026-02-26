package accesslog

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	StreamKey      = "arkloop:gateway:access_log"
	streamMaxLen   = 10000
	writeTimeout   = 500 * time.Millisecond
)

// Entry 是一条访问日志记录。
type Entry struct {
	Timestamp    string // RFC3339
	TraceID      string
	Method       string
	Path         string
	StatusCode   int
	DurationMs   int64
	ClientIP     string
	Country      string
	City         string
	UserAgent    string
	UAType       string
	RiskScore    int
	IdentityType string // jwt / api_key / anonymous
	OrgID        string
	UserID       string
}

// Writer 将访问日志写入 Redis Stream。
type Writer struct {
	rdb *redis.Client
}

func NewWriter(rdb *redis.Client) *Writer {
	return &Writer{rdb: rdb}
}

// Write 将一条记录写入 Redis Stream，非阻塞，失败忽略（不影响请求处理）。
func (w *Writer) Write(entry Entry) {
	if w == nil || w.rdb == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamKey,
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]any{
			"ts":            entry.Timestamp,
			"trace_id":      entry.TraceID,
			"method":        entry.Method,
			"path":          entry.Path,
			"status":        fmt.Sprintf("%d", entry.StatusCode),
			"duration_ms":   fmt.Sprintf("%d", entry.DurationMs),
			"client_ip":     entry.ClientIP,
			"country":       entry.Country,
			"city":          entry.City,
			"user_agent":    entry.UserAgent,
			"ua_type":       entry.UAType,
			"risk_score":    fmt.Sprintf("%d", entry.RiskScore),
			"identity_type": entry.IdentityType,
			"org_id":        entry.OrgID,
			"user_id":       entry.UserID,
		},
	})
}
