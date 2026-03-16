package consumer

import "sync"

// LocalNotifier is a pure in-memory WorkNotifier.
// Call Notify() to wake all goroutines blocked on Wake().
type LocalNotifier struct {
	mu      sync.Mutex
	waiters []chan struct{}
}

func NewLocalNotifier() *LocalNotifier {
	return &LocalNotifier{}
}

// Wake returns a channel that closes when Notify is called.
func (n *LocalNotifier) Wake() <-chan struct{} {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.waiters = append(n.waiters, ch)
	n.mu.Unlock()
	return ch
}

// Notify wakes all goroutines currently waiting on Wake().
func (n *LocalNotifier) Notify() {
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
