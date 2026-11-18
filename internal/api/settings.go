package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/settings"
)

type groupPath struct {
	Group string `path:"group" enum:"allocation,planner,tax,tactical,ai" doc:"Settings group"`
}

// putGroup registers PUT /api/settings/{name} for one parameter group.
func putGroup[T any](s *server, name, id string, save func(context.Context, T) (T, error)) {
	huma.Register(s.api, op(http.MethodPut, "/api/settings/"+name, id, "Replace the "+name+" settings", "settings"),
		func(ctx context.Context, in *Body[T]) (*Body[T], error) {
			return ok(save(ctx, in.Body))
		})
}

// WithSettings mounts the editable rule settings and key status routes.
func WithSettings(svc *settings.Service) Option {
	return func(s *server) {
		const tag = "settings"
		huma.Register(s.api, op(http.MethodGet, "/api/settings", "getSettings", "Every settings group: current values and defaults", tag),
			func(ctx context.Context, _ *struct{}) (*Body[settings.Overview], error) {
				return ok(svc.All(ctx))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/settings/status", "getSettingsStatus", "API key status (never the values) and AI usage", tag),
			func(ctx context.Context, _ *struct{}) (*Body[settings.StatusReport], error) {
				return ok(svc.Status(ctx))
			})
		putGroup(s, settings.GroupAllocation, "setAllocationSettings", func(ctx context.Context, v settings.Allocation) (settings.Allocation, error) {
			return svc.SetAllocation(ctx, v)
		})
		putGroup(s, settings.GroupPlanner, "setPlannerSettings", func(ctx context.Context, v settings.Planner) (settings.Planner, error) {
			return svc.SetPlanner(ctx, v)
		})
		putGroup(s, settings.GroupTax, "setTaxSettings", func(ctx context.Context, v settings.Tax) (settings.Tax, error) {
			return svc.SetTax(ctx, v)
		})
		putGroup(s, settings.GroupTactical, "setTacticalSettings", func(ctx context.Context, v settings.Tactical) (settings.Tactical, error) {
			return svc.SetTactical(ctx, v)
		})
		putGroup(s, settings.GroupAI, "setAISettings", func(ctx context.Context, v settings.AI) (settings.AI, error) {
			return svc.SetAI(ctx, v)
		})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/settings/{group}/reset", "resetSettings", "Restore a group's defaults", tag, http.StatusNoContent),
			func(ctx context.Context, in *groupPath) (*struct{}, error) {
				return noContent(svc.Reset(ctx, in.Group))
			})
	}
}
