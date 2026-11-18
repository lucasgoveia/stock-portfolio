package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
)

type alertListIn struct {
	State   string `query:"state" doc:"open, snoozed, acknowledged or resolved"`
	Type    string `query:"type"`
	AssetID int64  `query:"asset_id" minimum:"0" doc:"Filter by asset; 0 or absent means any"`
}

// AlertList is the body of GET /api/alerts.
type AlertList struct {
	Alerts []alerts.Alert `json:"alerts"`
}

// Snooze is the body of POST /api/alerts/{id}/snooze.
type Snooze struct {
	Until time.Time `json:"until" doc:"RFC 3339 time the alert reopens"`
}

// WithAlerts mounts the alert inbox endpoints.
func WithAlerts(svc *alerts.Service) Option {
	return func(s *server) {
		const tag = "alerts"
		huma.Register(s.api, op(http.MethodGet, "/api/alerts", "listAlerts", "List alerts", tag),
			func(ctx context.Context, in *alertListIn) (*Body[AlertList], error) {
				f := alerts.Filter{State: in.State, Type: in.Type}
				if in.AssetID > 0 {
					f.AssetID = &in.AssetID
				}
				list, err := svc.List(ctx, f)
				return ok(AlertList{Alerts: list}, err)
			})
		huma.Register(s.api, op(http.MethodPost, "/api/alerts/{id}/ack", "acknowledgeAlert", "Acknowledge an alert", tag),
			func(ctx context.Context, in *IDPath) (*Body[alerts.Alert], error) {
				return ok(svc.Acknowledge(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/alerts/{id}/snooze", "snoozeAlert", "Snooze an alert until a time", tag),
			func(ctx context.Context, in *BodyIn[Snooze]) (*Body[alerts.Alert], error) {
				if in.Body.Until.IsZero() {
					return nil, &Error{status: http.StatusBadRequest, Message: `body must be {"until": RFC3339 time}`}
				}
				return ok(svc.Snooze(ctx, in.ID, in.Body.Until))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/alerts/{id}/resolve", "resolveAlert", "Resolve an alert", tag),
			func(ctx context.Context, in *IDPath) (*Body[alerts.Alert], error) {
				return ok(svc.Resolve(ctx, in.ID))
			})
	}
}
