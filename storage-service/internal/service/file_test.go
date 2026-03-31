package service_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"sync"
	"testing"

	"github.com/glbsh/filemanage/storage-service/internal/service"
	"github.com/minio/minio-go/v7"
)

// fakeMinio implements service.MinioStorage in-memory for unit tests.
type fakeMinio struct {
	mu   sync.RWMutex
	data map[string][]byte
	fail bool
}

func newFakeMinio() *fakeMinio {
	return &fakeMinio{data: make(map[string][]byte)}
}

func (f *fakeMinio) Upload(_ context.Context, id string, r io.Reader, _ int64, _ string) error {
	if f.fail {
		return fmt.Errorf("simulated upload failure")
	}
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
	return io.NopCloser(bytes.NewReader(b)), minio.ObjectInfo{Key: id, Size: int64(len(b))}, nil
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

func (f *fakeMinio) count() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.data)
}

// buildHeader creates a *multipart.FileHeader backed by real bytes.
func buildHeader(filename, contentType string, data []byte) *multipart.FileHeader {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", contentType)

	part, _ := w.CreatePart(h)
	part.Write(data)
	w.Close()

	mr := multipart.NewReader(body, w.Boundary())
	form, _ := mr.ReadForm(1 << 20)
	return form.File["file"][0]
}

// --- Upload tests ---

func TestUploadGeneratesUUID(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "files", 2)
	header := buildHeader("hello.txt", "text/plain", []byte("hello world"))

	res, err := svc.Upload(context.Background(), header)
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if res.ID == "" {
		t.Error("expected non-empty UUID")
	}
	if res.Filename != "hello.txt" {
		t.Errorf("expected filename 'hello.txt', got %q", res.Filename)
	}
	if res.Size != int64(len("hello world")) {
		t.Errorf("expected size %d, got %d", len("hello world"), res.Size)
	}
}

func TestUploadSetsLocation(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "mybucket", 2)
	header := buildHeader("doc.pdf", "application/pdf", []byte("pdf-content"))

	res, err := svc.Upload(context.Background(), header)
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	expected := "mybucket/" + res.ID
	if res.Location != expected {
		t.Errorf("expected location %q, got %q", expected, res.Location)
	}
}

func TestUploadUUIDsAreUnique(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "files", 4)

	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		header := buildHeader("file.txt", "text/plain", []byte("data"))
		res, err := svc.Upload(context.Background(), header)
		if err != nil {
			t.Fatalf("Upload() error: %v", err)
		}
		if ids[res.ID] {
			t.Errorf("duplicate UUID: %s", res.ID)
		}
		ids[res.ID] = true
	}
}

// --- Retrieve test ---

func TestDownloadReturnsUploadedContent(t *testing.T) {
	store := newFakeMinio()
	svc := service.NewFileServiceWithStorage(store, "files", 2)

	content := []byte("file content here")
	header := buildHeader("data.txt", "text/plain", content)
	res, err := svc.Upload(context.Background(), header)
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	rc, info, err := svc.Download(context.Background(), res.ID)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, content) {
		t.Errorf("expected content %q, got %q", content, got)
	}
	if info.Key != res.ID {
		t.Errorf("expected key %q, got %q", res.ID, info.Key)
	}
}

func TestDownloadNonExistentReturnsError(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "files", 2)
	_, _, err := svc.Download(context.Background(), "ghost-id")
	if err == nil {
		t.Error("expected error downloading non-existent file")
	}
}

// --- Delete tests ---

func TestDeleteExistingFile(t *testing.T) {
	store := newFakeMinio()
	svc := service.NewFileServiceWithStorage(store, "files", 2)

	header := buildHeader("todelete.txt", "text/plain", []byte("bye"))
	res, err := svc.Upload(context.Background(), header)
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	if err := svc.Delete(context.Background(), res.ID); err != nil {
		t.Errorf("Delete() error: %v", err)
	}

	// Confirm it's gone.
	_, _, err = svc.Download(context.Background(), res.ID)
	if err == nil {
		t.Error("expected error downloading deleted file")
	}
}

func TestDeleteNonExistentFile(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "files", 2)
	if err := svc.Delete(context.Background(), "non-existent-id"); err == nil {
		t.Error("expected error when deleting non-existent file")
	}
}

// --- Concurrent mixed-operation test ---

// TestConcurrentUploadRetrieveDelete fires concurrent uploads, retrieves, and
// deletes simultaneously and checks for data races and consistency.
func TestConcurrentUploadRetrieveDelete(t *testing.T) {
	store := newFakeMinio()
	svc := service.NewFileServiceWithStorage(store, "files", 10)

	const uploaders = 20
	const deleters = 10

	// Phase 1: upload files concurrently, collect IDs.
	ids := make([]string, uploaders)
	var uploadWg sync.WaitGroup
	var mu sync.Mutex

	uploadWg.Add(uploaders)
	for i := 0; i < uploaders; i++ {
		go func(i int) {
			defer uploadWg.Done()
			content := fmt.Sprintf("content-%d", i)
			header := buildHeader(fmt.Sprintf("file%d.txt", i), "text/plain", []byte(content))
			res, err := svc.Upload(context.Background(), header)
			if err != nil {
				t.Errorf("Upload(%d) error: %v", i, err)
				return
			}
			mu.Lock()
			ids[i] = res.ID
			mu.Unlock()
		}(i)
	}
	uploadWg.Wait()

	// Phase 2: concurrently retrieve all files and delete the first half,
	// while new uploads continue.
	var wg sync.WaitGroup

	// Retrieve all uploaded files.
	for i := 0; i < uploaders; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if id == "" {
				return
			}
			rc, _, err := svc.Download(context.Background(), id)
			if err != nil {
				// May have been deleted concurrently — acceptable.
				return
			}
			io.ReadAll(rc)
			rc.Close()
		}(ids[i])
	}

	// Delete first half concurrently.
	for i := 0; i < deleters; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if id == "" {
				return
			}
			svc.Delete(context.Background(), id) // ignore not-found errors
		}(ids[i])
	}

	// Upload more files while deletes + retrieves are in flight.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			header := buildHeader(fmt.Sprintf("extra%d.txt", i), "text/plain", []byte(fmt.Sprintf("extra-%d", i)))
			if _, err := svc.Upload(context.Background(), header); err != nil {
				t.Errorf("extra Upload(%d) error: %v", i, err)
			}
		}(i)
	}

	wg.Wait()

	// After deleting the first deleters files and adding 10 more, total should be
	// at least (uploaders - deleters) and at most (uploaders + 10).
	count := store.count()
	min := uploaders - deleters
	max := uploaders + 10
	if count < min || count > max {
		t.Errorf("unexpected file count: got %d, want [%d, %d]", count, min, max)
	}
}

// TestConcurrentUploadOnly is the original concurrency smoke test.
func TestConcurrentUploadOnly(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "files", 5)

	const n = 50
	results := make(chan error, n)

	for i := 0; i < n; i++ {
		go func(i int) {
			header := buildHeader(
				fmt.Sprintf("file%d.txt", i),
				"text/plain",
				[]byte(fmt.Sprintf("content-%d", i)),
			)
			_, err := svc.Upload(context.Background(), header)
			results <- err
		}(i)
	}

	for i := 0; i < n; i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent Upload() error: %v", err)
		}
	}
}
