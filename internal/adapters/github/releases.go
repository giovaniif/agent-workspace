package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

const ReleasesURL = "https://api.github.com/repos/giovaniif/crew/releases?per_page=10"

type ReleaseChecker struct {
	URL       string
	CachePath string
	TTL       time.Duration
	Client    *http.Client
	Now       func() time.Time
}

type releaseCache struct {
	Tag     string    `json:"tag"`
	Checked time.Time `json:"checked"`
}

func (c ReleaseChecker) Latest(ctx context.Context) (string, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	ttl := c.TTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	if b, err := os.ReadFile(c.CachePath); err == nil {
		var cached releaseCache
		if json.Unmarshal(b, &cached) == nil && cached.Tag != "" && now().Sub(cached.Checked) < ttl {
			return cached.Tag, nil
		}
	}
	tag, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}
	if b, err := json.Marshal(releaseCache{Tag: tag, Checked: now()}); err == nil {
		_ = os.MkdirAll(filepath.Dir(c.CachePath), 0o700)
		_ = os.WriteFile(c.CachePath, b, 0o600)
	}
	return tag, nil
}

func (c ReleaseChecker) fetch(ctx context.Context) (string, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("releases: %s", resp.Status)
	}
	var releases []struct {
		Tag   string `json:"tag_name"`
		Draft bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", err
	}
	var tags []string
	for _, r := range releases {
		if !r.Draft && r.Tag != "" {
			tags = append(tags, r.Tag)
		}
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("releases: none published")
	}
	return domain.HighestRelease(tags), nil
}
