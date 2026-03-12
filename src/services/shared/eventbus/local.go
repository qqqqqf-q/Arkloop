package eventbus

import (
	"context"
	"sync"
)

const localSubscriptionBuffer = 64

// LocalEventBus implements EventBus as an in-process pub/sub using Go channels.
// Intended for development, testing, and single-process (Desktop) deployments.
type LocalEventBus struct {
	mu     sync.RWMutex
	subs   map[string][]*localSubscription
	closed bool
}

// NewLocalEventBus creates an in-process EventBus.
func NewLocalEventBus() *LocalEventBus {
	return &LocalEventBus{
		subs: make(map[string][]*localSubscription),
	}
}

func (b *LocalEventBus) Publish(_ context.Context, topic string, payload string) error {
	msg := Message{Topic: topic, Payload: payload}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil
	}

	for _, sub := range b.subs[topic] {
		sub.send(msg)
	}
	return nil
}

func (b *LocalEventBus) Subscribe(_ context.Context, topic string) (Subscription, error) {
	sub := &localSubscription{
		ch:    make(chan Message, localSubscriptionBuffer),
		topic: topic,
		bus:   b,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		close(sub.ch)
		return sub, nil
	}

	b.subs[topic] = append(b.subs[topic], sub)
	return sub, nil
}

func (b *LocalEventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	b.closed = true

	for topic, subs := range b.subs {
		for _, sub := range subs {
			close(sub.ch)
		}
		delete(b.subs, topic)
	}
	return nil
}

// removeSub removes a subscription from the bus (called by localSubscription.Close).
func (b *LocalEventBus) removeSub(sub *localSubscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subs[sub.topic]
	for i, s := range subs {
		if s == sub {
			b.subs[sub.topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(b.subs[sub.topic]) == 0 {
		delete(b.subs, sub.topic)
	}
}

type localSubscription struct {
	ch    chan Message
	topic string
	bus   *LocalEventBus
	once  sync.Once
}

func (s *localSubscription) Channel() <-chan Message {
	return s.ch
}

func (s *localSubscription) Close() error {
	s.once.Do(func() {
		s.bus.removeSub(s)
		close(s.ch)
	})
	return nil
}

// send attempts a non-blocking send. If the subscriber's buffer is full
// the message is dropped (fire-and-forget, matching Redis Pub/Sub semantics).
func (s *localSubscription) send(msg Message) {
	select {
	case s.ch <- msg:
	default:
	}
}
