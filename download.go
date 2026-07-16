package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// downloadAsset downloads an asset via HTTP into destDir and returns a bubbletea message.
// token is the GitHub personal access token (may be empty for public repos).
// progress is updated concurrently as bytes arrive; the tick loop reads it from the model.
func downloadAsset(ctx context.Context, asset AssetInfo, token, destDir string, progress *ProgressState) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
		if err != nil {
			return downloadErrorMsg(fmt.Sprintf("Error creating request: %v", err))
		}
		req.Header.Set("Accept", "application/octet-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return downloadErrorMsg("Download cancelled by user")
			}
			return downloadErrorMsg(fmt.Sprintf("Error downloading file: %v", err))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return downloadErrorMsg(fmt.Sprintf("HTTP error: %d", resp.StatusCode))
		}

		filePath := asset.Name
		if destDir != "" {
			filePath = destDir + string(os.PathSeparator) + asset.Name
		}

		out, err := os.Create(filePath)
		if err != nil {
			return downloadErrorMsg(fmt.Sprintf("Error creating file: %v", err))
		}
		defer out.Close()

		progressReader := &ProgressReader{
			reader: resp.Body,
			total:  asset.Size,
			onProgress: func(downloaded, total int64) {
				progress.Update(downloaded, total)
			},
		}

		_, err = io.Copy(out, progressReader)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				os.Remove(filePath) //nolint:errcheck
				return downloadErrorMsg("Download cancelled by user")
			}
			os.Remove(filePath) //nolint:errcheck
			return downloadErrorMsg(fmt.Sprintf("Error writing file: %v", err))
		}

		if err := verifyChecksum(filePath, asset.Digest); err != nil {
			os.Remove(filePath) //nolint:errcheck
			return downloadErrorMsg(fmt.Sprintf("Checksum verification failed for %s: %v", filePath, err))
		}

		return downloadCompleteMsg{filename: filePath}
	}
}

// fetchReleases fetches GitHub releases and returns a bubbletea message with either
// a release list or a pre-filtered asset list (when ASSET_MASK is set).
// All parameters are read from model fields; no config file is loaded here.
func fetchReleases(m model) tea.Cmd {
	return func() tea.Msg {
		if m.repoOwner == "" || m.repoName == "" {
			return errorMsg("missing repo: pass a GitHub releases URL or run from the dashboard")
		}

		releases, err := fetchReleasesFromGitHub(m.downloadCtx, m.repoOwner, m.repoName, m.tag, m.gitHubToken)
		if err != nil {
			return errorMsg(err.Error())
		}

		// Tag-specific fetch: expose assets from that single release directly.
		if m.tag != "" {
			release := releases[0]
			var assets []AssetInfo
			formatter := AssetFormatter{}
			for _, asset := range release.Assets {
				assetInfo := formatter.FormatAssetInfo(asset, release)
				assetInfo.DisplayLine = formatter.createDisplayLineWithoutTag(asset.Name, assetInfo.SizeStr, assetInfo.FormattedDate)
				assets = append(assets, assetInfo)
			}
			return releasesMsg{assets: assets, releases: releases}
		}

		assetMaskValue := ""
		if m.assetMask != nil {
			assetMaskValue = *m.assetMask
		}

		if assetMaskValue == "" || m.startWithReleases {
			return releasesMsg{releases: releases}
		}

		// Filter assets by ASSET_MASK across all releases.
		var assets []AssetInfo
		formatter := AssetFormatter{}
		for _, release := range releases {
			for _, asset := range release.Assets {
				matched, err := path.Match(assetMaskValue, asset.Name)
				if err != nil || !matched {
					continue
				}
				assetInfo := formatter.FormatAssetInfo(asset, release)
				assets = append(assets, assetInfo)
			}
		}

		if len(assets) == 0 {
			return errorMsg("artifacts not found")
		}

		return releasesMsg{assets: assets, releases: releases}
	}
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

// calculateSHA256 calculates the SHA256 hash of a file
func calculateSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Printf("Warning: failed to close file: %v\n", closeErr)
		}
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// verifyChecksum verifies the SHA256 checksum of a downloaded file
func verifyChecksum(filename string, expectedDigest string) error {
	// If no digest is provided, skip verification
	if expectedDigest == "" {
		return nil
	}

	// Extract the actual digest from the expectedDigest string
	// GitHub API returns digest in format "sha256:abcdef..."
	parts := strings.Split(expectedDigest, ":")
	if len(parts) != 2 || parts[0] != "sha256" {
		return fmt.Errorf("invalid digest format: %s", expectedDigest)
	}
	expectedSHA256 := parts[1]

	// Calculate actual SHA256 of the file
	actualSHA256, err := calculateSHA256(filename)
	if err != nil {
		return fmt.Errorf("error calculating checksum: %v", err)
	}

	// Compare checksums
	if actualSHA256 != expectedSHA256 {
		return fmt.Errorf("checksum verification failed: expected %s, got %s", expectedSHA256, actualSHA256)
	}

	return nil
}
