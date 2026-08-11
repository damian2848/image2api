package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrUpdaterDisabled = errors.New("online updater is not configured")

// ReleaseInfo mirrors the public fields returned by the host-side updater.
// It is intentionally display-only: the browser cannot select a repository,
// tag, source path, or Docker command.
type ReleaseInfo struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

type UpdateStatus struct {
	State          string       `json:"state"`
	Step           string       `json:"step"`
	CurrentVersion string       `json:"current_version"`
	LatestVersion  string       `json:"latest_version,omitempty"`
	HasUpdate      bool         `json:"has_update"`
	Release        *ReleaseInfo `json:"release,omitempty"`
	StartedAt      string       `json:"started_at,omitempty"`
	FinishedAt     string       `json:"finished_at,omitempty"`
	Error          string       `json:"error,omitempty"`
}

type UpdaterError struct {
	Status int
	Detail string
}

func (e *UpdaterError) Error() string { return e.Detail }

// SystemUpdateService proxies the two small updater operations to the
// loopback-only host agent. Docker and Git permissions never enter this
// application container.
type SystemUpdateService struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewSystemUpdateService(baseURL, token string) *SystemUpdateService {
	return &SystemUpdateService{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		client:  &http.Client{Timeout: 12 * time.Second},
	}
}

func (s *SystemUpdateService) Enabled() bool {
	return s.baseURL != "" && s.token != ""
}

func (s *SystemUpdateService) Status(ctx context.Context, force bool) (*UpdateStatus, error) {
	var status UpdateStatus
	path := "/v1/status"
	if force {
		path += "?refresh=true"
	}
	if err := s.request(ctx, http.MethodGet, path, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (s *SystemUpdateService) Start(ctx context.Context) (*UpdateStatus, error) {
	var status UpdateStatus
	if err := s.request(ctx, http.MethodPost, "/v1/update", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (s *SystemUpdateService) request(ctx context.Context, method, path string, body any, out any) error {
	if !s.Enabled() {
		return ErrUpdaterDisabled
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build updater request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Image2API-Update-Token", s.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("contact host updater: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read updater response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var problem struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(data, &problem)
		if problem.Detail == "" {
			problem.Detail = "host updater returned HTTP " + resp.Status
		}
		return &UpdaterError{Status: resp.StatusCode, Detail: problem.Detail}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode updater response: %w", err)
	}
	return nil
}
