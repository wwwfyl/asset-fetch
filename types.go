package main

import (
	"context"
	"io"
	"sync"
)

// Global context and cancel function for download cancellation
var downloadContext context.Context
var downloadCancel context.CancelFunc

// Global variable for download progress
var downloadProgress int64
var downloadProgressMutex sync.Mutex

// Config structure for storing configuration
type Config struct {
	GitHubToken string
	RepoOwner   string
	RepoName    string
	AssetMask   string
}

// Asset structure for storing artifact information
type Asset struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	CreatedAt          string `json:"created_at"`
}

// Release structure for storing release information
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// AssetInfo structure for storing artifact information
type AssetInfo struct {
	Name          string
	ID            int
	URL           string
	DownloadURL   string
	Size          int64
	CreatedAt     string
	ReleaseTag    string
	ReleaseName   string
	FormattedDate string
	SizeStr       string
	DisplayLine   string
}

// DownloadProgress structure for tracking download progress
type DownloadProgress struct {
	downloadedBytes int64
	totalBytes      int64
	completed       bool
}

// ProgressReader structure for tracking download progress
type ProgressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	onProgress func(downloaded, total int64)
}

// Read implements io.Reader interface
func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.downloaded += int64(n)

	// Call onProgress callback if provided
	if pr.onProgress != nil {
		pr.onProgress(pr.downloaded, pr.total)
	}

	return n, err
}

// DownloadQueue manages the download queue and progress
type DownloadQueue struct {
	assets       []AssetInfo
	progress     []DownloadProgress
	currentIndex int
}

func (dq *DownloadQueue) Add(asset AssetInfo) {
	dq.assets = append(dq.assets, asset)
	dq.progress = append(dq.progress, DownloadProgress{})
}

func (dq *DownloadQueue) AddMultiple(assets []AssetInfo) {
	for _, asset := range assets {
		dq.Add(asset)
	}
}

func (dq *DownloadQueue) GetCurrent() *AssetInfo {
	if dq.currentIndex >= 0 && dq.currentIndex < len(dq.assets) {
		return &dq.assets[dq.currentIndex]
	}
	return nil
}

func (dq *DownloadQueue) UpdateProgress(downloaded, total int64) {
	if dq.currentIndex >= 0 && dq.currentIndex < len(dq.progress) {
		dq.progress[dq.currentIndex] = DownloadProgress{
			downloadedBytes: downloaded,
			totalBytes:      total,
			completed:       downloaded >= total && total > 0,
		}
	}
}

func (dq *DownloadQueue) CompleteCurrentDownload(actualSize int64) {
	if dq.currentIndex >= 0 && dq.currentIndex < len(dq.progress) {
		finalSize := actualSize
		if finalSize == 0 {
			finalSize = dq.assets[dq.currentIndex].Size
		}
		if finalSize == 0 {
			finalSize = dq.progress[dq.currentIndex].downloadedBytes
		}

		dq.progress[dq.currentIndex] = DownloadProgress{
			downloadedBytes: finalSize,
			totalBytes:      finalSize,
			completed:       true,
		}
	}
}

func (dq *DownloadQueue) NextDownload() bool {
	dq.currentIndex++
	return dq.currentIndex < len(dq.assets)
}

func (dq *DownloadQueue) IsEmpty() bool {
	return len(dq.assets) == 0
}

func (dq *DownloadQueue) Reset() {
	dq.assets = []AssetInfo{}
	dq.progress = []DownloadProgress{}
	dq.currentIndex = 0
}

// ViewState represents the current state of the application
type ViewState int

const (
	StateReleases ViewState = iota
	StateAssets
	StateDownloading
	StateFinished
)

// Custom messages
type errorMsg string

type releasesData struct {
	assets   []AssetInfo
	releases []Release
}

type releasesMsg releasesData
type downloadCompleteMsg string
type downloadErrorMsg string
type cancelDownloadMsg struct{}

// startDownloadProgressMsg message to start download progress updates
type startDownloadProgressMsg struct {
	asset AssetInfo
}

// updateDownloadProgressMsg message to update download progress
type updateDownloadProgressMsg struct {
	asset AssetInfo
}
