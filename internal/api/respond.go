package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/candidates"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/corpactions"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/jobs"
	"github.com/lucasgoveia/stock-portfolio/internal/performance"
	"github.com/lucasgoveia/stock-portfolio/internal/portfolio"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

// Error is the body of every error response: {"error": "message"}.
type Error struct {
	status  int
	Message string `json:"error" doc:"Human-readable error message"`
}

func (e *Error) Error() string  { return e.Message }
func (e *Error) GetStatus() int { return e.status }

func init() {
	// huma reports request validation failures as 422; this API has always
	// answered 400 for any malformed request, so fold them together.
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		if status == http.StatusUnprocessableEntity {
			status = http.StatusBadRequest
		}
		var details []string
		for _, e := range errs {
			if e != nil {
				details = append(details, e.Error())
			}
		}
		if len(details) > 0 {
			msg += ": " + strings.Join(details, "; ")
		}
		return &Error{status: status, Message: msg}
	}
}

// apiError maps domain errors to HTTP errors. Internal errors are not leaked.
func apiError(err error) error {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, assets.ErrValidation),
		errors.Is(err, alerts.ErrInvalid), errors.Is(err, alerts.ErrInvalidType),
		errors.Is(err, trades.ErrValidation), errors.Is(err, cash.ErrValidation),
		errors.Is(err, ratings.ErrValidation), errors.Is(err, research.ErrValidation),
		errors.Is(err, thesis.ErrValidation), errors.Is(err, valuation.ErrValidation),
		errors.Is(err, portfolio.ErrValidation), errors.Is(err, tactical.ErrValidation),
		errors.Is(err, dividends.ErrValidation), errors.Is(err, corpactions.ErrValidation),
		errors.Is(err, performance.ErrValidation), errors.Is(err, settings.ErrValidation), errors.Is(err, fundamentals.ErrValidation), errors.Is(err, analyst.ErrValidation), errors.Is(err, candidates.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, assets.ErrNotFound), errors.Is(err, alerts.ErrNotFound),
		errors.Is(err, trades.ErrNotFound), errors.Is(err, cash.ErrNotFound),
		errors.Is(err, ratings.ErrNotFound), errors.Is(err, research.ErrNotFound),
		errors.Is(err, thesis.ErrNotFound), errors.Is(err, valuation.ErrNotFound),
		errors.Is(err, jobs.ErrNoRuns), errors.Is(err, portfolio.ErrNotFound),
		errors.Is(err, tactical.ErrNotFound), errors.Is(err, dividends.ErrNotFound),
		errors.Is(err, corpactions.ErrNotFound), errors.Is(err, settings.ErrNotFound), errors.Is(err, analyst.ErrNotFound), errors.Is(err, candidates.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, assets.ErrDuplicate), errors.Is(err, assets.ErrInvalidTransition),
		errors.Is(err, alerts.ErrResolved), errors.Is(err, trades.ErrInsufficientQuantity),
		errors.Is(err, cash.ErrInsufficientBalance), errors.Is(err, cash.ErrNoRate),
		errors.Is(err, portfolio.ErrNoFX), errors.Is(err, jobs.ErrRunning),
		errors.Is(err, portfolio.ErrConflict), errors.Is(err, dividends.ErrConflict),
		errors.Is(err, corpactions.ErrConflict), errors.Is(err, analyst.ErrConflict),
		errors.Is(err, analyst.ErrNoKey), errors.Is(err, analyst.ErrCapReached), errors.Is(err, candidates.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, fundamentals.ErrUpstream), errors.Is(err, analyst.ErrUpstream):
		status = http.StatusBadGateway
	}
	msg := err.Error()
	if status == http.StatusInternalServerError {
		msg = "internal error"
	}
	return &Error{status: status, Message: msg}
}

// Shared input and output shapes.

// IDPath is the {id} path parameter shared by most routes.
type IDPath struct {
	ID int64 `path:"id" minimum:"1" doc:"Resource id"`
}

// Body wraps a JSON response body.
type Body[T any] struct {
	Body T
}

// BodyIn wraps a JSON request body for a route with an {id} path parameter.
type BodyIn[T any] struct {
	IDPath
	Body T
}

func ok[T any](v T, err error) (*Body[T], error) {
	if err != nil {
		return nil, apiError(err)
	}
	return &Body[T]{Body: v}, nil
}

func noContent(err error) (*struct{}, error) {
	if err != nil {
		return nil, apiError(err)
	}
	return nil, nil
}
