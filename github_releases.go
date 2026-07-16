package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// apiClient bounds GitHub API calls so a stalled network cannot leave
// dashboard tiles loading forever. Asset downloads keep http.DefaultClient:
// a whole-request timeout would kill legitimately long transfers.
var apiClient = &http.Client{Timeout: 30 * time.Second}

// fetchReleasesFromGitHub returns releases from the GitHub REST API for the given repo.
// If tag is non-empty, only that release is fetched and returned as a single-element slice.
func fetchReleasesFromGitHub(ctx context.Context, owner, repo, tag, token string) ([]Release, error) {
	var apiURL string
	if tag != "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if tag != "" {
		var release Release
		if err := json.Unmarshal(body, &release); err != nil {
			return nil, err
		}
		return []Release{release}, nil
	}

	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}
