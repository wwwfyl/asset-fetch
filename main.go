package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	GITHUB_TOKEN string
	REPO_OWNER   string
	REPO_NAME    string
	ASSET_MASK   string
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

// DownloadState structure for tracking download progress
type DownloadState struct {
	totalBytes    int64
	expectedBytes int64
	error         error
	completed     bool
	mutex         sync.Mutex
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

// Model structure for bubbletea
type model struct {
	assets           []AssetInfo
	cursor           int
	quitting         bool
	loading          bool
	errorMsg         string
	downloading      bool
	downloadMsg      string
	confirming       bool
	confirmAsset     *AssetInfo
	downloadState    *DownloadState
	downloadAsset    *AssetInfo
	downloadProgress int64
}

// Init bubbletea initialization
func (m model) Init() tea.Cmd {
	return fetchReleases
}

// Update bubbletea message processing
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If we are in confirmation state, handle special keys
		if m.confirming {
			switch msg.String() {
			case "y", "Y":
				// Confirm download
				m.confirming = false
				asset := *m.confirmAsset
				m.confirmAsset = nil
				return m, tea.Batch(
					func() tea.Msg {
						return startDownloadProgressMsg{asset: asset}
					},
					downloadAsset(asset),
				)
			case "n", "N", "esc", "q", "ctrl+c":
				// Cancel download
				m.confirming = false
				m.confirmAsset = nil
				return m, nil
			default:
				// Ignore other keys
				return m, nil
			}
		}

		// Regular key handling
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
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.assets)-1 {
				m.cursor++
			}
		case "enter", " ":
			if len(m.assets) > 0 {
				// Confirm download
				return m, tea.Batch(
					tea.Printf("Selected artifact: %s", m.assets[m.cursor].Name),
					confirmDownload(m.assets[m.cursor]),
				)
			}
		}
	case releasesMsg:
		m.assets = msg
		m.loading = false
		sort.Slice(m.assets, func(i, j int) bool {
			return m.assets[i].CreatedAt > m.assets[j].CreatedAt
		})
	case errorMsg:
		m.errorMsg = string(msg)
		m.loading = false
	case downloadConfirmMsg:
		asset := AssetInfo(msg)
		// Set confirmation state
		m.confirming = true
		m.confirmAsset = &asset
		return m, nil
	case startDownloadProgressMsg:
		// Start download progress updates
		m.downloadAsset = &msg.asset
		m.downloading = true
		// Start a ticker to send progress updates
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			if m.downloadAsset != nil {
				return updateDownloadProgressMsg{asset: *m.downloadAsset}
			}
			return nil
		})
	case updateDownloadProgressMsg:
		// Update download progress
		if m.downloadAsset != nil {
			// Get actual progress from global variable
			downloadProgressMutex.Lock()
			progress := downloadProgress
			downloadProgressMutex.Unlock()

			// Send progress update
			return m, tea.Batch(
				func() tea.Msg {
					return downloadProgressUpdateMsg{
						totalBytes:    progress,
						expectedBytes: m.downloadAsset.Size,
					}
				},
				tea.Tick(time.Second, func(t time.Time) tea.Msg {
					if m.downloadAsset != nil {
						return updateDownloadProgressMsg{asset: *m.downloadAsset}
					}
					return nil
				}),
			)
		}
		return m, nil
	case downloadProgressMsg:
		m.downloading = true
		m.downloadMsg = string(msg)

		// Parse download progress to update progress bar
		// Expected format: "Downloading filename: size / total"
		parts := strings.Split(string(msg), ":")
		if len(parts) == 2 {
			progressParts := strings.Split(parts[1], "/")
			if len(progressParts) == 2 {
				// Remove spaces and parse sizes
				downloadedStr := strings.TrimSpace(progressParts[0])
				totalStr := strings.TrimSpace(progressParts[1])

				// Parse downloaded size
				downloadedBytes := parseSize(downloadedStr)
				totalBytes := parseSize(totalStr)

				// Update download state for progress bar
				m.downloadState = &DownloadState{
					totalBytes:    downloadedBytes,
					expectedBytes: totalBytes,
				}
			}
		}
	case downloadProgressUpdateMsg:
		// Update download state for progress bar
		m.downloadState = &DownloadState{
			totalBytes:    msg.totalBytes,
			expectedBytes: msg.expectedBytes,
		}
	case downloadCompleteMsg:
		m.downloading = false
		m.downloadAsset = nil
		fmt.Printf("File downloaded successfully: %s\n", string(msg))
		return m, tea.Quit
	case downloadErrorMsg:
		m.downloading = false
		m.downloadAsset = nil
		m.errorMsg = string(msg)
	case cancelDownloadMsg:
		m.downloading = false
		m.downloadAsset = nil
		m.errorMsg = "Download cancelled by user"
	}

	return m, nil
}

// View interface display
func (m model) View() string {
	// If we are in confirmation state, display confirmation dialog
	if m.confirming && m.confirmAsset != nil {
		s := fmt.Sprintf("\nSelected artifact:\n")
		s += fmt.Sprintf("  Name: %s\n", m.confirmAsset.Name)
		s += fmt.Sprintf("  Release: %s\n", m.confirmAsset.ReleaseTag)
		s += fmt.Sprintf(" Size: %s\n", m.confirmAsset.SizeStr)
		s += fmt.Sprintf("\nDownload this file to the current folder? (y/N): ")
		return s
	}

	if m.quitting {
		return "Goodbye!\n"
	}

	if m.loading {
		return "Searching for available artifacts...\n"
	}

	if m.errorMsg != "" {
		return fmt.Sprintf("Error: %s\n", m.errorMsg)
	}

	if len(m.assets) == 0 {
		return "No artifacts found\n"
	}

	s := "Select artifact for download:\n\n"

	// Styles
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Bold(true)
	defaultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Display artifacts with height limit
	const maxVisibleItems = 15
	start := 0
	end := len(m.assets)

	if len(m.assets) > maxVisibleItems {
		// Center the selected item
		if m.cursor < maxVisibleItems/2 {
			end = maxVisibleItems
		} else if m.cursor > len(m.assets)-maxVisibleItems/2 {
			start = len(m.assets) - maxVisibleItems
		} else {
			start = m.cursor - maxVisibleItems/2
			end = start + maxVisibleItems
		}
	}

	// Add scroll information if available
	if start > 0 {
		s += fmt.Sprintf("  ... %d more above\n", start)
	}

	for i := start; i < end && i < len(m.assets); i++ {
		asset := m.assets[i]
		if i == m.cursor {
			s += selectedStyle.Render("> "+asset.DisplayLine) + "\n"
		} else {
			s += defaultStyle.Render("  "+asset.DisplayLine) + "\n"
		}
	}

	// Add scroll information if available
	if end < len(m.assets) {
		s += fmt.Sprintf(" ... %d more below\n", len(m.assets)-end)
	}

	// Display additional information about the selected artifact
	if len(m.assets) > 0 && m.cursor < len(m.assets) {
		selectedAsset := m.assets[m.cursor]
		s += "\n" + infoStyle.Render(fmt.Sprintf("Selected: %s", selectedAsset.Name)) + "\n"
		s += infoStyle.Render(fmt.Sprintf("Release: %s", selectedAsset.ReleaseTag)) + "\n"
		s += infoStyle.Render(fmt.Sprintf("Size: %s", selectedAsset.SizeStr)) + "\n"
		s += infoStyle.Render(fmt.Sprintf("Created: %s", selectedAsset.FormattedDate)) + "\n"
	}

	s += "\nPress '↑/↓' or 'j/k' to navigate, 'enter' to select, 'q' or 'ctrl+c' to quit\n"

	if m.downloading {
		// Display download progress
		if m.downloadAsset != nil {
			s += fmt.Sprintf("\nDownloading %s...\n", m.downloadAsset.Name)
		}

		// Display progress information if we have download state
		if m.downloadState != nil {
			s += fmt.Sprintf("Downloaded: %s / %s\n",
				formatSize(m.downloadState.totalBytes),
				formatSize(m.downloadState.expectedBytes))
		} else if m.downloadAsset != nil {
			// Display initial progress information
			s += fmt.Sprintf("Downloaded: %s / %s\n",
				formatSize(0),
				formatSize(m.downloadAsset.Size))
		}
	}

	return s
}

// Custom messages
type releasesMsg []AssetInfo
type errorMsg string
type downloadConfirmMsg AssetInfo
type downloadProgressMsg string
type downloadCompleteMsg string
type downloadErrorMsg string
type cancelDownloadMsg struct{}

// startDownloadProgressMsg message to start download progress updates
type startDownloadProgressMsg struct {
	asset AssetInfo
}

// downloadProgressUpdateMsg message for updating download progress
type downloadProgressUpdateMsg struct {
	totalBytes    int64
	expectedBytes int64
}

// updateDownloadProgressMsg message to update download progress
type updateDownloadProgressMsg struct {
	asset AssetInfo
}

// fetchReleases get list of releases
func fetchReleases() tea.Msg {
	config, err := loadConfig()
	if err != nil {
		return errorMsg(err.Error())
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", config.REPO_OWNER, config.REPO_NAME)

	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return errorMsg(err.Error())
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+config.GITHUB_TOKEN)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return errorMsg(err.Error())
	}
	defer resp.Body.Close()

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

	var assets []AssetInfo
	for _, release := range releases {
		for _, asset := range release.Assets {
			// Use asset mask from config or default to "*.tag.gz"
			assetMask := config.ASSET_MASK
			if assetMask == "" {
				assetMask = "*.tag.gz"
			}

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
				// Format creation date
				formattedDate := formatCreatedAt(asset.CreatedAt)

				// Format file size
				sizeStr := formatSize(asset.Size)

				displayLine := fmt.Sprintf("[%s] %s (%s, %s)", release.TagName, asset.Name, sizeStr, formattedDate)

				assets = append(assets, AssetInfo{
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
					DisplayLine:   displayLine,
				})
			}
		}
	}

	if len(assets) == 0 {
		return errorMsg("artifacts not found")
	}

	return releasesMsg(assets)
}

// formatCreatedAt format creation date
func formatCreatedAt(created_at string) string {
	t, err := time.Parse(time.RFC3339, created_at)
	if err != nil {
		return created_at
	}
	return t.Format("2006-01-02 15:04")
}

// parseSize parse file size from string
func parseSize(sizeStr string) int64 {
	// Remove spaces
	sizeStr = strings.TrimSpace(sizeStr)

	// Handle "Unknown" case
	if sizeStr == "Unknown" {
		return 0
	}

	// Handle different units
	var multiplier int64 = 1
	var numberStr string

	// Check for units
	if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		numberStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		numberStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		numberStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "B") {
		multiplier = 1
		numberStr = strings.TrimSuffix(sizeStr, "B")
	} else {
		// No unit, assume bytes
		numberStr = sizeStr
	}

	// Parse the number
	numberStr = strings.TrimSpace(numberStr)
	var number float64
	fmt.Sscanf(numberStr, "%f", &number)

	return int64(number * float64(multiplier))
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

// confirmDownload confirm download
func confirmDownload(asset AssetInfo) tea.Cmd {
	return func() tea.Msg {
		return downloadConfirmMsg(asset)
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
		req.Header.Set("Authorization", "Bearer "+config.GITHUB_TOKEN)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			// Check if the error is due to context cancellation
			if downloadContext.Err() == context.Canceled {
				return downloadErrorMsg("Download cancelled by user")
			}
			return downloadErrorMsg(fmt.Sprintf("Error downloading file: %v", err))
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			return downloadErrorMsg(fmt.Sprintf("HTTP error: %d", resp.StatusCode))
		}

		// Create output file
		out, err := os.Create(asset.Name)
		if err != nil {
			return downloadErrorMsg(fmt.Sprintf("Error creating file: %v", err))
		}
		defer out.Close()

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
			if downloadContext.Err() == context.Canceled {
				// Clean up partial file
				os.Remove(asset.Name)
				return downloadErrorMsg("Download cancelled by user")
			}
			// Clean up partial file
			os.Remove(asset.Name)
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
		return nil, fmt.Errorf("Configuration file not found in %s or %s", configFile, homeConfigFile)
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
			config.GITHUB_TOKEN = value
		case "REPO_OWNER":
			config.REPO_OWNER = value
		case "REPO_NAME":
			config.REPO_NAME = value
		case "ASSET_MASK":
			config.ASSET_MASK = value
		}
	}

	return config, nil
}

func main() {
	// Create context with cancel function
	downloadContext, downloadCancel = context.WithCancel(context.Background())
	defer downloadCancel()

	// Pass context to model
	m := model{
		assets:  []AssetInfo{},
		loading: true,
	}

	// Run bubbletea
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Check if context was cancelled
	if downloadContext.Err() == context.Canceled {
		fmt.Println("Download cancelled by user")
		os.Exit(0)
	}
}
