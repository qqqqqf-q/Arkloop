package eventbus

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisEventBus implements EventBus using Redis Pub/Sub.
type RedisEventBus struct {
	rdb *redis.Client
}

// NewRedisEventBus creates an EventBus backed by Redis Pub/Sub.
// The caller retains ownership of rdb and is responsible for closing it.
func NewRedisEventBus(rdb *redis.Client) *RedisEventBus {
	return &RedisEventBus{rdb: rdb}
}

func (b *RedisEventBus) Publish(ctx context.Context, topic string, payload string) error {
	return b.rdb.Publish(ctx, topic, payload).Err()
}

func (b *RedisEventBus) Subscribe(ctx context.Context, topic string) (Subscription, error) {
	sub := b.rdb.Subscribe(ctx, topic)
	// Wait for Redis to confirm the subscription is active.
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("eventbus: redis subscribe %q: %w", topic, err)
	}
	return &redisSubscription{sub: sub}, nil
}

func (b *RedisEventBus) Close() error {
	return nil // does not own the Redis client
}

type redisSubscription struct {
	sub *redis.PubSub
}

func (s *redisSubscription) Channel() <-chan Message {
	ch := make(chan Message, 1)
	go func() {
		defer close(ch)
		msgCh := s.sub.Channel()
		for msg := range msgCh {
			ch <- Message{Topic: msg.Channel, Payload: msg.Payload}
		}
	}()
	return ch
}

func (s *redisSubscription) Close() error {
	return s.sub.Close()
}
