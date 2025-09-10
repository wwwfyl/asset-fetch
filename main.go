package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// ViewState represents the current state of the application
type ViewState int

const (
	StateReleases ViewState = iota
	StateAssets
	StateDownloading
	StateFinished
)

// NavigationHandler handles common navigation keys
type NavigationHandler struct {
	cursor   *int
	maxItems int
}

func (nh NavigationHandler) HandleKey(key string) bool {
	switch key {
	case "up", "k":
		if *nh.cursor > 0 {
			(*nh.cursor)--
		}
		return true
	case "down", "j":
		if *nh.cursor < nh.maxItems-1 {
			(*nh.cursor)++
		}
		return true
	}
	return false
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

// UnifiedListView handles both releases and assets display
type UnifiedListView struct {
	items        []interface{}
	cursor       int
	selected     []bool
	multiSelect  bool
	title        string
	instructions string
}

func (ulv *UnifiedListView) SetReleases(releases []Release) {
	ulv.items = make([]interface{}, len(releases))
	for i, release := range releases {
		ulv.items[i] = release
	}
	ulv.cursor = 0
	ulv.selected = nil
	ulv.multiSelect = false
	ulv.title = "Select release:"
	ulv.instructions = "Press '↑/↓' or 'j/k' to navigate, 'enter' to select, 'q' or 'ctrl+c' to quit"
}

func (ulv *UnifiedListView) SetAssets(assets []AssetInfo) {
	ulv.items = make([]interface{}, len(assets))
	for i, asset := range assets {
		ulv.items[i] = asset
	}
	ulv.cursor = 0
	ulv.selected = make([]bool, len(assets))
	ulv.multiSelect = true
	ulv.title = "Select assets to download (press space to select, enter to download):"
	ulv.instructions = "Press '↑/↓' or 'j/k' to navigate, 'space' to select/deselect, 'enter' to download, 'q' or 'ctrl+c' to quit"
}

func (ulv *UnifiedListView) GetSelectedCount() int {
	if !ulv.multiSelect {
		return 0
	}
	count := 0
	for _, selected := range ulv.selected {
		if selected {
			count++
		}
	}
	return count
}

func (ulv *UnifiedListView) ToggleSelection() {
	if ulv.multiSelect && ulv.cursor < len(ulv.selected) {
		ulv.selected[ulv.cursor] = !ulv.selected[ulv.cursor]
	}
}

func (ulv *UnifiedListView) GetSelectedAssets() []AssetInfo {
	var result []AssetInfo
	if !ulv.multiSelect {
		return result
	}

	for i, selected := range ulv.selected {
		if selected && i < len(ulv.items) {
			if asset, ok := ulv.items[i].(AssetInfo); ok {
				result = append(result, asset)
			}
		}
	}
	return result
}

func (ulv *UnifiedListView) GetCurrentAsset() *AssetInfo {
	if ulv.cursor < len(ulv.items) {
		if asset, ok := ulv.items[ulv.cursor].(AssetInfo); ok {
			return &asset
		}
	}
	return nil
}

func (ulv *UnifiedListView) GetCurrentRelease() *Release {
	if ulv.cursor < len(ulv.items) {
		if release, ok := ulv.items[ulv.cursor].(Release); ok {
			return &release
		}
	}
	return nil
}

func (ulv *UnifiedListView) Render() string {
	s := ulv.title + "\n\n"

	// Styles
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Bold(true)
	defaultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	selectedAssetStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("46")) // Green
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Display items
	for i, item := range ulv.items {
		var line string
		var selectionMarker string

		if release, ok := item.(Release); ok {
			line = fmt.Sprintf("[%s] %s", release.TagName, release.Name)
		} else if asset, ok := item.(AssetInfo); ok {
			line = asset.DisplayLine
			if ulv.multiSelect {
				selectionMarker = " [ ] "
				if i < len(ulv.selected) && ulv.selected[i] {
					selectionMarker = selectedAssetStyle.Render(" [x] ")
				}
			}
		}

		if i == ulv.cursor {
			s += selectedStyle.Render("> "+selectionMarker+line) + "\n"
		} else {
			s += defaultStyle.Render("  "+selectionMarker+line) + "\n"
		}
	}

	// Display selection info for multi-select mode
	if ulv.multiSelect {
		selectedCount := ulv.GetSelectedCount()
		if selectedCount > 0 {
			s += "\n" + infoStyle.Render(fmt.Sprintf("%d asset(s) selected", selectedCount)) + "\n"
		}
	}

	s += "\n" + ulv.instructions + "\n"
	return s
}

// ProgressFormatter handles progress display formatting
type ProgressFormatter struct{}

func (pf ProgressFormatter) FormatProgress(asset AssetInfo, progress DownloadProgress) (string, string) {
	var status, progressInfo string

	if progress.completed {
		status = "[✓]"
		if progress.totalBytes > 0 {
			progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.totalBytes), formatSize(progress.totalBytes))
		} else if asset.Size > 0 {
			progressInfo = fmt.Sprintf("%s / %s", formatSize(asset.Size), formatSize(asset.Size))
		} else {
			progressInfo = formatSize(progress.downloadedBytes) + " / " + formatSize(progress.downloadedBytes)
		}
	} else if progress.downloadedBytes > 0 || progress.totalBytes > 0 {
		status = "[-]"
		totalSize := progress.totalBytes
		if totalSize == 0 && asset.Size > 0 {
			totalSize = asset.Size
		}
		if totalSize > 0 {
			progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.downloadedBytes), formatSize(totalSize))
		} else {
			progressInfo = formatSize(progress.downloadedBytes) + " / Unknown"
		}
	} else {
		status = "[ ]"
		if asset.Size > 0 {
			progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
		} else {
			progressInfo = "0B / Unknown"
		}
	}

	return status, progressInfo
}

func (pf ProgressFormatter) RenderProgressTable(assets []AssetInfo, progresses []DownloadProgress) string {
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
	s := headerStyle.Render("Filename                                 Status          Tag                            Progress") + "\n"

	for i, asset := range assets {
		var progress DownloadProgress
		if i < len(progresses) {
			progress = progresses[i]
		}

		status, progressInfo := pf.FormatProgress(asset, progress)

		s += fmt.Sprintf("%-40s %-15s %-30s %s\n",
			truncateString(asset.Name, 40),
			status,
			truncateString(asset.ReleaseTag, 30),
			progressInfo)
	}

	return s
}

// AssetFormatter handles asset information formatting
type AssetFormatter struct{}

func (af AssetFormatter) FormatAssetInfo(asset Asset, release Release) AssetInfo {
	formattedDate := formatCreatedAt(asset.CreatedAt)
	sizeStr := formatSize(asset.Size)

	return AssetInfo{
		Name:          asset.Name,
		ID:            asset.ID,
		URL:           asset.URL,
		DownloadURL:   asset.BrowserDownloadURL,
		Size:          asset.Size,
		CreatedAt:     asset.CreatedAt,
		ReleaseTag:    release.TagName,
		ReleaseName:   release.Name,
		FormattedDate: formattedDate,
		SizeStr:       sizeStr,
		DisplayLine:   af.createDisplayLine(asset.Name, sizeStr, formattedDate, release.TagName),
	}
}

func (af AssetFormatter) createDisplayLine(name, sizeStr, formattedDate, releaseTag string) string {
	if releaseTag != "" {
		return fmt.Sprintf("[%s] %s (%s, %s)", releaseTag, name, sizeStr, formattedDate)
	}
	return fmt.Sprintf("%s (%s, %s)", name, sizeStr, formattedDate)
}

func (af AssetFormatter) createDisplayLineWithoutTag(name, sizeStr, formattedDate string) string {
	return fmt.Sprintf("%s (%s, %s)", name, sizeStr, formattedDate)
}

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

// truncateString truncates a string to the specified length and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Model structure for bubbletea - simplified unified version
type model struct {
	// Unified state management
	state    ViewState
	loading  bool
	quitting bool
	errorMsg string

	// Unified list view
	listView UnifiedListView

	// Download queue (always used, even for single downloads)
	downloadQueue    DownloadQueue
	downloading      bool
	downloadFinished bool
	downloadSuccess  bool
	downloadResult   string

	// Helper components
	assetFormatter    AssetFormatter
	progressFormatter ProgressFormatter

	// Legacy fields for compatibility during transition
	releases []Release
}

// Init bubbletea initialization
func (m model) Init() tea.Cmd {
	return fetchReleases
}

// Update bubbletea message processing - unified version
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.downloading {
				// Cancel download
				if downloadCancel != nil {
					downloadCancel()
				}
				return m, func() tea.Msg {
					return cancelDownloadMsg{}
				}
			} else {
				m.quitting = true
				return m, tea.Quit
			}
		}

		// Handle state-specific navigation and actions
		switch m.state {
		case StateReleases:
			return m.handleReleasesInput(msg.String())
		case StateAssets:
			return m.handleAssetsInput(msg.String())
		case StateDownloading, StateFinished:
			// No input handling during download states
			return m, nil
		}

	case releasesMsg:
		// Check if we have filtered assets (ASSET_MASK was used)
		if len(msg.assets) > 0 {
			// Show filtered assets directly
			m.listView.SetAssets(msg.assets)
			m.state = StateAssets
			m.loading = false
		} else {
			// Show releases list
			m.listView.SetReleases(msg.releases)
			m.releases = msg.releases
			m.state = StateReleases
			m.loading = false
		}

	case errorMsg:
		m.errorMsg = string(msg)
		m.loading = false

	case startDownloadProgressMsg:
		// Start download progress updates
		m.downloading = true
		m.state = StateDownloading
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			if !m.downloadQueue.IsEmpty() {
				return updateDownloadProgressMsg{asset: msg.asset}
			}
			return nil
		})

	case updateDownloadProgressMsg:
		// Update download progress
		downloadProgressMutex.Lock()
		progress := downloadProgress
		downloadProgressMutex.Unlock()

		// Update download queue progress
		m.downloadQueue.UpdateProgress(progress, msg.asset.Size)

		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			if m.downloading {
				return updateDownloadProgressMsg{asset: msg.asset}
			}
			return nil
		})

	case downloadCompleteMsg:
		m.downloading = false

		// Get actual file size from filesystem for completed download
		var actualSize int64
		if fileInfo, err := os.Stat(string(msg)); err == nil {
			actualSize = fileInfo.Size()
		}

		// Mark current download as completed with actual file size
		m.downloadQueue.CompleteCurrentDownload(actualSize)

		// Move to next download in queue
		if m.downloadQueue.NextDownload() {
			// Start next download
			asset := m.downloadQueue.GetCurrent()
			return m, tea.Batch(
				func() tea.Msg {
					return startDownloadProgressMsg{asset: *asset}
				},
				downloadAsset(*asset),
			)
		} else {
			// All downloads completed
			m.downloadFinished = true
			m.downloadSuccess = true
			m.downloadResult = "All files downloaded successfully"
			m.state = StateFinished
			// Exit after showing results
			return m, tea.Quit
		}

	case downloadErrorMsg:
		m.downloading = false

		// Move to next download in queue
		if m.downloadQueue.NextDownload() {
			// Start next download
			asset := m.downloadQueue.GetCurrent()
			return m, tea.Batch(
				func() tea.Msg {
					return startDownloadProgressMsg{asset: *asset}
				},
				downloadAsset(*asset),
			)
		} else {
			// All downloads completed (with errors)
			m.downloadFinished = true
			m.downloadSuccess = false
			m.downloadResult = "Downloads completed with errors"
			m.state = StateFinished
			// Exit after showing results
			return m, tea.Quit
		}

	case cancelDownloadMsg:
		m.downloading = false
		m.errorMsg = "Download cancelled by user"
		m.state = StateAssets
	}

	return m, nil
}

// Handle input when in releases state
func (m model) handleReleasesInput(key string) (tea.Model, tea.Cmd) {
	navHandler := NavigationHandler{
		cursor:   &m.listView.cursor,
		maxItems: len(m.listView.items),
	}

	if navHandler.HandleKey(key) {
		return m, nil
	}

	switch key {
	case "enter", " ":
		if selectedRelease := m.listView.GetCurrentRelease(); selectedRelease != nil {
			// Convert release assets to AssetInfo and show asset selection
			var assets []AssetInfo
			for _, asset := range selectedRelease.Assets {
				assetInfo := m.assetFormatter.FormatAssetInfo(asset, *selectedRelease)
				assetInfo.DisplayLine = m.assetFormatter.createDisplayLineWithoutTag(asset.Name, assetInfo.SizeStr, assetInfo.FormattedDate)
				assets = append(assets, assetInfo)
			}

			m.listView.SetAssets(assets)
			m.state = StateAssets
		}
	}

	return m, nil
}

// Handle input when in assets state
func (m model) handleAssetsInput(key string) (tea.Model, tea.Cmd) {
	navHandler := NavigationHandler{
		cursor:   &m.listView.cursor,
		maxItems: len(m.listView.items),
	}

	if navHandler.HandleKey(key) {
		return m, nil
	}

	switch key {
	case " ":
		// Toggle selection
		m.listView.ToggleSelection()

	case "enter":
		// Start downloading selected assets or current asset if none selected
		selectedAssets := m.listView.GetSelectedAssets()

		// If no assets selected, add current asset to queue
		if len(selectedAssets) == 0 {
			if currentAsset := m.listView.GetCurrentAsset(); currentAsset != nil {
				selectedAssets = []AssetInfo{*currentAsset}
			}
		}

		if len(selectedAssets) > 0 {
			// Initialize download queue with selected assets
			m.downloadQueue.Reset()
			m.downloadQueue.AddMultiple(selectedAssets)

			// Start first download
			if !m.downloadQueue.IsEmpty() {
				asset := m.downloadQueue.GetCurrent()
				return m, tea.Batch(
					func() tea.Msg {
						return startDownloadProgressMsg{asset: *asset}
					},
					downloadAsset(*asset),
				)
			}
		}
	}

	return m, nil
}

// View interface display - unified version
func (m model) View() string {
	switch m.state {
	case StateReleases:
		return m.listView.Render()
	case StateAssets:
		return m.listView.Render()
	case StateDownloading:
		s := "Download progress:\n\n"
		s += m.progressFormatter.RenderProgressTable(m.downloadQueue.assets, m.downloadQueue.progress)
		return s
	case StateFinished:
		s := "Download results:\n\n"
		s += m.progressFormatter.RenderProgressTable(m.downloadQueue.assets, m.downloadQueue.progress)
		s += "\n" + m.downloadResult + "\n"
		return s
	}

	// Default states
	switch {
	case m.quitting:
		return "Goodbye!\n"
	case m.loading:
		return "Searching for available artifacts...\n"
	case m.errorMsg != "":
		return fmt.Sprintf("Error: %s\n", m.errorMsg)
	default:
		return "No artifacts found\n"
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

// loadConfig load configuration
func loadConfig() (*Config, error) {
	scriptDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return nil, err
	}

	configFile := filepath.Join(scriptDir, "afetch.conf")
	homeConfigFile := filepath.Join(os.Getenv("HOME"), ".config", "afetch.conf")

	// Check if configuration file exists
	var fileToRead string
	if _, err := os.Stat(configFile); err == nil {
		fileToRead = configFile
	} else if _, err := os.Stat(homeConfigFile); err == nil {
		fileToRead = homeConfigFile
	} else {
		return nil, fmt.Errorf("configuration file not found in %s or %s", configFile, homeConfigFile)
	}

	// Read configuration file
	content, err := os.ReadFile(fileToRead)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	lines := strings.Split(string(content), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}

		switch key {
		case "GITHUB_TOKEN":
			config.GitHubToken = value
		case "REPO_OWNER":
			config.RepoOwner = value
		case "REPO_NAME":
			config.RepoName = value
		case "ASSET_MASK":
			config.AssetMask = value
		}
	}

	return config, nil
}

func main() {
	// Create context with cancel function
	downloadContext, downloadCancel = context.WithCancel(context.Background())
	defer downloadCancel()

	// Initialize unified model
	m := model{
		loading: true,
		state:   StateReleases,
	}

	// Run bubbletea
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Check if context was cancelled
	if errors.Is(downloadContext.Err(), context.Canceled) {
		fmt.Println("Download cancelled by user")
		os.Exit(0)
	}
}
