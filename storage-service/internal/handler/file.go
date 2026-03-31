package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/glbsh/filemanage/storage-service/internal/events"
	"github.com/glbsh/filemanage/storage-service/internal/service"
	"github.com/go-chi/chi/v5"
)

type FileHandler struct {
	svc *service.FileService
	pub events.Notifier
}

func NewFileHandler(svc *service.FileService, pub events.Notifier) *FileHandler {
	return &FileHandler{svc: svc, pub: pub}
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	const maxSize = 50 << 20 // 50 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		http.Error(w, "file too large or invalid form", http.StatusBadRequest)
		return
	}

	_, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing 'file' field in form", http.StatusBadRequest)
		return
	}

	result, err := h.svc.Upload(r.Context(), header)
	if err != nil {
		http.Error(w, "upload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.pub.Publish(r.Context(), events.Event{
		Event:    "file.uploaded",
		ID:       result.ID,
		Filename: result.Filename,
		Size:     result.Size,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":           result.ID,
		"filename":     result.Filename,
		"size":         result.Size,
		"content_type": result.ContentType,
		"location":     result.Location,
	})
}

func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rc, info, err := h.svc.Download(r.Context(), id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+info.Key+"\"")
	io.Copy(w, rc)
}

func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Delete(r.Context(), id); err != nil {
		http.Error(w, "file not found or delete failed", http.StatusNotFound)
		return
	}

	h.pub.Publish(r.Context(), events.Event{
		Event: "file.deleted",
		ID:    id,
	})

	w.WriteHeader(http.StatusNoContent)
}
