package handler_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glbsh/filemanage/notification-service/internal/handler"
)

// fakeSource implements MessageSource for unit tests using a buffered channel.
type fakeSource struct {
	ch chan string
}

func newFakeSource(messages ...string) *fakeSource {
	ch := make(chan string, len(messages)+1)
	for _, m := range messages {
		ch <- m
	}
	return &fakeSource{ch: ch}
}

func (f *fakeSource) Subscribe(ctx context.Context) <-chan string {
	out := make(chan string, cap(f.ch))
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-f.ch:
				if !ok {
					return
				}
				out <- msg
			}
		}
	}()
	return out
}

func TestStreamSendsConnectedComment(t *testing.T) {
	src := newFakeSource()
	h := handler.NewEventsHandler(src)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Stream(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("expected SSE comment ': connected', got:\n%s", body)
	}
}

func TestStreamSetsSSEHeaders(t *testing.T) {
	src := newFakeSource()
	h := handler.NewEventsHandler(src)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Stream(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control 'no-cache', got %q", cc)
	}
}

func TestStreamForwardsMessages(t *testing.T) {
	payload := `{"event":"file.uploaded","id":"abc","filename":"test.txt","timestamp":"2024-01-01T00:00:00Z"}`
	src := newFakeSource(payload)
	h := handler.NewEventsHandler(src)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Stream(rec, req)

	body := rec.Body.String()
	expected := "data: " + payload
	if !strings.Contains(body, expected) {
		t.Errorf("expected SSE data line %q in body:\n%s", expected, body)
	}
}

func TestStreamForwardsMultipleMessages(t *testing.T) {
	events := []string{
		`{"event":"file.uploaded","id":"1"}`,
		`{"event":"file.deleted","id":"2"}`,
		`{"event":"metadata.created","id":"3"}`,
	}
	src := newFakeSource(events...)
	h := handler.NewEventsHandler(src)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Stream(rec, req)

	body := rec.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(body))
	dataLines := 0
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data:") {
			dataLines++
		}
	}
	if dataLines != len(events) {
		t.Errorf("expected %d data lines, got %d\nbody:\n%s", len(events), dataLines, body)
	}
}

func TestStreamAllEventTypes(t *testing.T) {
	payloads := []string{
		`{"event":"file.uploaded","id":"1","filename":"a.txt","timestamp":"2024-01-01T00:00:00Z"}`,
		`{"event":"file.deleted","id":"2","timestamp":"2024-01-01T00:00:00Z"}`,
		`{"event":"metadata.created","id":"3","filename":"a.txt","timestamp":"2024-01-01T00:00:00Z"}`,
	}
	src := newFakeSource(payloads...)
	h := handler.NewEventsHandler(src)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	body := rec.Body.String()
	for _, p := range payloads {
		if !strings.Contains(body, p) {
			t.Errorf("missing event payload in SSE output: %s", p)
		}
	}
}

func TestStreamConcurrentClients(t *testing.T) {
	// Verify the handler is safe to call from multiple goroutines simultaneously.
	const clients = 10
	var wg sync.WaitGroup
	wg.Add(clients)
	for i := 0; i < clients; i++ {
		go func() {
			defer wg.Done()
			src := newFakeSource(`{"event":"file.uploaded","id":"x"}`)
			h := handler.NewEventsHandler(src)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
			h.Stream(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
}

func TestStreamDisconnectsOnContextCancel(t *testing.T) {
	src := newFakeSource()
	h := handler.NewEventsHandler(src)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.Stream(rec, req)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Error("Stream did not exit after context cancellation")
	}
}
