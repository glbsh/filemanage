package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// MinioStorage is the subset of MinioClient used by FileService.
type MinioStorage interface {
	Upload(ctx context.Context, id string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, id string) (*minio.Object, error)
	Delete(ctx context.Context, id string) error
	Stat(ctx context.Context, id string) (minio.ObjectInfo, error)
}

type UploadResult struct {
	ID          string
	Filename    string
	Size        int64
	ContentType string
	Location    string
	Bucket      string
}

type FileService struct {
	store  MinioStorage
	bucket string
	jobs   chan uploadJob
}

type uploadJob struct {
	ctx         context.Context
	id          string
	file        multipart.File
	size        int64
	contentType string
	result      chan error
}

// NewFileService creates a FileService backed by a real MinIO client.
func NewFileService(store MinioStorage, bucket string, workers int) *FileService {
	return NewFileServiceWithStorage(store, bucket, workers)
}

// NewFileServiceWithStorage allows injecting any MinioStorage (useful in tests).
func NewFileServiceWithStorage(store MinioStorage, bucket string, workers int) *FileService {
	fs := &FileService{
		store:  store,
		bucket: bucket,
		jobs:   make(chan uploadJob, workers*2),
	}
	for i := 0; i < workers; i++ {
		go fs.worker()
	}
	return fs
}

func (fs *FileService) worker() {
	for job := range fs.jobs {
		job.result <- fs.store.Upload(job.ctx, job.id, job.file, job.size, job.contentType)
	}
}

func (fs *FileService) Upload(ctx context.Context, header *multipart.FileHeader) (*UploadResult, error) {
	file, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	id := uuid.New().String()
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = detectContentType(file)
	}

	result := make(chan error, 1)
	fs.jobs <- uploadJob{
		ctx:         ctx,
		id:          id,
		file:        file,
		size:        header.Size,
		contentType: contentType,
		result:      result,
	}

	if err := <-result; err != nil {
		return nil, fmt.Errorf("upload to minio: %w", err)
	}

	return &UploadResult{
		ID:          id,
		Filename:    header.Filename,
		Size:        header.Size,
		ContentType: contentType,
		Location:    fmt.Sprintf("%s/%s", fs.bucket, id),
		Bucket:      fs.bucket,
	}, nil
}

func (fs *FileService) Download(ctx context.Context, id string) (*minio.Object, *minio.ObjectInfo, error) {
	obj, err := fs.store.Download(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, nil, err
	}
	return obj, &info, nil
}

func (fs *FileService) Delete(ctx context.Context, id string) error {
	if _, err := fs.store.Stat(ctx, id); err != nil {
		return err
	}
	return fs.store.Delete(ctx, id)
}

func detectContentType(r io.ReadSeeker) string {
	buf := make([]byte, 512)
	n, _ := r.Read(buf)
	r.Seek(0, io.SeekStart)
	return http.DetectContentType(buf[:n])
}
