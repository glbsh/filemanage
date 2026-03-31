package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type FileMetadata struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	Location    string    `json:"location"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

// MetadataStore is the storage interface, enabling testing without a real DB.
type MetadataStore interface {
	Save(ctx context.Context, m FileMetadata) error
	FindByID(ctx context.Context, id string) (*FileMetadata, error)
	ListAll(ctx context.Context) ([]FileMetadata, error)
	Delete(ctx context.Context, id string) error
}

type MetadataService struct {
	store MetadataStore
}

func NewMetadataService(store MetadataStore) *MetadataService {
	return &MetadataService{store: store}
}

func (s *MetadataService) Save(ctx context.Context, m FileMetadata) (*FileMetadata, error) {
	if m.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if m.Filename == "" {
		return nil, fmt.Errorf("filename is required")
	}
	if m.UploadedAt.IsZero() {
		m.UploadedAt = time.Now().UTC()
	}
	if err := s.store.Save(ctx, m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *MetadataService) GetByID(ctx context.Context, id string) (*FileMetadata, error) {
	return s.store.FindByID(ctx, id)
}

func (s *MetadataService) ListAll(ctx context.Context) ([]FileMetadata, error) {
	return s.store.ListAll(ctx)
}

func (s *MetadataService) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

// PostgresStore implements MetadataStore against a real pgxpool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (p *PostgresStore) Save(ctx context.Context, m FileMetadata) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO file_metadata (id, filename, size, content_type, location, uploaded_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE
		   SET filename=$2, size=$3, content_type=$4, location=$5, uploaded_at=$6`,
		m.ID, m.Filename, m.Size, m.ContentType, m.Location, m.UploadedAt,
	)
	return err
}

func (p *PostgresStore) FindByID(ctx context.Context, id string) (*FileMetadata, error) {
	row := p.pool.QueryRow(ctx,
		`SELECT id, filename, size, content_type, location, uploaded_at
		 FROM file_metadata WHERE id=$1`, id)
	var m FileMetadata
	if err := row.Scan(&m.ID, &m.Filename, &m.Size, &m.ContentType, &m.Location, &m.UploadedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (p *PostgresStore) ListAll(ctx context.Context) ([]FileMetadata, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, filename, size, content_type, location, uploaded_at
		 FROM file_metadata ORDER BY uploaded_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []FileMetadata
	for rows.Next() {
		var m FileMetadata
		if err := rows.Scan(&m.ID, &m.Filename, &m.Size, &m.ContentType, &m.Location, &m.UploadedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (p *PostgresStore) Delete(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM file_metadata WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}
