package accesslog

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	StreamKey     = "arkloop:gateway:access_log"
	streamMaxLen  = 10000
	queueSize     = 4096
	batchSize     = 64
	flushInterval = 10 * time.Millisecond
	writeTimeout  = 20 * time.Millisecond
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
	ch  chan Entry
}

func NewWriter(rdb *redis.Client) *Writer {
	w := &Writer{
		rdb: rdb,
		ch:  make(chan Entry, queueSize),
	}
	go w.loop()
	return w
}

// Write 将一条记录写入队列，满了就丢弃。
func (w *Writer) Write(entry Entry) {
	if w == nil || w.rdb == nil {
		return
	}

	select {
	case w.ch <- entry:
	default:
	}
}

func (w *Writer) loop() {
	if w == nil || w.rdb == nil || w.ch == nil {
		return
	}

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]Entry, 0, batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()

		entries := batch
		batch = batch[:0]

		_, _ = w.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for _, entry := range entries {
				pipe.XAdd(ctx, &redis.XAddArgs{
					Stream: StreamKey,
					MaxLen: streamMaxLen,
					Approx: true,
					Limit:  100,
					Values: []string{
						"ts", entry.Timestamp,
						"trace_id", entry.TraceID,
						"method", entry.Method,
						"path", entry.Path,
						"status", strconv.Itoa(entry.StatusCode),
						"duration_ms", strconv.FormatInt(entry.DurationMs, 10),
						"client_ip", entry.ClientIP,
						"country", entry.Country,
						"city", entry.City,
						"user_agent", entry.UserAgent,
						"ua_type", entry.UAType,
						"risk_score", strconv.Itoa(entry.RiskScore),
						"identity_type", entry.IdentityType,
						"org_id", entry.OrgID,
						"user_id", entry.UserID,
					},
				})
			}
			return nil
		})
	}

	for {
		select {
		case entry := <-w.ch:
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
