package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// TestOpenAPIUpToDate fails when api/openapi.json differs from the document
// the handlers generate. Regenerate with `make openapi`.
func TestOpenAPIUpToDate(t *testing.T) {
	want, err := json.MarshalIndent(OpenAPI(AllRoutes()...), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), want) {
		t.Fatal("api/openapi.json is stale; run `make openapi`")
	}
}

func TestOpenAPIServed(t *testing.T) {
	rec := get(t, testUI(), "/api/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var doc struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc.OpenAPI == "" || doc.Paths["/api/health"] == nil {
		t.Fatalf("doc %v %v", doc.OpenAPI, err)
	}
}

func TestErrorShape(t *testing.T) {
	h := newServer(t)
	rec := doAlert(h, "POST", "/api/assets", `{"ticker":"X","market":"B3","kind":"stock","bogus":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == nil || len(body) != 1 {
		t.Fatalf("body %s", rec.Body)
	}
}
