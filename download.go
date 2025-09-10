package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// downloadAsset download artifact using http.Client
func downloadAsset(asset AssetInfo) tea.Cmd {
	return func() tea.Msg {
		config, err := loadConfig()
		if err != nil {
			return downloadErrorMsg(err.Error())
		}

		// Create HTTP client with context
		client := &http.Client{}

		// Create request with context
		req, err := http.NewRequestWithContext(downloadContext, "GET", asset.URL, nil)
		if err != nil {
			return downloadErrorMsg(fmt.Sprintf("Error creating request: %v", err))
		}

		// Set headers
		req.Header.Set("Accept", "application/octet-stream")
		// Only add authorization header if token is provided
		if config.GitHubToken != "" {
			req.Header.Set("Authorization", "Bearer "+config.GitHubToken)
		}
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			// Check if the error is due to context cancellation
			if errors.Is(downloadContext.Err(), context.Canceled) {
				return downloadErrorMsg("Download cancelled by user")
			}
			return downloadErrorMsg(fmt.Sprintf("Error downloading file: %v", err))
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				// Log the error but don't return it as it's in defer
			}
		}()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			return downloadErrorMsg(fmt.Sprintf("HTTP error: %d", resp.StatusCode))
		}

		// Create output file
		out, err := os.Create(asset.Name)
		if err != nil {
			return downloadErrorMsg(fmt.Sprintf("Error creating file: %v", err))
		}
		defer func() {
			if closeErr := out.Close(); closeErr != nil {
				// Log the error but don't return it as it's in defer
			}
		}()

		// Create a progress reader
		progressReader := &ProgressReader{
			reader: resp.Body,
			total:  asset.Size,
			onProgress: func(downloaded, total int64) {
				// Update global progress variable
				downloadProgressMutex.Lock()
				downloadProgress = downloaded
				downloadProgressMutex.Unlock()
			},
		}

		// Copy response body to file
		_, err = io.Copy(out, progressReader)
		if err != nil {
			// Check if the error is due to context cancellation
			if errors.Is(downloadContext.Err(), context.Canceled) {
				// Clean up partial file
				if removeErr := os.Remove(asset.Name); removeErr != nil {
					// Log the error but don't return it as we already have a cancellation error
				}
				return downloadErrorMsg("Download cancelled by user")
			}
			// Clean up partial file
			if removeErr := os.Remove(asset.Name); removeErr != nil {
				// Log the error but don't return it as we already have a write error
			}
			return downloadErrorMsg(fmt.Sprintf("Error writing file: %v", err))
		}

		return downloadCompleteMsg(asset.Name)
	}
}

// fetchReleases get list of releases with ASSET_MASK filtering
func fetchReleases() tea.Msg {
	config, err := loadConfig()
	if err != nil {
		return errorMsg(err.Error())
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", config.RepoOwner, config.RepoName)

	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return errorMsg(err.Error())
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	// Only add authorization header if token is provided
	if config.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.GitHubToken)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return errorMsg(err.Error())
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			// Log the error but don't return it as it's in defer
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return errorMsg(fmt.Sprintf("GitHub API error: %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorMsg(err.Error())
	}

	var releases []Release
	err = json.Unmarshal(body, &releases)
	if err != nil {
		return errorMsg(err.Error())
	}

	// If AssetMask is empty, return releases list
	if config.AssetMask == "" {
		return releasesMsg{releases: releases}
	}

	// Filter assets by ASSET_MASK
	var assets []AssetInfo
	formatter := AssetFormatter{}

	for _, release := range releases {
		for _, asset := range release.Assets {
			// Use asset mask from config
			assetMask := config.AssetMask

			// Parse the mask into prefix and suffix
			parts := strings.Split(assetMask, "*")
			var prefix, suffix string
			if len(parts) == 2 {
				prefix = parts[0]
				suffix = parts[1]
			} else {
				// If no asterisk or multiple asterisks, use the whole mask as prefix
				prefix = assetMask
			}

			// Check if asset name matches the mask
			if strings.HasPrefix(asset.Name, prefix) && strings.HasSuffix(asset.Name, suffix) {
				assetInfo := formatter.FormatAssetInfo(asset, release)
				assets = append(assets, assetInfo)
			}
		}
	}

	if len(assets) == 0 {
		return errorMsg("artifacts not found")
	}

	return releasesMsg{assets: assets, releases: releases}
}

// formatCreatedAt format creation date
func formatCreatedAt(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return createdAt
	}
	return t.Format("2006-01-02 15:04")
}

// formatSize format file size
func formatSize(size int64) string {
	if size <= 0 {
		return "Unknown"
	}

	switch {
	case size >= 1024*1024*1024:
		return fmt.Sprintf("%.1fGB", float64(size)/(1024*1024*1024))
	case size >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.1fKB", float64(size)/1024)
	default:
		return fmt.Sprintf("%dB", size)
	}
}
