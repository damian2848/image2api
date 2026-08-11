package service

import "testing"

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
