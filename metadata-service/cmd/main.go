package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/glbsh/filemanage/metadata-service/internal/db"
	"github.com/glbsh/filemanage/metadata-service/internal/events"
	"github.com/glbsh/filemanage/metadata-service/internal/handler"
	"github.com/glbsh/filemanage/metadata-service/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	ctx := context.Background()

	pool, err := db.NewPool(ctx)
	if err != nil {
		log.Fatalf("failed to connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	store := service.NewPostgresStore(pool)
	svc := service.NewMetadataService(store)

	publisher := events.NewPublisher()
	defer publisher.Close()

	h := handler.NewMetadataHandler(svc, publisher)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/metadata", h.Save)
	r.Get("/metadata/{id}", h.GetByID)
	r.Get("/files/", h.ListAll)

	addr := ":" + getenv("PORT", "8081")
	log.Printf("metadata-service listening on %s", addr)
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
