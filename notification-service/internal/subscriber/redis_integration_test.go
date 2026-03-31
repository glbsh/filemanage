//go:build integration

package subscriber_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/glbsh/filemanage/notification-service/internal/subscriber"
)

func redisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func TestRedisSubscriberReceivesMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub := subscriber.NewRedisSubscriber()
	defer sub.Close()

	messages := sub.Subscribe(ctx)

	// Publish a test message via a separate client.
	pub := redis.NewClient(&redis.Options{Addr: redisAddr()})
	defer pub.Close()

	payload := `{"event":"file.uploaded","id":"test-integration","filename":"integration.txt"}`
	if err := pub.Publish(ctx, "filemanage:events", payload).Err(); err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	select {
	case msg := <-messages:
		if msg != payload {
			t.Errorf("expected %q, got %q", payload, msg)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for message from Redis")
	}
}

func TestRedisSubscriberMultipleMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub := subscriber.NewRedisSubscriber()
	defer sub.Close()

	messages := sub.Subscribe(ctx)

	pub := redis.NewClient(&redis.Options{Addr: redisAddr()})
	defer pub.Close()

	payloads := []string{
		`{"event":"file.uploaded","id":"1"}`,
		`{"event":"file.deleted","id":"2"}`,
		`{"event":"metadata.created","id":"3"}`,
	}

	for _, p := range payloads {
		if err := pub.Publish(ctx, "filemanage:events", p).Err(); err != nil {
			t.Fatalf("failed to publish: %v", err)
		}
	}

	received := make([]string, 0, len(payloads))
	timeout := time.After(3 * time.Second)
	for len(received) < len(payloads) {
		select {
		case msg := <-messages:
			received = append(received, msg)
		case <-timeout:
			t.Errorf("timed out: received %d/%d messages", len(received), len(payloads))
			return
		}
	}
}

func TestRedisSubscriberCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	sub := subscriber.NewRedisSubscriber()
	defer sub.Close()

	messages := sub.Subscribe(ctx)
	cancel()

	// After cancellation the channel should close.
	select {
	case _, ok := <-messages:
		if ok {
			t.Error("expected channel to be closed after context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for channel to close after cancellation")
	}
}
