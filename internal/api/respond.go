package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps domain errors to HTTP status codes. Internal errors are not leaked.
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, assets.ErrValidation),
		errors.Is(err, alerts.ErrInvalid), errors.Is(err, alerts.ErrInvalidType),
		errors.Is(err, trades.ErrValidation), errors.Is(err, cash.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, assets.ErrNotFound), errors.Is(err, alerts.ErrNotFound),
		errors.Is(err, trades.ErrNotFound), errors.Is(err, cash.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, assets.ErrDuplicate), errors.Is(err, assets.ErrInvalidTransition),
		errors.Is(err, alerts.ErrResolved), errors.Is(err, trades.ErrInsufficientQuantity),
		errors.Is(err, cash.ErrInsufficientBalance), errors.Is(err, cash.ErrNoRate):
		status = http.StatusConflict
	}
	msg := err.Error()
	if status == http.StatusInternalServerError {
		msg = "internal error"
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
