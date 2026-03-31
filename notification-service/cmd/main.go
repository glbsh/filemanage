package main

import (
	"log"
	"net/http"
	"os"

	"github.com/glbsh/filemanage/notification-service/internal/handler"
	"github.com/glbsh/filemanage/notification-service/internal/subscriber"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	sub := subscriber.NewRedisSubscriber()
	defer sub.Close()

	eventsHandler := handler.NewEventsHandler(sub)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/events", eventsHandler.Stream)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := ":" + getenv("PORT", "8082")
	log.Printf("notification-service listening on %s", addr)
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
