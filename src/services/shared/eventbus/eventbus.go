package eventbus

import "context"

// Message represents a message received from a subscription.
type Message struct {
	Topic   string
	Payload string
}

// Subscription represents an active subscription to a topic.
// Callers must call Close when done to release resources.
type Subscription interface {
	// Channel returns a receive-only channel that delivers messages.
	// The channel is closed when the subscription is closed or the
	// underlying transport disconnects.
	Channel() <-chan Message
	// Close unsubscribes and releases all resources.
	Close() error
}

// EventBus provides topic-based publish/subscribe messaging.
type EventBus interface {
	// Publish sends a payload to all current subscribers of the given topic.
	Publish(ctx context.Context, topic string, payload string) error
	// Subscribe creates a subscription to the given topic. The subscription
	// is active when Subscribe returns (no additional handshake needed).
	Subscribe(ctx context.Context, topic string) (Subscription, error)
	// Close shuts down the event bus and releases resources.
	Close() error
}
