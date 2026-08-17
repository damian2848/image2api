package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"backend/internal/model"
	"backend/internal/provider/adobe"
)

func TestTryAccountAdobeRouteErrorsDoNotFanOutOrPenalizeAccount(t *testing.T) {
	for _, routeErr := range []error{adobe.ErrSubmitOverloaded, adobe.ErrJobOverloaded, adobe.ErrSubmitTransport} {
		t.Run(routeErr.Error(), func(t *testing.T) {
			token := model.TokenAccount{ID: "adobe-1", Pool: "adobe", Status: "active", Fails: 3, FailTotal: 7}
			service := &V1Service{}
			attempts := 0
			_, gotErr, failover, temporaryAccountFailure := service.tryAccount(
				context.Background(), "evt-overload", token.Pool, token, "image",
				func(model.TokenAccount) ([]byte, error) {
					attempts++
					return nil, fmt.Errorf("upstream response: %w", routeErr)
				},
				adobeErrClass,
				nil,
				true,
			)
			if !errors.Is(gotErr, routeErr) {
				t.Fatalf("error = %v, want %v", gotErr, routeErr)
			}
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
			if failover || temporaryAccountFailure {
				t.Fatalf("route error must not fail over or penalize the account")
			}
		})
	}
}

func TestAdobeOverloadJitterIsStableAndBounded(t *testing.T) {
	got := adobeOverloadJitter("evt-abc", 1)
	if got != adobeOverloadJitter("evt-abc", 1) {
		t.Fatal("jitter must be stable for the same event and attempt")
	}
	if got < 0 || got >= 5*time.Second {
		t.Fatalf("jitter %s is outside [0, 5s)", got)
	}
}

func TestAdobeSubmitBucketMatchesUpstreamEndpoint(t *testing.T) {
	if got := adobeSubmitBucket("firefly-image-5"); got != adobeSubmitBucketImageV5 {
		t.Fatalf("Image 5 bucket = %q, want %q", got, adobeSubmitBucketImageV5)
	}
	if got := adobeSubmitBucket("firefly-gpt-image-2"); got != adobeSubmitBucket3P {
		t.Fatalf("Image 2 bucket = %q, want %q", got, adobeSubmitBucket3P)
	}
}

func TestAdobeOverloadPauseRequiresCorrelatedFailures(t *testing.T) {
	tests := []struct {
		count int
		want  time.Duration
	}{
		{count: 1, want: 0},
		{count: 7, want: 0},
		{count: 8, want: 10 * time.Second},
		{count: 9, want: 20 * time.Second},
		{count: 10, want: 40 * time.Second},
		{count: 11, want: time.Minute},
	}
	for _, test := range tests {
		if got := adobeOverloadPause(test.count); got != test.want {
			t.Fatalf("pause(%d) = %s, want %s", test.count, got, test.want)
		}
	}
}
