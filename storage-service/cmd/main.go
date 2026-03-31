package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/glbsh/filemanage/storage-service/internal/events"
	"github.com/glbsh/filemanage/storage-service/internal/handler"
	"github.com/glbsh/filemanage/storage-service/internal/service"
	"github.com/glbsh/filemanage/storage-service/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	minioClient, err := storage.NewMinioClient()
	if err != nil {
		log.Fatalf("failed to connect to MinIO: %v", err)
	}

	bucket := getenv("MINIO_BUCKET", "files")
	workers := getenvInt("UPLOAD_WORKERS", 10)

	fileSvc := service.NewFileService(minioClient, bucket, workers)
	publisher := events.NewPublisher()
	defer publisher.Close()

	fileHandler := handler.NewFileHandler(fileSvc, publisher)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/upload", fileHandler.Upload)
	r.Get("/file/{id}", fileHandler.Download)
	r.Delete("/file/{id}", fileHandler.Delete)

	addr := ":" + getenv("PORT", "8080")
	log.Printf("storage-service listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
