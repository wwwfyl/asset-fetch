package main

import (
	"io"
	"sync"
)

// ProgressState is a thread-safe container for a single download's byte counters.
// Held by pointer in model so bubbletea model copies share the same instance.
type ProgressState struct {
	mu         sync.Mutex
	downloaded int64
	total      int64
}

// Update sets the current byte counters; safe to call from any goroutine.
func (ps *ProgressState) Update(downloaded, total int64) {
	ps.mu.Lock()
	ps.downloaded = downloaded
	ps.total = total
	ps.mu.Unlock()
}

// Get returns the current byte counters; safe to call from any goroutine.
func (ps *ProgressState) Get() (downloaded, total int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.downloaded, ps.total
}

// Config is the legacy single-app key=value config (kept for migration to GlobalConfig).
type Config struct {
	GitHubToken string
	RepoOwner   string
	RepoName    string
	AssetMask   string
}

// GlobalConfig is the top-level YAML config: a global token and a list of tracked apps.
type GlobalConfig struct {
	GitHubToken string      `yaml:"github_token"`
	Apps        []AppConfig `yaml:"apps"`
}

// AppConfig is one tracked application: where to fetch it from, how to detect the local
// version, which asset to download, and what to do with it after download.
type AppConfig struct {
	Name        string          `yaml:"name"`
	Repo        string          `yaml:"repo"`         // "owner/repo"
	ReleaseType string          `yaml:"release_type"` // latest | latest-stable | pre-release
	AssetMask   string          `yaml:"asset_mask"`
	InstallDir  string          `yaml:"install_dir"`  // optional; default depends on euid
	GitHubToken string          `yaml:"github_token"` // optional per-app override
	Version     VersionConfig   `yaml:"version"`
	Install     InstallConfig   `yaml:"install"`
	Uninstall   UninstallConfig `yaml:"uninstall"`
}

// VersionConfig describes how to detect the currently installed version of an app.
type VersionConfig struct {
	Command string `yaml:"command"` // full command line, e.g. "lazygit --version"
	Regex   string `yaml:"regex"`   // first capture group is the version string
}

// InstallConfig groups the unpack and install phases. Both are lists of shell
// steps executed in order; unpack runs first (typically to extract the asset)
// and steps follows (typically to move files into INSTALL_DIR).
type InstallConfig struct {
	Unpack []InstallStep `yaml:"unpack"`
	Steps  []InstallStep `yaml:"steps"`
}

// UninstallConfig describes how to remove an installed app.
type UninstallConfig struct {
	Steps []InstallStep `yaml:"steps"`
}

// InstallStep is one shell command to run during install/uninstall.
// Run is executed as `sh -c "$run"` with $ASSET_FILE, $WORK_DIR, $INSTALL_DIR, $VERSION set.
type InstallStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

// Asset structure for storing artifact information
type Asset struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	CreatedAt          string `json:"created_at"`
	Digest             string `json:"digest"`
}

// Release structure for storing release information
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// AssetInfo structure for storing artifact information
type AssetInfo struct {
	Name          string
	ID            int
	URL           string
	DownloadURL   string
	Size          int64
	CreatedAt     string
	Digest        string
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
	StateDashboard ViewState = iota
	StateReleases
	StateAssets
	StateDownloading
	StateFinished
)

// TileStatus tracks the per-app card lifecycle on the dashboard.
type TileStatus int

const (
	TileStatusLoading TileStatus = iota
	TileStatusReady
	TileStatusError
	TileStatusUpdating
	TileStatusUninstalling
)

// TileInfo is the rendered state of one dashboard card.
type TileInfo struct {
	Name             string
	LatestVersion    string
	InstalledVersion string
	Status           TileStatus
	Err              string
}

// Custom messages
type errorMsg string

type releasesData struct {
	assets   []AssetInfo
	releases []Release
}

type releasesMsg releasesData
type downloadErrorMsg string
type cancelDownloadMsg struct{}

// checksumVerifiedMsg message to indicate checksum verification result
type checksumVerifiedMsg struct {
	filename string
	success  bool
	err      string
}

// startDownloadProgressMsg message to start download progress updates
type startDownloadProgressMsg struct {
	asset AssetInfo
}

// updateDownloadProgressMsg message to update download progress
type updateDownloadProgressMsg struct {
	asset AssetInfo
}

// tileUpdatedMsg is emitted by background fetches to update one dashboard tile.
type tileUpdatedMsg struct {
	index            int
	latestVersion    string
	installedVersion string
	err              string
}
