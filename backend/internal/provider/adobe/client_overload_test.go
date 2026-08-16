package adobe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestIsSystemOverloaded(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "message", body: `{"message":"system under load"}`, want: true},
		{name: "error code", body: `{"error_code":"timeout_error"}`, want: true},
		{name: "case insensitive", body: `{"message":"SYSTEM UNDER LOAD"}`, want: true},
		{name: "other timeout", body: `{"message":"gateway timed out"}`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isSystemOverloaded(test.body); got != test.want {
				t.Fatalf("isSystemOverloaded(%q) = %v, want %v", test.body, got, test.want)
			}
		})
	}
}

func TestSubmitOverloadedWrapsTemporaryUpstream(t *testing.T) {
	if !errors.Is(ErrSubmitOverloaded, ErrTemporaryUpstream) {
		t.Fatal("submit overload must remain a temporary upstream error")
	}
	if !errors.Is(ErrJobOverloaded, ErrTemporaryUpstream) {
		t.Fatal("job overload must remain a temporary upstream error")
	}
}

func TestSubmitPermitReceivesAdobeSubmitResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error_code":"timeout_error","message":"system under load"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "")
	session, err := client.newDirectTLSClient()
	if err != nil {
		t.Fatal(err)
	}
	var reported error
	permit := func(context.Context) (func(error), error) {
		return func(err error) { reported = err }, nil
	}
	_, _, err = client.submitImageWithPermit(context.Background(), session, "token", "prompt", server.URL, map[string]any{"prompt": "test"}, permit)
	if !errors.Is(err, ErrSubmitOverloaded) {
		t.Fatalf("submit error = %v, want ErrSubmitOverloaded", err)
	}
	if !errors.Is(reported, ErrSubmitOverloaded) {
		t.Fatalf("reported error = %v, want ErrSubmitOverloaded", reported)
	}
}

func TestPollOverloadIsClassifiedAsJobCapacity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error_code":"timeout_error","message":"system under load"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "")
	session, err := client.newDirectTLSClient()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.pollImage(context.Background(), session, "token", server.URL, false)
	if !errors.Is(err, ErrJobOverloaded) {
		t.Fatalf("poll error = %v, want ErrJobOverloaded", err)
	}
	if !errors.Is(err, ErrTemporaryUpstream) {
		t.Fatalf("poll error = %v, want temporary classification", err)
	}
}

func TestProxyAccessIsConcurrentSafe(t *testing.T) {
	client := NewClient("test-key", "")
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(2)
		go func() {
			defer group.Done()
			client.SetProxy("http://127.0.0.1:8080")
		}()
		go func() {
			defer group.Done()
			_ = client.proxyURL()
		}()
	}
	group.Wait()
}
