package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lorasim/internal/sim"
)

func TestHealthAndComparison(t *testing.T) {
	srv := NewServer(99, 40, 1)
	handler := srv.Handler()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	cmp := httptest.NewRecorder()
	handler.ServeHTTP(cmp, httptest.NewRequest(http.MethodGet, "/api/comparison", nil))
	if cmp.Code != http.StatusOK {
		t.Fatalf("comparison status = %d", cmp.Code)
	}
	var got sim.Comparison
	if err := json.Unmarshal(cmp.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid comparison JSON: %v", err)
	}
	if got.ScenarioSeed != 99 || got.NodeCount != 40 {
		t.Fatalf("unexpected scenario: seed=%d nodes=%d", got.ScenarioSeed, got.NodeCount)
	}
	if !got.Determinism.FixedRepeatedIdentical || !got.Determinism.ADRRepeatedIdentical {
		t.Fatal("determinism flags not set")
	}
	if len(got.Fixed.Channels) == 0 || len(got.Fixed.Nodes) != 40 {
		t.Fatal("report payload incomplete")
	}
}

func TestRerunAcceptsNewParams(t *testing.T) {
	srv := NewServer(1, 30, 1)
	handler := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rerun",
		strings.NewReader(`{"seed":7,"nodes":55,"hours":2}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rerun status = %d", rec.Code)
	}
	var got sim.Comparison
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid rerun JSON: %v", err)
	}
	if got.ScenarioSeed != 7 || got.NodeCount != 55 {
		t.Fatalf("rerun did not apply params: seed=%d nodes=%d", got.ScenarioSeed, got.NodeCount)
	}
}

func TestRerunRejectsBadJSON(t *testing.T) {
	srv := NewServer(1, 30, 1)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rerun", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status = %d, want 400", rec.Code)
	}
}

func TestStaticIndexServed(t *testing.T) {
	srv := NewServer(1, 20, 1)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "LoRa") {
		t.Fatalf("index not served: status=%d bytes=%d", rec.Code, rec.Body.Len())
	}
}
