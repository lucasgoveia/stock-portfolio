package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
)

// WithAlerts mounts the alert inbox endpoints.
func WithAlerts(svc *alerts.Service) Option {
	h := &alertHandler{svc: svc}
	return func(r chi.Router) {
		r.Get("/api/alerts", h.list)
		r.Post("/api/alerts/{id}/ack", h.ack)
		r.Post("/api/alerts/{id}/snooze", h.snooze)
		r.Post("/api/alerts/{id}/resolve", h.resolve)
	}
}

type alertHandler struct{ svc *alerts.Service }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, alerts.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, alerts.ErrResolved):
		status = http.StatusConflict
	case errors.Is(err, alerts.ErrInvalid), errors.Is(err, alerts.ErrInvalidType):
		status = http.StatusBadRequest
	}
	msg := err.Error()
	if status == http.StatusInternalServerError {
		msg = "internal error"
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *alertHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := alerts.Filter{State: q.Get("state"), Type: q.Get("type")}
	if v := q.Get("asset_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid asset_id"})
			return
		}
		f.AssetID = &id
	}
	list, err := h.svc.List(r.Context(), f)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": list})
}

func alertID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func (h *alertHandler) ack(w http.ResponseWriter, r *http.Request) {
	id, ok := alertID(w, r)
	if !ok {
		return
	}
	a, err := h.svc.Acknowledge(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *alertHandler) resolve(w http.ResponseWriter, r *http.Request) {
	id, ok := alertID(w, r)
	if !ok {
		return
	}
	a, err := h.svc.Resolve(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *alertHandler) snooze(w http.ResponseWriter, r *http.Request) {
	id, ok := alertID(w, r)
	if !ok {
		return
	}
	var body struct {
		Until time.Time `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Until.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"until\": RFC3339 time}"})
		return
	}
	a, err := h.svc.Snooze(r.Context(), id, body.Until)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
