package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Asset is the JSON shape of an asset.
type Asset struct {
	ID        int64  `json:"id"`
	Ticker    string `json:"ticker"`
	Name      string `json:"name"`
	Market    string `json:"market" enum:"B3,US"`
	Currency  string `json:"currency" enum:"BRL,USD"`
	Kind      string `json:"kind"`
	Sleeve    string `json:"sleeve" enum:"core,tactical"`
	Status    string `json:"status" enum:"watchlist,holding,exited"`
	Benchmark string `json:"benchmark"`
	CNPJ      string `json:"cnpj" doc:"B3 company CNPJ used by fundamentals; empty when unset"`
	CIK       string `json:"cik" doc:"SEC CIK (10 digits) used by fundamentals; empty when unset"`
	// UnderlyingAssetID is the US asset a BDR represents.
	UnderlyingAssetID *int64 `json:"underlying_asset_id" doc:"US asset this BDR represents; its rating, thesis and fair value apply; null when not linked"`
	BDRRatio          string `json:"bdr_ratio" doc:"BDRs per underlying share; empty when not linked"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

func toAsset(a db.Asset) Asset {
	out := Asset{ID: a.ID, Ticker: a.Ticker, Name: a.Name, Market: a.Market, Currency: a.Currency, Kind: a.Kind, Sleeve: a.Sleeve,
		Status: a.Status, Benchmark: a.Benchmark, CNPJ: a.Cnpj, CIK: a.Cik, BDRRatio: a.BdrRatio, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
	if a.UnderlyingAssetID.Valid {
		v := a.UnderlyingAssetID.Int64
		out.UnderlyingAssetID = &v
	}
	return out
}

func okAsset(a db.Asset, err error) (*Body[Asset], error) {
	if err != nil {
		return nil, apiError(err)
	}
	return &Body[Asset]{Body: toAsset(a)}, nil
}

// AssetCreate is the body of POST /api/assets.
type AssetCreate struct {
	Ticker    string `json:"ticker"`
	Name      string `json:"name" required:"false"`
	Market    string `json:"market" doc:"B3 or US"`
	Currency  string `json:"currency" required:"false" doc:"Defaults from market"`
	Kind      string `json:"kind" doc:"stock, bdr or etf"`
	Sleeve    string `json:"sleeve" required:"false" doc:"core (default) or tactical"`
	Benchmark string `json:"benchmark" required:"false" doc:"Defaults from market"`
	CNPJ      string `json:"cnpj" required:"false" doc:"B3 company CNPJ (fundamentals)"`
	CIK       string `json:"cik" required:"false" doc:"SEC CIK (fundamentals); resolved from the ticker when empty"`
	// BDR link.
	UnderlyingAssetID *int64 `json:"underlying_asset_id,omitempty" required:"false" doc:"For a bdr: the US asset it represents (needs bdr_ratio)"`
	BDRRatio          string `json:"bdr_ratio" required:"false" doc:"For a bdr: BDRs per underlying share, for example 1 or 0.1"`
}

// AssetUpdate is the body of PATCH /api/assets/{id}.
type AssetUpdate struct {
	Name      *string `json:"name,omitempty"`
	Sleeve    *string `json:"sleeve,omitempty"`
	Benchmark *string `json:"benchmark,omitempty"`
	CNPJ      *string `json:"cnpj,omitempty" doc:"Empty string clears"`
	CIK       *string `json:"cik,omitempty" doc:"Empty string clears"`
	// BDR link (kind bdr only).
	UnderlyingAssetID *int64  `json:"underlying_asset_id,omitempty" doc:"US asset the bdr represents; 0 clears the link"`
	BDRRatio          *string `json:"bdr_ratio,omitempty" doc:"BDRs per underlying share"`
}

// StatusChange is a body carrying only a status.
type StatusChange struct {
	Status string `json:"status"`
}

type assetListIn struct {
	Status string `query:"status"`
	Sleeve string `query:"sleeve"`
	Market string `query:"market"`
}

// WithAssets mounts the asset routes.
func WithAssets(svc *assets.Service) Option {
	return func(s *server) {
		const tag = "assets"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets", "createAsset", "Create an asset (starts on the watchlist)", tag, http.StatusCreated),
			func(ctx context.Context, in *Body[AssetCreate]) (*Body[Asset], error) {
				return okAsset(svc.Create(ctx, assets.CreateInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets", "listAssets", "List assets", tag),
			func(ctx context.Context, in *assetListIn) (*Body[[]Asset], error) {
				list, err := svc.List(ctx, assets.Filter{Status: in.Status, Sleeve: in.Sleeve, Market: in.Market})
				if err != nil {
					return nil, apiError(err)
				}
				out := make([]Asset, 0, len(list))
				for _, a := range list {
					out = append(out, toAsset(a))
				}
				return &Body[[]Asset]{Body: out}, nil
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}", "getAsset", "Get an asset", tag),
			func(ctx context.Context, in *IDPath) (*Body[Asset], error) {
				return okAsset(svc.Get(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPatch, "/api/assets/{id}", "updateAsset", "Update name, sleeve, benchmark, CNPJ, CIK or the BDR underlying link", tag),
			func(ctx context.Context, in *BodyIn[AssetUpdate]) (*Body[Asset], error) {
				return okAsset(svc.Update(ctx, in.ID, assets.UpdateInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/assets/{id}/status", "transitionAsset", "Move an asset through its lifecycle", tag),
			func(ctx context.Context, in *BodyIn[StatusChange]) (*Body[Asset], error) {
				return okAsset(svc.Transition(ctx, in.ID, in.Body.Status))
			})
	}
}
