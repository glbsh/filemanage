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

func (f *fakeMinio) Download(_ context.Context, id string) (*minio.Object, error) {
	// Real minio.Object cannot be constructed in unit tests; return error to exercise error path.
	return nil, fmt.Errorf("not implemented in fake")
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

func TestUploadConcurrency(t *testing.T) {
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
}

func TestDeleteNonExistentFile(t *testing.T) {
	svc := service.NewFileServiceWithStorage(newFakeMinio(), "files", 2)
	err := svc.Delete(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("expected error when deleting non-existent file")
	}
}
