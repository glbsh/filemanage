package storage

import (
	"context"
	"io"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const defaultBucket = "files"

type MinioClient struct {
	client *minio.Client
	bucket string
}

func NewMinioClient() (*MinioClient, error) {
	endpoint := getenv("MINIO_ENDPOINT", "localhost:9000")
	accessKey := getenv("MINIO_ACCESS_KEY", "minioadmin")
	secretKey := getenv("MINIO_SECRET_KEY", "minioadmin")
	bucket := getenv("MINIO_BUCKET", defaultBucket)

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	mc := &MinioClient{client: client, bucket: bucket}
	if err := mc.ensureBucket(context.Background()); err != nil {
		return nil, err
	}
	return mc, nil
}

func (m *MinioClient) ensureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return m.client.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{})
	}
	return nil
}

func (m *MinioClient) Upload(ctx context.Context, id string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, id, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (m *MinioClient) Download(ctx context.Context, id string) (*minio.Object, error) {
	return m.client.GetObject(ctx, m.bucket, id, minio.GetObjectOptions{})
}

func (m *MinioClient) Delete(ctx context.Context, id string) error {
	return m.client.RemoveObject(ctx, m.bucket, id, minio.RemoveObjectOptions{})
}

func (m *MinioClient) Stat(ctx context.Context, id string) (minio.ObjectInfo, error) {
	return m.client.StatObject(ctx, m.bucket, id, minio.StatObjectOptions{})
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
