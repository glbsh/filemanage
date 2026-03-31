package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glbsh/filemanage/metadata-service/internal/service"
)

// inMemoryStore is a thread-safe in-memory MetadataStore for unit tests.
type inMemoryStore struct {
	mu   sync.RWMutex
	data map[string]service.FileMetadata
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{data: make(map[string]service.FileMetadata)}
}

func (s *inMemoryStore) Save(_ context.Context, m service.FileMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[m.ID] = m
	return nil
}

func (s *inMemoryStore) FindByID(_ context.Context, id string) (*service.FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.data[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &m, nil
}

func (s *inMemoryStore) ListAll(_ context.Context) ([]service.FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]service.FileMetadata, 0, len(s.data))
	for _, m := range s.data {
		out = append(out, m)
	}
	return out, nil
}

func (s *inMemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return fmt.Errorf("not found")
	}
	delete(s.data, id)
	return nil
}

func sampleMeta(id string) service.FileMetadata {
	return service.FileMetadata{
		ID:          id,
		Filename:    "test.txt",
		Size:        100,
		ContentType: "text/plain",
		Location:    "files/" + id,
		UploadedAt:  time.Now().UTC(),
	}
}

func TestSaveAndRetrieve(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	meta := sampleMeta("uuid-1")

	saved, err := svc.Save(context.Background(), meta)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if saved.ID != meta.ID {
		t.Errorf("expected ID %q, got %q", meta.ID, saved.ID)
	}

	got, err := svc.GetByID(context.Background(), meta.ID)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if got.Filename != meta.Filename {
		t.Errorf("expected filename %q, got %q", meta.Filename, got.Filename)
	}
}

func TestSaveRequiresID(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	_, err := svc.Save(context.Background(), service.FileMetadata{Filename: "test.txt"})
	if err == nil {
		t.Error("expected error when ID is empty")
	}
}

func TestSaveRequiresFilename(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	_, err := svc.Save(context.Background(), service.FileMetadata{ID: "uuid-2"})
	if err == nil {
		t.Error("expected error when filename is empty")
	}
}

func TestListAll(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())

	for i := 1; i <= 3; i++ {
		if _, err := svc.Save(context.Background(), sampleMeta(fmt.Sprintf("uuid-%d", i))); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
	}

	all, err := svc.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll() error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 items, got %d", len(all))
	}
}

func TestDelete(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	meta := sampleMeta("uuid-del")
	svc.Save(context.Background(), meta)

	if err := svc.Delete(context.Background(), meta.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err := svc.GetByID(context.Background(), meta.ID)
	if err == nil {
		t.Error("expected error after deleting metadata")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	err := svc.Delete(context.Background(), "ghost-id")
	if err == nil {
		t.Error("expected error when deleting non-existent metadata")
	}
}

func TestGetByIDNotFound(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	_, err := svc.GetByID(context.Background(), "missing")
	if err == nil {
		t.Error("expected error for missing ID")
	}
}

func TestSaveSetsUploadedAt(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	m := service.FileMetadata{ID: "uuid-ts", Filename: "f.txt", Size: 1, ContentType: "text/plain", Location: "l"}
	before := time.Now().UTC()
	saved, _ := svc.Save(context.Background(), m)
	after := time.Now().UTC()

	if saved.UploadedAt.Before(before) || saved.UploadedAt.After(after) {
		t.Errorf("UploadedAt not set correctly: %v", saved.UploadedAt)
	}
}

// TestConcurrentSaveListDelete fires concurrent saves, lists, and deletes and
// checks for data races and final consistency.
func TestConcurrentSaveListDelete(t *testing.T) {
	svc := service.NewMetadataService(newInMemoryStore())
	const n = 40

	// Phase 1: save n records concurrently.
	ids := make([]string, n)
	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("uuid-concurrent-%d", i)
			m := service.FileMetadata{
				ID:          id,
				Filename:    fmt.Sprintf("file%d.txt", i),
				Size:        int64(i * 100),
				ContentType: "text/plain",
				Location:    fmt.Sprintf("files/%s", id),
			}
			if _, err := svc.Save(context.Background(), m); err != nil {
				t.Errorf("Save(%d) error: %v", i, err)
				return
			}
			mu.Lock()
			ids[i] = id
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Phase 2: concurrent list + delete (first half) + save more.
	const deleters = n / 2
	const extra = 10

	wg.Add(deleters + extra + 3) // 3 list goroutines

	// Delete first half.
	for i := 0; i < deleters; i++ {
		go func(id string) {
			defer wg.Done()
			svc.Delete(context.Background(), id) // ignore not-found
		}(ids[i])
	}

	// List concurrently three times.
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			all, err := svc.ListAll(context.Background())
			if err != nil {
				t.Errorf("ListAll() error: %v", err)
				return
			}
			// Length is non-deterministic but must be non-negative.
			if len(all) < 0 {
				t.Error("ListAll() returned negative length")
			}
		}()
	}

	// Save extra records while deletes+lists are in flight.
	for i := 0; i < extra; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("uuid-extra-%d", i)
			m := service.FileMetadata{
				ID: id, Filename: fmt.Sprintf("extra%d.txt", i),
				Size: 1, ContentType: "text/plain", Location: "l",
			}
			if _, err := svc.Save(context.Background(), m); err != nil {
				t.Errorf("extra Save(%d) error: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// Final state: (n - deleters) + extra records should exist.
	all, err := svc.ListAll(context.Background())
	if err != nil {
		t.Fatalf("final ListAll() error: %v", err)
	}
	expected := (n - deleters) + extra
	if len(all) != expected {
		t.Errorf("expected %d records after concurrent ops, got %d", expected, len(all))
	}
}
