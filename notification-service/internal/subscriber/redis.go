package subscriber

import (
	"context"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

const Channel = "filemanage:events"

type RedisSubscriber struct {
	client *redis.Client
}

func NewRedisSubscriber() *RedisSubscriber {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	return &RedisSubscriber{client: client}
}

// Subscribe returns a channel that emits raw JSON messages from Redis Pub/Sub.
// It runs until ctx is cancelled, then closes the returned channel.
func (s *RedisSubscriber) Subscribe(ctx context.Context) <-chan string {
	out := make(chan string, 64)
	go func() {
		defer close(out)
		sub := s.client.Subscribe(ctx, Channel)
		defer sub.Close()

		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				select {
				case out <- msg.Payload:
				default:
					log.Println("subscriber: slow consumer, dropping message")
				}
			}
		}
	}()
	return out
}

func (s *RedisSubscriber) Close() {
	s.client.Close()
}
