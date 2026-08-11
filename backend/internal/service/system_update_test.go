package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemUpdateServiceStatusForwardsSharedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Image2API-Update-Token"); got != "test-token" {
			t.Fatalf("unexpected token: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"idle","step":"ready","current_version":"v1.0.0","latest_version":"v1.1.0","has_update":true}`))
	}))
	defer server.Close()

	service := NewSystemUpdateService(server.URL, "test-token")
	status, err := service.Status(context.Background(), false)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.CurrentVersion != "v1.0.0" || !status.HasUpdate {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestSystemUpdateServiceRejectsDisabledUpdater(t *testing.T) {
	service := NewSystemUpdateService("", "")
	if _, err := service.Status(context.Background(), false); err != ErrUpdaterDisabled {
		t.Fatalf("Status() error = %v, want ErrUpdaterDisabled", err)
	}
}

func TestSystemUpdateServiceKeepsUpdaterHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"already running the latest release"}`))
	}))
	defer server.Close()

	service := NewSystemUpdateService(server.URL, "test-token")
	_, err := service.Start(context.Background())
	var updaterErr *UpdaterError
	if !errors.As(err, &updaterErr) || updaterErr.Status != http.StatusConflict {
		t.Fatalf("Start() error = %#v, want HTTP 409 updater error", err)
	}
}
