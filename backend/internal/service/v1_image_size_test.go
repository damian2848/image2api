package service

import (
	"net/netip"
	"testing"
	"time"

	"backend/internal/model"
)

func TestParseImageSize(t *testing.T) {
	tests := []struct {
		name                           string
		size, aspectRatio, imageSize   string
		wantAspectRatio, wantImageSize string
	}{
		{
			name:            "explicit image size and aspect ratio",
			aspectRatio:     "16:9",
			imageSize:       "2K",
			wantAspectRatio: "16:9",
			wantImageSize:   "2K",
		},
		{
			name:            "explicit fields override legacy size",
			size:            "2048x2048",
			aspectRatio:     "16:9",
			imageSize:       "2K",
			wantAspectRatio: "16:9",
			wantImageSize:   "2K",
		},
		{
			name:            "legacy size remains supported",
			size:            "2048x1152",
			wantAspectRatio: "16:9",
			wantImageSize:   "2K",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aspectRatio, imageSize := parseImageSize(tt.size, tt.aspectRatio, tt.imageSize)
			if aspectRatio != tt.wantAspectRatio || imageSize != tt.wantImageSize {
				t.Fatalf("parseImageSize(%q, %q, %q) = (%q, %q), want (%q, %q)", tt.size, tt.aspectRatio, tt.imageSize, aspectRatio, imageSize, tt.wantAspectRatio, tt.wantImageSize)
			}
		})
	}
}

func TestAsyncImageStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "pending", want: "PENDING"},
		{status: "success", want: "SUCCESS"},
		{status: "failed", want: "FAILED"},
		{status: "abandoned", want: "PENDING"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := asyncImageStatus(&model.EventLog{Status: tt.status}); got != tt.want {
				t.Fatalf("asyncImageStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestAsyncImageSuccessResponseUsesUpstreamAsyncShape(t *testing.T) {
	ev := &model.EventLog{
		ID:       "evt-IMAGE123",
		Status:   "success",
		Provider: "adobe",
		File:     "https://images.example.test/result.png",
		TS:       time.Unix(1_786_431_287, 0),
	}
	response, err := asyncImageSuccessResponse(ev, "https://api.example.test")
	if err != nil {
		t.Fatalf("asyncImageSuccessResponse returned error: %v", err)
	}
	if got, ok := response["status"].(int); !ok || got != 200 {
		t.Fatalf("status = %#v, want 200", response["status"])
	}
	if got, ok := response["statusText"].(string); !ok || got != "" {
		t.Fatalf("statusText = %#v, want empty string", response["statusText"])
	}
	urls, ok := response["imageUrls"].([]string)
	if !ok || len(urls) != 1 || urls[0] != ev.File {
		t.Fatalf("imageUrls = %#v, want one image URL", response["imageUrls"])
	}
	if response["errorPreview"] != nil {
		t.Fatalf("errorPreview = %#v, want nil", response["errorPreview"])
	}
	if got, ok := response["durationMs"].(int); !ok || got != 0 {
		t.Fatalf("durationMs = %#v, want 0", response["durationMs"])
	}
	createdAt, ok := response["createdAt"].(string)
	if !ok {
		t.Fatalf("createdAt = %#v, want RFC3339 timestamp", response["createdAt"])
	}
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		t.Fatalf("createdAt = %q, want RFC3339 timestamp: %v", createdAt, err)
	}
}

func TestIsPublicReferenceIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "8.8.8.8", want: true},
		{ip: "127.0.0.1", want: false},
		{ip: "10.0.0.1", want: false},
		{ip: "169.254.169.254", want: false},
		{ip: "0.0.0.0", want: false},
		{ip: "224.0.0.1", want: false},
		{ip: "::1", want: false},
		{ip: "fe80::1", want: false},
		{ip: "fc00::1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := netip.MustParseAddr(tt.ip)
			if got := isPublicReferenceIP(ip); got != tt.want {
				t.Fatalf("isPublicReferenceIP(%s) = %t, want %t", tt.ip, got, tt.want)
			}
		})
	}
}
