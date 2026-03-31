package handler

import (
	"encoding/json"
	"net/http"

	"github.com/glbsh/filemanage/metadata-service/internal/events"
	"github.com/glbsh/filemanage/metadata-service/internal/service"
	"github.com/go-chi/chi/v5"
)

type MetadataHandler struct {
	svc *service.MetadataService
	pub events.Notifier
}

func NewMetadataHandler(svc *service.MetadataService, pub events.Notifier) *MetadataHandler {
	return &MetadataHandler{svc: svc, pub: pub}
}

func (h *MetadataHandler) Save(w http.ResponseWriter, r *http.Request) {
	var m service.FileMetadata
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	saved, err := h.svc.Save(r.Context(), m)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.pub.Publish(r.Context(), events.Event{
		Event:    "metadata.created",
		ID:       saved.ID,
		Filename: saved.Filename,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(saved)
}

func (h *MetadataHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	meta, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}

func (h *MetadataHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	files, err := h.svc.ListAll(r.Context())
	if err != nil {
		http.Error(w, "failed to list files", http.StatusInternalServerError)
		return
	}

	if files == nil {
		files = []service.FileMetadata{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}
