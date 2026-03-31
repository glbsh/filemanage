package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// MessageSource is anything that can provide a stream of raw event strings.
type MessageSource interface {
	Subscribe(ctx context.Context) <-chan string
}

type EventsHandler struct {
	source MessageSource
}

func NewEventsHandler(source MessageSource) *EventsHandler {
	return &EventsHandler{source: source}
}

// Stream handles GET /events — an SSE endpoint that forwards Redis messages to the client.
func (h *EventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send an initial comment so the client knows the stream is live.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	messages := h.source.Subscribe(ctx)

	// Keep-alive ticker: SSE clients disconnect on long silences.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case msg, ok := <-messages:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
			log.Printf("sse: forwarded event to client")
		}
	}
}
