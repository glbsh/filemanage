package handler_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/glbsh/filemanage/storage-service/internal/events"
	"github.com/glbsh/filemanage/storage-service/internal/handler"
	"github.com/glbsh/filemanage/storage-service/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/minio/minio-go/v7"
)

// --- Fakes ---

type fakeMinio struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newFakeMinio() *fakeMinio {
	return &fakeMinio{data: make(map[string][]byte)}
}

func (f *fakeMinio) Upload(_ context.Context, id string, r io.Reader, _ int64, _ string) error {
	b, _ := io.ReadAll(r)
	f.mu.Lock()
	f.data[id] = b
	f.mu.Unlock()
	return nil
}

func (f *fakeMinio) Download(_ context.Context, id string) (io.ReadCloser, minio.ObjectInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	b, ok := f.data[id]
	if !ok {
		return nil, minio.ObjectInfo{}, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), minio.ObjectInfo{Key: id, Size: int64(len(b)), ContentType: "text/plain"}, nil
}

func (f *fakeMinio) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[id]; !ok {
		return fmt.Errorf("not found")
	}
	delete(f.data, id)
	return nil
}

func (f *fakeMinio) Stat(_ context.Context, id string) (minio.ObjectInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if _, ok := f.data[id]; !ok {
		return minio.ObjectInfo{}, fmt.Errorf("not found")
	}
	return minio.ObjectInfo{Key: id}, nil
}

// mockPublisher records every Publish call for assertion.
type mockPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (m *mockPublisher) Publish(_ context.Context, e events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockPublisher) received() []events.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]events.Event, len(m.events))
	copy(out, m.events)
	return out
}

// --- Helpers ---

func buildMultipart(filename string, content []byte) (*bytes.Buffer, string) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", "text/plain")
	part, _ := w.CreatePart(h)
	part.Write(content)
	w.Close()
	return body, w.FormDataContentType()
}

func newHandler(t *testing.T) (*handler.FileHandler, *fakeMinio, *mockPublisher) {
	t.Helper()
	store := newFakeMinio()
	svc := service.NewFileServiceWithStorage(store, "files", 4)
	pub := &mockPublisher{}
	h := handler.NewFileHandler(svc, pub)
	return h, store, pub
}

func uploadFile(t *testing.T, h *handler.FileHandler, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := buildMultipart(filename, content)
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.Upload(rec, req)
	return rec
}

// --- Upload handler tests ---

func TestUploadHandlerReturns201(t *testing.T) {
	h, _, _ := newHandler(t)
	rec := uploadFile(t, h, "test.txt", []byte("hello"))
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadHandlerPublishesEvent(t *testing.T) {
	h, _, pub := newHandler(t)
	uploadFile(t, h, "report.pdf", []byte("pdf bytes"))

	evts := pub.received()
	if len(evts) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(evts))
	}
	if evts[0].Event != "file.uploaded" {
		t.Errorf("expected event 'file.uploaded', got %q", evts[0].Event)
	}
	if evts[0].Filename != "report.pdf" {
		t.Errorf("expected filename 'report.pdf', got %q", evts[0].Filename)
	}
	if evts[0].ID == "" {
		t.Error("expected non-empty ID in event")
	}
}

func TestUploadHandlerMissingFileReturns400(t *testing.T) {
	h, _, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("no file here"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Upload(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// --- Download handler tests ---

func TestDownloadHandlerReturnsFileContent(t *testing.T) {
	h, _, _ := newHandler(t)
	content := []byte("the file content")
	rec := uploadFile(t, h, "data.txt", content)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d", rec.Code)
	}

	// Extract ID from response.
	body := rec.Body.String()
	// Quick parse: find "id":"<uuid>"
	idStart := strings.Index(body, `"id":"`) + 6
	idEnd := strings.Index(body[idStart:], `"`) + idStart
	id := body[idStart:idEnd]

	r := chi.NewRouter()
	r.Get("/file/{id}", h.Download)
	req := httptest.NewRequest(http.MethodGet, "/file/"+id, nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec2.Code)
	}
	if !bytes.Equal(rec2.Body.Bytes(), content) {
		t.Errorf("expected content %q, got %q", content, rec2.Body.Bytes())
	}
}

func TestDownloadHandlerNotFoundReturns404(t *testing.T) {
	h, _, _ := newHandler(t)
	r := chi.NewRouter()
	r.Get("/file/{id}", h.Download)
	req := httptest.NewRequest(http.MethodGet, "/file/nonexistent-id", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Delete handler tests ---

func TestDeleteHandlerReturns204(t *testing.T) {
	h, _, _ := newHandler(t)
	rec := uploadFile(t, h, "todelete.txt", []byte("bye"))

	body := rec.Body.String()
	idStart := strings.Index(body, `"id":"`) + 6
	idEnd := strings.Index(body[idStart:], `"`) + idStart
	id := body[idStart:idEnd]

	r := chi.NewRouter()
	r.Delete("/file/{id}", h.Delete)
	req := httptest.NewRequest(http.MethodDelete, "/file/"+id, nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)

	if rec2.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestDeleteHandlerPublishesEvent(t *testing.T) {
	h, _, pub := newHandler(t)
	rec := uploadFile(t, h, "todelete.txt", []byte("bye"))

	body := rec.Body.String()
	idStart := strings.Index(body, `"id":"`) + 6
	idEnd := strings.Index(body[idStart:], `"`) + idStart
	id := body[idStart:idEnd]

	r := chi.NewRouter()
	r.Delete("/file/{id}", h.Delete)
	req := httptest.NewRequest(http.MethodDelete, "/file/"+id, nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)

	evts := pub.received()
	// First event: file.uploaded; second: file.deleted
	if len(evts) != 2 {
		t.Fatalf("expected 2 events (upload + delete), got %d", len(evts))
	}
	if evts[1].Event != "file.deleted" {
		t.Errorf("expected 'file.deleted' event, got %q", evts[1].Event)
	}
	if evts[1].ID != id {
		t.Errorf("expected deleted ID %q, got %q", id, evts[1].ID)
	}
}

func TestDeleteHandlerNotFoundReturns404(t *testing.T) {
	h, _, _ := newHandler(t)
	r := chi.NewRouter()
	r.Delete("/file/{id}", h.Delete)
	req := httptest.NewRequest(http.MethodDelete, "/file/ghost-id", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Concurrent handler tests ---

func TestConcurrentUploadDeleteRetrieve(t *testing.T) {
	h, _, pub := newHandler(t)

	const n = 30
	ids := make([]string, n)
	var mu sync.Mutex
	var uploadWg sync.WaitGroup

	// Concurrently upload n files.
	uploadWg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer uploadWg.Done()
			rec := uploadFile(t, h, fmt.Sprintf("f%d.txt", i), []byte(fmt.Sprintf("content-%d", i)))
			if rec.Code != http.StatusCreated {
				t.Errorf("upload %d got %d", i, rec.Code)
				return
			}
			body := rec.Body.String()
			idStart := strings.Index(body, `"id":"`) + 6
			idEnd := strings.Index(body[idStart:], `"`) + idStart
			mu.Lock()
			ids[i] = body[idStart:idEnd]
			mu.Unlock()
		}(i)
	}
	uploadWg.Wait()

	// Concurrently retrieve all + delete first half.
	router := chi.NewRouter()
	router.Get("/file/{id}", h.Download)
	router.Delete("/file/{id}", h.Delete)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		id := ids[i]
		if id == "" {
			continue
		}
		// Retrieve.
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/file/"+id, nil)
			router.ServeHTTP(httptest.NewRecorder(), req)
		}(id)

		// Delete the first half.
		if i < n/2 {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodDelete, "/file/"+id, nil)
				router.ServeHTTP(httptest.NewRecorder(), req)
			}(id)
		}
	}
	wg.Wait()

	// Verify event counts: n uploads + n/2 deletes = n + n/2 events.
	evts := pub.received()
	uploaded := 0
	deleted := 0
	for _, e := range evts {
		switch e.Event {
		case "file.uploaded":
			uploaded++
		case "file.deleted":
			deleted++
		}
	}
	if uploaded != n {
		t.Errorf("expected %d file.uploaded events, got %d", n, uploaded)
	}
	if deleted != n/2 {
		t.Errorf("expected %d file.deleted events, got %d", n/2, deleted)
	}
}
