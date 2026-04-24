package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocalEventBus_PublishSubscribe(t *testing.T) {
	bus := NewLocalEventBus()
	defer func() { _ = bus.Close() }()

	sub, err := bus.Subscribe(context.Background(), "topic1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()

	if err := bus.Publish(context.Background(), "topic1", "hello"); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-sub.Channel():
		if msg.Topic != "topic1" {
			t.Errorf("got topic %q, want %q", msg.Topic, "topic1")
		}
		if msg.Payload != "hello" {
			t.Errorf("got payload %q, want %q", msg.Payload, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestLocalEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewLocalEventBus()
	defer func() { _ = bus.Close() }()

	sub1, _ := bus.Subscribe(context.Background(), "topic1")
	defer func() { _ = sub1.Close() }()
	sub2, _ := bus.Subscribe(context.Background(), "topic1")
	defer func() { _ = sub2.Close() }()

	if err := bus.Publish(context.Background(), "topic1", "broadcast"); err != nil {
		t.Fatal(err)
	}

	for _, sub := range []Subscription{sub1, sub2} {
		select {
		case msg := <-sub.Channel():
			if msg.Payload != "broadcast" {
				t.Errorf("got %q, want %q", msg.Payload, "broadcast")
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
}

func TestLocalEventBus_TopicIsolation(t *testing.T) {
	bus := NewLocalEventBus()
	defer func() { _ = bus.Close() }()

	sub1, _ := bus.Subscribe(context.Background(), "topicA")
	defer func() { _ = sub1.Close() }()
	sub2, _ := bus.Subscribe(context.Background(), "topicB")
	defer func() { _ = sub2.Close() }()

	if err := bus.Publish(context.Background(), "topicA", "only-A"); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-sub1.Channel():
		if msg.Payload != "only-A" {
			t.Errorf("got %q, want %q", msg.Payload, "only-A")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	select {
	case <-sub2.Channel():
		t.Fatal("topicB subscriber should not receive topicA message")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestLocalEventBus_Unsubscribe(t *testing.T) {
	bus := NewLocalEventBus()
	defer func() { _ = bus.Close() }()

	sub, _ := bus.Subscribe(context.Background(), "topic1")
	_ = sub.Close()

	// Channel should be closed after unsubscribe
	_, ok := <-sub.Channel()
	if ok {
		t.Fatal("channel should be closed after Close()")
	}

	// Publishing after unsubscribe should not panic
	if err := bus.Publish(context.Background(), "topic1", "after-unsub"); err != nil {
		t.Fatal(err)
	}
}

func TestLocalEventBus_CloseAll(t *testing.T) {
	bus := NewLocalEventBus()

	sub1, _ := bus.Subscribe(context.Background(), "topic1")
	sub2, _ := bus.Subscribe(context.Background(), "topic2")

	_ = bus.Close()

	// All subscription channels should be closed
	if _, ok := <-sub1.Channel(); ok {
		t.Fatal("sub1 channel should be closed")
	}
	if _, ok := <-sub2.Channel(); ok {
		t.Fatal("sub2 channel should be closed")
	}

	// Publish after close should not panic or error
	if err := bus.Publish(context.Background(), "topic1", "after-close"); err != nil {
		t.Fatal(err)
	}
}

func TestLocalEventBus_ConcurrentAccess(t *testing.T) {
	bus := NewLocalEventBus()
	defer func() { _ = bus.Close() }()

	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent subscribe
	subs := make([]Subscription, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := bus.Subscribe(context.Background(), "concurrent")
			if err != nil {
				t.Errorf("subscribe error: %v", err)
				return
			}
			subs[idx] = s
		}(i)
	}
	wg.Wait()

	// Concurrent publish
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := bus.Publish(context.Background(), "concurrent", "msg"); err != nil {
				t.Errorf("publish: %v", err)
			}
		}()
	}
	wg.Wait()

	// Concurrent unsubscribe
	for i := 0; i < goroutines; i++ {
		if subs[i] != nil {
			wg.Add(1)
			go func(s Subscription) {
				defer wg.Done()
				_ = s.Close()
			}(subs[i])
		}
	}
	wg.Wait()
}

func TestLocalEventBus_DoubleClose(t *testing.T) {
	bus := NewLocalEventBus()
	defer func() { _ = bus.Close() }()

	sub, _ := bus.Subscribe(context.Background(), "topic1")

	// Double close should not panic
	_ = sub.Close()
	_ = sub.Close()
}

func TestLocalEventBus_SubscribeAfterClose(t *testing.T) {
	bus := NewLocalEventBus()
	_ = bus.Close()

	sub, err := bus.Subscribe(context.Background(), "topic1")
	if err != nil {
		t.Fatal(err)
	}

	// Channel should be immediately closed
	_, ok := <-sub.Channel()
	if ok {
		t.Fatal("channel should be closed for subscription on closed bus")
	}
}
