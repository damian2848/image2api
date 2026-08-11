package main

import "testing"

func TestGitHubRepoFromOrigin(t *testing.T) {
	cases := map[string]string{
		"https://github.com/damian2848/image2api.git":   "damian2848/image2api",
		"git@github.com:damian2848/image2api.git":       "damian2848/image2api",
		"ssh://git@github.com/damian2848/image2api.git": "damian2848/image2api",
		"https://example.com/damian2848/image2api.git":  "",
		"git@github.com:damian2848/not-image2api.git":   "damian2848/not-image2api",
	}
	for origin, want := range cases {
		if got := githubRepoFromOrigin(origin); got != want {
			t.Errorf("githubRepoFromOrigin(%q) = %q, want %q", origin, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.1.0", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"a1b2c3d", "v1.0.0", -1},
	}
	for _, tc := range cases {
		got := compareVersions(tc.current, tc.latest)
		if (got < 0 && tc.want < 0) || (got == 0 && tc.want == 0) || (got > 0 && tc.want > 0) {
			continue
		}
		t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tc.current, tc.latest, got, tc.want)
	}
}
