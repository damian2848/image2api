// image2api-updater is a deliberately small host-side update agent. It owns
// repository and Docker access so the web application never needs either.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxCommandOutput = 4000

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	tagPattern        = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z.-]+)?$`)
)

type config struct {
	listen       string
	repoDir      string
	githubRepo   string
	composeFiles []string
	services     []string
	token        string
}

type release struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

type updateStatus struct {
	State          string   `json:"state"`
	Step           string   `json:"step"`
	CurrentVersion string   `json:"current_version"`
	LatestVersion  string   `json:"latest_version,omitempty"`
	HasUpdate      bool     `json:"has_update"`
	Release        *release `json:"release,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	FinishedAt     string   `json:"finished_at,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type server struct {
	cfg    config
	client *http.Client

	mu     sync.RWMutex
	status updateStatus
}

func main() {
	var (
		listen       = flag.String("listen", "127.0.0.1:7070", "HTTP listen address (must stay loopback)")
		repoDir      = flag.String("repo", "", "absolute path of the image2api Git working tree")
		githubRepo   = flag.String("github-repo", "damian2848/image2api", "trusted GitHub repository, owner/name")
		composeFiles = flag.String("compose-files", "docker-compose.yml,docker-compose.prod.yml", "comma-separated compose files, relative to repo")
		services     = flag.String("services", "backend,web", "comma-separated services to rebuild and recreate")
		token        = flag.String("token", os.Getenv("UPDATER_TOKEN"), "shared token (prefer UPDATER_TOKEN environment variable)")
	)
	flag.Parse()

	if !strings.HasPrefix(*listen, "127.0.0.1:") && !strings.HasPrefix(*listen, "[::1]:") {
		log.Fatal("refusing a non-loopback listen address")
	}
	if !filepath.IsAbs(*repoDir) {
		log.Fatal("-repo must be an absolute path")
	}
	if !repositoryPattern.MatchString(*githubRepo) {
		log.Fatal("-github-repo must be owner/name")
	}
	if len(*token) < 32 {
		log.Fatal("UPDATER_TOKEN must be at least 32 characters")
	}

	cleanRepoDir := filepath.Clean(*repoDir)
	if info, err := os.Stat(cleanRepoDir); err != nil || !info.IsDir() {
		log.Fatalf("repository directory unavailable: %v", err)
	}

	s := &server{
		cfg: config{
			listen:       *listen,
			repoDir:      cleanRepoDir,
			githubRepo:   *githubRepo,
			composeFiles: splitList(*composeFiles),
			services:     splitList(*services),
			token:        *token,
		},
		client: &http.Client{Timeout: 15 * time.Second},
		status: updateStatus{State: "idle", Step: "ready"},
	}
	if len(s.cfg.composeFiles) == 0 || len(s.cfg.services) == 0 {
		log.Fatal("at least one compose file and service are required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/status", s.statusHandler)
	mux.HandleFunc("POST /v1/update", s.updateHandler)

	log.Printf("image2api updater listening on %s for %s", s.cfg.listen, s.cfg.githubRepo)
	if err := http.ListenAndServe(s.cfg.listen, s.authorize(mux)); err != nil {
		log.Fatal(err)
	}
}

func splitList(raw string) []string {
	items := strings.Split(raw, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (s *server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		provided := r.Header.Get("X-Image2API-Update-Token")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "invalid updater token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) statusHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.refreshStatus(r.Context(), r.URL.Query().Get("refresh") == "true"); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.snapshot())
}

func (s *server) updateHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.status.State == "updating" {
		status := s.status
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"detail": "an update is already in progress", "data": status})
		return
	}
	s.mu.Unlock()

	if err := s.refreshStatus(r.Context(), true); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": err.Error()})
		return
	}
	status := s.snapshot()
	if !status.HasUpdate || status.Release == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"detail": "already running the latest release"})
		return
	}

	s.mu.Lock()
	s.status.State = "updating"
	s.status.Step = "queued"
	s.status.StartedAt = time.Now().UTC().Format(time.RFC3339)
	s.status.FinishedAt = ""
	s.status.Error = ""
	status = s.status
	s.mu.Unlock()

	go s.apply(status.Release)
	writeJSON(w, http.StatusAccepted, status)
}

func (s *server) refreshStatus(ctx context.Context, force bool) error {
	s.mu.RLock()
	state := s.status.State
	s.mu.RUnlock()
	if state == "updating" || (!force && (state == "succeeded" || state == "failed")) {
		return nil
	}

	current, err := s.git(ctx, "describe", "--tags", "--always", "--dirty")
	if err != nil {
		return fmt.Errorf("read current version: %w", err)
	}
	rel, err := s.latestRelease(ctx)
	if err != nil {
		return err
	}
	if rel.Draft || rel.Prerelease || !tagPattern.MatchString(rel.TagName) {
		return errors.New("GitHub latest release is not a supported stable version tag")
	}

	s.mu.Lock()
	s.status = updateStatus{
		State:          "idle",
		Step:           "ready",
		CurrentVersion: strings.TrimSpace(current),
		LatestVersion:  rel.TagName,
		HasUpdate:      compareVersions(current, rel.TagName) < 0,
		Release:        rel,
	}
	s.mu.Unlock()
	return nil
}

func (s *server) latestRelease(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+s.cfg.githubRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "image2api-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query GitHub release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query GitHub release: unexpected HTTP %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode GitHub release: %w", err)
	}
	return &rel, nil
}

func (s *server) apply(rel *release) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	s.setStep("checking_repository")
	if err := s.verifyOrigin(ctx); err != nil {
		s.fail(err)
		return
	}

	oldRef, _ := s.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	oldCommit, err := s.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		s.fail(err)
		return
	}

	s.setStep("checking_worktree")
	dirty, err := s.git(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		s.fail(fmt.Errorf("check worktree: %w", err))
		return
	}
	if strings.TrimSpace(dirty) != "" {
		s.fail(errors.New("refusing to update: tracked local changes are present"))
		return
	}

	s.setStep("fetching_release")
	if _, err := s.git(ctx, "fetch", "--force", "origin", "refs/tags/"+rel.TagName+":refs/tags/"+rel.TagName); err != nil {
		s.fail(fmt.Errorf("fetch release %s: %w", rel.TagName, err))
		return
	}

	s.setStep("switching_source")
	if _, err := s.git(ctx, "checkout", "--detach", "refs/tags/"+rel.TagName); err != nil {
		s.fail(fmt.Errorf("switch source to %s: %w", rel.TagName, err))
		return
	}

	s.setStep("rebuilding_containers")
	if err := s.compose(ctx); err != nil {
		// Containers keep serving the former image when Compose fails. Put the
		// source tree back too, so the next manual diagnosis starts clean.
		restoreTarget := strings.TrimSpace(oldRef)
		restoreArgs := []string{"checkout"}
		if restoreTarget == "" {
			restoreTarget = strings.TrimSpace(oldCommit)
			restoreArgs = append(restoreArgs, "--detach")
		}
		restoreArgs = append(restoreArgs, restoreTarget)
		if _, restoreErr := s.git(ctx, restoreArgs...); restoreErr != nil {
			s.fail(fmt.Errorf("rebuild failed: %v; source restore also failed: %v", err, restoreErr))
			return
		}
		s.fail(fmt.Errorf("rebuild failed; source restored to %s: %w", restoreTarget, err))
		return
	}

	current, err := s.git(ctx, "describe", "--tags", "--always", "--dirty")
	if err != nil {
		current = rel.TagName
	}
	s.mu.Lock()
	s.status.State = "succeeded"
	s.status.Step = "complete"
	s.status.CurrentVersion = strings.TrimSpace(current)
	s.status.HasUpdate = false
	s.status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.status.Error = ""
	s.mu.Unlock()
}

func (s *server) compose(ctx context.Context) error {
	for _, file := range s.cfg.composeFiles {
		path := filepath.Join(s.cfg.repoDir, file)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("compose file %s: %w", path, err)
		}
	}
	args := s.composeArgs()
	buildArgs := append(append([]string{}, args...), "build")
	buildArgs = append(buildArgs, s.cfg.services...)
	if _, err := run(ctx, s.cfg.repoDir, "docker", buildArgs...); err != nil {
		return err
	}

	upArgs := append(append([]string{}, args...), "up", "-d", "--no-deps")
	upArgs = append(upArgs, s.cfg.services...)
	_, err := run(ctx, s.cfg.repoDir, "docker", upArgs...)
	return err
}

func (s *server) composeArgs() []string {
	args := []string{"compose"}
	for _, file := range s.cfg.composeFiles {
		path := filepath.Join(s.cfg.repoDir, file)
		args = append(args, "-f", path)
	}
	return args
}

func (s *server) verifyOrigin(ctx context.Context) error {
	origin, err := s.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("read git origin: %w", err)
	}
	if githubRepoFromOrigin(origin) != s.cfg.githubRepo {
		return fmt.Errorf("refusing to update: git origin is not github.com/%s", s.cfg.githubRepo)
	}
	return nil
}

func githubRepoFromOrigin(raw string) string {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if strings.HasPrefix(raw, "git@github.com:") {
		return strings.TrimPrefix(raw, "git@github.com:")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

func (s *server) git(ctx context.Context, args ...string) (string, error) {
	return run(ctx, s.cfg.repoDir, "git", args...)
}

func (s *server) setStep(step string) {
	s.mu.Lock()
	s.status.Step = step
	s.mu.Unlock()
}

func (s *server) fail(err error) {
	log.Printf("update failed: %v", err)
	s.mu.Lock()
	s.status.State = "failed"
	s.status.Step = "failed"
	s.status.Error = err.Error()
	s.status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()
}

func (s *server) snapshot() updateStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func run(ctx context.Context, dir, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if len(text) > maxCommandOutput {
		text = text[len(text)-maxCommandOutput:]
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%s timed out", command)
		}
		if text != "" {
			return "", fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), errors.New(text))
		}
		return "", fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	return text, nil
}

func compareVersions(current, latest string) int {
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	latest = strings.TrimPrefix(strings.TrimSpace(latest), "v")
	if current == latest {
		return 0
	}
	// A source revision or a dirty worktree is intentionally treated as older:
	// it can be moved only to the immutable GitHub release selected above.
	if !tagPattern.MatchString("v" + current) {
		return -1
	}
	parse := func(raw string) []int {
		base := strings.SplitN(raw, "-", 2)[0]
		base = strings.SplitN(base, "+", 2)[0]
		chunks := strings.Split(base, ".")
		out := make([]int, 4)
		for i, chunk := range chunks {
			if i >= len(out) {
				break
			}
			for _, r := range chunk {
				out[i] = out[i]*10 + int(r-'0')
			}
		}
		return out
	}
	a, b := parse(current), parse(latest)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	// Stable releases are newer than their prerelease of the same numeric tag.
	if strings.Contains(current, "-") && !strings.Contains(latest, "-") {
		return -1
	}
	if !strings.Contains(current, "-") && strings.Contains(latest, "-") {
		return 1
	}
	return strings.Compare(current, latest)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
