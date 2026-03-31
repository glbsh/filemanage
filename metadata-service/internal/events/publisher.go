package events

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const Channel = "filemanage:events"

type Publisher struct {
	client *redis.Client
}

type Event struct {
	Event     string    `json:"event"`
	ID        string    `json:"id"`
	Filename  string    `json:"filename,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func NewPublisher() *Publisher {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	return &Publisher{client: client}
}

func (p *Publisher) Publish(ctx context.Context, event Event) {
	event.Timestamp = time.Now().UTC()
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("events: marshal error: %v", err)
		return
	}
	if err := p.client.Publish(ctx, Channel, data).Err(); err != nil {
		log.Printf("events: publish error: %v", err)
	}
}

func (p *Publisher) Close() {
	p.client.Close()
}
