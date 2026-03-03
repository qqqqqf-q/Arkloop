package consumer

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	notifyChannel  = "arkloop:jobs"
	reconnectDelay = 2 * time.Second
)

// Notifier 通过 PostgreSQL LISTEN/NOTIFY 接收新 job 信号。
// 空闲 worker goroutine 等待 Wake() 返回的 channel 而非 busy-polling。
type Notifier struct {
	pool *pgxpool.Pool

	mu       sync.Mutex
	waiters  []chan struct{}
	running  bool
}

func NewNotifier(directPool *pgxpool.Pool) *Notifier {
	return &Notifier{pool: directPool}
}

// Start 在后台启动 LISTEN 循环。ctx 取消后退出。
func (n *Notifier) Start(ctx context.Context) {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return
	}
	n.running = true
	n.mu.Unlock()

	go n.loop(ctx)
}

// Wake 返回一个 channel，收到信号时表示可能有新 job。
// 每次调用返回独立的 channel，信号触发后自动关闭。
func (n *Notifier) Wake() <-chan struct{} {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.waiters = append(n.waiters, ch)
	n.mu.Unlock()
	return ch
}

func (n *Notifier) broadcast() {
	n.mu.Lock()
	waiters := n.waiters
	n.waiters = nil
	n.mu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
		close(ch)
	}
}

func (n *Notifier) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		n.listen(ctx)

		// 连接断开，唤醒所有 waiter 让他们做一次 fallback poll
		n.broadcast()

		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (n *Notifier) listen(ctx context.Context) {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `LISTEN "`+notifyChannel+`"`)
	if err != nil {
		return
	}

	for {
		// WaitForNotification 阻塞直到有通知或超时
		_, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return
		}
		n.broadcast()
	}
}
