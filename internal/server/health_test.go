package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorMux_Liveness(t *testing.T) {
	mux := NewMonitorMux(nil)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health/live")
	if err != nil {
		t.Fatalf("GET /health/live: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("/health/live status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "ok") {
		t.Errorf("/health/live body = %q, want contains 'ok'", body)
	}
}

func TestMonitorMux_ReadinessBeforeReady(t *testing.T) {
	srv, _ := NewGRPCServer(":0")
	mux := NewMonitorMux(srv)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health/ready")
	if err != nil {
		t.Fatalf("GET /health/ready: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/health/ready (before ready) status = %d, want 503", resp.StatusCode)
	}
}

func TestMonitorMux_ReadinessAfterReady(t *testing.T) {
	srv, _ := NewGRPCServer(":0")
	mux := NewMonitorMux(srv)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	srv.SetReady()

	resp, err := http.Get(ts.URL + "/health/ready")
	if err != nil {
		t.Fatalf("GET /health/ready: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("/health/ready (after ready) status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "ok") {
		t.Errorf("/health/ready body = %q, want contains 'ok'", body)
	}
}

func TestMonitorMux_ReadinessAfterNotReady(t *testing.T) {
	srv, _ := NewGRPCServer(":0")
	mux := NewMonitorMux(srv)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	srv.SetReady()
	srv.SetNotReady()

	resp, err := http.Get(ts.URL + "/health/ready")
	if err != nil {
		t.Fatalf("GET /health/ready: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/health/ready (after not ready) status = %d, want 503", resp.StatusCode)
	}
}

func TestMonitorMux_HealthBackwardCompatible(t *testing.T) {
	mux := NewMonitorMux(nil)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("/health status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "ok") {
		t.Errorf("/health body = %q, want contains 'ok'", body)
	}
}

func TestMonitorMux_Metrics(t *testing.T) {
	mux := NewMonitorMux(nil)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("/metrics status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "# HELP") {
		t.Errorf("/metrics body missing Prometheus format")
	}
}

func TestGRPCServer_HealthServerExposed(t *testing.T) {
	srv, err := NewGRPCServer(":0")
	if err != nil {
		t.Fatalf("NewGRPCServer: %v", err)
	}
	if srv.HealthServer == nil {
		t.Error("HealthServer field should be exposed and non-nil")
	}
}
