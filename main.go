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

// DownloadState structure for tracking download progress
type DownloadState struct {
	totalBytes    int64
	expectedBytes int64
	error         error
	completed     bool
	mutex         sync.Mutex
}

// DownloadProgress structure for tracking download progress for tabular display
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

// truncateString truncates a string to the specified length and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Model structure for bubbletea
type model struct {
	// Original fields for asset selection mode
	assets                  []AssetInfo
	cursor                  int
	quitting                bool
	loading                 bool
	errorMsg                string
	downloading             bool
	downloadMsg             string
	confirming              bool
	confirmAsset            *AssetInfo
	downloadState           *DownloadState
	downloadAsset           *AssetInfo
	currentDownloadProgress int64
	downloadFinished        bool
	downloadSuccess         bool
	downloadResult          string

	// New fields for release selection mode
	releases      []Release
	releaseCursor int
	showReleases  bool // Flag to indicate we are showing releases

	// New fields for multi-asset selection mode
	selectedAssets   []bool // Slice to track which assets are selected
	selectMode       bool   // Flag to indicate we are in multi-select mode
	selectedAssetMsg string // Message to show which assets are selected

	// New fields for sequential download mode
	downloadQueue        []AssetInfo        // Queue of assets to download
	currentDownloadIndex int                // Index of currently downloading asset
	downloadResults      []string           // Results of downloads
	downloadsProgress    []DownloadProgress // Progress of each download in the queue
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

		// Handle keys when showing releases
		if m.showReleases {
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			case "up", "k":
				if m.releaseCursor > 0 {
					m.releaseCursor--
				}
			case "down", "j":
				if m.releaseCursor < len(m.releases)-1 {
					m.releaseCursor++
				}
			case "enter", " ":
				if len(m.releases) > 0 {
					// Show assets for selected release
					m.showReleases = false
					m.selectMode = true
					m.assets = []AssetInfo{}

					// Populate assets for selected release
					release := m.releases[m.releaseCursor]
					for _, asset := range release.Assets {
						// Format creation date
						formattedDate := formatCreatedAt(asset.CreatedAt)

						// Format file size
						sizeStr := formatSize(asset.Size)

						displayLine := fmt.Sprintf("%s (%s, %s)", asset.Name, sizeStr, formattedDate)

						m.assets = append(m.assets, AssetInfo{
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

					// Initialize selected assets slice
					m.selectedAssets = make([]bool, len(m.assets))
					m.cursor = 0
				}
			}
			return m, nil
		}

		// Handle keys when in multi-select mode
		if m.selectMode {
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.assets)-1 {
					m.cursor++
				}
			case " ":
				// Toggle selection
				if len(m.selectedAssets) > m.cursor {
					m.selectedAssets[m.cursor] = !m.selectedAssets[m.cursor]
				}
			case "enter":
				// Start downloading selected assets
				m.selectMode = false
				m.downloadQueue = []AssetInfo{}

				// Build download queue
				for i, selected := range m.selectedAssets {
					if selected && i < len(m.assets) {
						m.downloadQueue = append(m.downloadQueue, m.assets[i])
					}
				}

				// If no assets selected, select the current one
				if len(m.downloadQueue) == 0 && len(m.assets) > 0 && m.cursor < len(m.assets) {
					m.downloadQueue = append(m.downloadQueue, m.assets[m.cursor])
				}

				m.downloadResults = []string{}
				m.currentDownloadIndex = 0
				m.downloadsProgress = make([]DownloadProgress, len(m.downloadQueue))

				// Start downloading if we have assets in queue
				if len(m.downloadQueue) > 0 {
					asset := m.downloadQueue[0]
					return m, tea.Batch(
						func() tea.Msg {
							return startDownloadProgressMsg{asset: asset}
						},
						downloadAsset(asset),
					)
				}
				return m, nil
			}
			return m, nil
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
		// Check if we have releases or assets
		if len(msg.releases) > 0 && len(msg.assets) == 0 {
			// Show releases list
			m.releases = msg.releases
			m.showReleases = true
			m.releaseCursor = 0
		} else {
			// Show assets list (original behavior)
			m.assets = msg.assets
			m.loading = false
			sort.Slice(m.assets, func(i, j int) bool {
				return m.assets[i].CreatedAt > m.assets[j].CreatedAt
			})
		}
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

		// Update download progress for tabular display
		if m.currentDownloadIndex >= 0 && m.currentDownloadIndex < len(m.downloadsProgress) {
			m.downloadsProgress[m.currentDownloadIndex] = DownloadProgress{
				downloadedBytes: msg.totalBytes,
				totalBytes:      msg.expectedBytes,
				completed:       msg.totalBytes >= msg.expectedBytes,
			}
		}
	case downloadCompleteMsg:
		m.downloading = false
		m.downloadAsset = nil

		// Get actual file size from filesystem for completed download
		var actualSize int64
		if fileInfo, err := os.Stat(string(msg)); err == nil {
			actualSize = fileInfo.Size()
		}

		// Mark current download as completed with actual file size
		if m.currentDownloadIndex >= 0 && m.currentDownloadIndex < len(m.downloadsProgress) {
			asset := m.downloadQueue[m.currentDownloadIndex]
			// Use actual file size if available, otherwise fall back to asset size or downloaded bytes
			finalSize := actualSize
			if finalSize == 0 {
				finalSize = asset.Size
			}
			if finalSize == 0 {
				finalSize = m.downloadsProgress[m.currentDownloadIndex].downloadedBytes
			}

			m.downloadsProgress[m.currentDownloadIndex] = DownloadProgress{
				downloadedBytes: finalSize,
				totalBytes:      finalSize,
				completed:       true,
			}
		}

		// Move to next download in queue
		m.currentDownloadIndex++
		if m.currentDownloadIndex < len(m.downloadQueue) {
			// Start next download
			asset := m.downloadQueue[m.currentDownloadIndex]
			return m, tea.Batch(
				func() tea.Msg {
					return startDownloadProgressMsg{asset: asset}
				},
				downloadAsset(asset),
			)
		} else {
			// All downloads completed
			m.downloadFinished = true
			m.downloadSuccess = true
			m.downloadResult = "All files downloaded successfully"
			// Exit after showing results
			return m, tea.Quit
		}
	case downloadErrorMsg:
		m.downloading = false
		m.downloadAsset = nil

		// Move to next download in queue
		m.currentDownloadIndex++
		if m.currentDownloadIndex < len(m.downloadQueue) {
			// Start next download
			asset := m.downloadQueue[m.currentDownloadIndex]
			return m, tea.Batch(
				func() tea.Msg {
					return startDownloadProgressMsg{asset: asset}
				},
				downloadAsset(asset),
			)
		} else {
			// All downloads completed (with errors)
			m.downloadFinished = true
			m.downloadSuccess = false
			m.downloadResult = "Downloads completed with errors"
			// Exit after showing results
			return m, tea.Quit
		}
	case cancelDownloadMsg:
		m.downloading = false
		m.downloadAsset = nil
		m.errorMsg = "Download cancelled by user"
	}

	return m, nil
}

// View interface display
func (m model) View() string {
	// If we are showing releases
	if m.showReleases {
		s := "Select release:\n\n"

		// Styles
		selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Bold(true)
		defaultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

		// Display releases
		for i, release := range m.releases {
			line := fmt.Sprintf("[%s] %s", release.TagName, release.Name)
			if i == m.releaseCursor {
				s += selectedStyle.Render("> "+line) + "\n"
			} else {
				s += defaultStyle.Render("  "+line) + "\n"
			}
		}

		s += "\nPress '↑/↓' or 'j/k' to navigate, 'enter' to select, 'q' or 'ctrl+c' to quit\n"
		return s
	}

	// If we are in multi-select mode
	if m.selectMode {
		s := "Select assets to download (press space to select, enter to download):\n\n"

		// Styles
		selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Bold(true)
		defaultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
		selectedAssetStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("46")) // Green
		infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		// Display assets with selection markers
		for i, asset := range m.assets {
			selectionMarker := " [ ] "
			if i < len(m.selectedAssets) && m.selectedAssets[i] {
				selectionMarker = selectedAssetStyle.Render(" [x] ")
			}

			if i == m.cursor {
				s += selectedStyle.Render("> "+selectionMarker+asset.DisplayLine) + "\n"
			} else {
				s += defaultStyle.Render("  "+selectionMarker+asset.DisplayLine) + "\n"
			}
		}

		// Display information about selected assets
		selectedCount := 0
		for _, selected := range m.selectedAssets {
			if selected {
				selectedCount++
			}
		}

		if selectedCount > 0 {
			s += "\n" + infoStyle.Render(fmt.Sprintf("%d asset(s) selected", selectedCount)) + "\n"
		}

		s += "\nPress '↑/↓' or 'j/k' to navigate, 'space' to select/deselect, 'enter' to download, 'q' or 'ctrl+c' to quit\n"
		return s
	}

	// If download is in progress, display download results so far
	if m.downloading && len(m.downloadResults) > 0 {
		s := "Download progress:\n\n"

		// Display previous download results
		for _, result := range m.downloadResults {
			s += result + "\n"
		}

		// Display tabular progress for all files in the queue
		headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
		s += headerStyle.Render("Filename                                 Status          Tag                            Progress") + "\n"
		for i, asset := range m.downloadQueue {
			status := "[ ]"
			progressInfo := ""

			if i < len(m.downloadsProgress) {
				progress := m.downloadsProgress[i]
				if progress.completed {
					status = "[✓]"
					// For completed downloads, show actual downloaded size
					if progress.totalBytes > 0 {
						progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.totalBytes), formatSize(progress.totalBytes))
					} else if asset.Size > 0 {
						progressInfo = fmt.Sprintf("%s / %s", formatSize(asset.Size), formatSize(asset.Size))
					} else {
						// File downloaded but size was unknown, try to get actual size from filesystem
						progressInfo = formatSize(progress.downloadedBytes) + " / " + formatSize(progress.downloadedBytes)
					}
				} else if progress.downloadedBytes > 0 || progress.totalBytes > 0 {
					status = "[-]"
					// Show progress with the best available size information
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
			} else {
				// Not yet started
				status = "[ ]"
				if asset.Size > 0 {
					progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
				} else {
					progressInfo = "0B / Unknown"
				}
			}

			// Format the line with proper spacing
			s += fmt.Sprintf("%-40s %-15s %-30s %s\n",
				truncateString(asset.Name, 40),
				status,
				truncateString(asset.ReleaseTag, 30),
				progressInfo)
		}

		return s
	}

	// If downloads are in progress but no results yet, show current download
	if m.downloading && m.downloadAsset != nil {
		s := "Download progress:\n\n"

		// Display tabular progress for all files in the queue
		headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
		s += headerStyle.Render("Filename                                 Status          Tag                            Progress") + "\n"
		for i, asset := range m.downloadQueue {
			status := "[ ]"
			progressInfo := ""

			if i < len(m.downloadsProgress) {
				progress := m.downloadsProgress[i]
				if progress.completed {
					status = "[✓]"
					progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.totalBytes), formatSize(progress.totalBytes))
				} else if progress.totalBytes > 0 {
					status = "[-]"
					progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.downloadedBytes), formatSize(progress.totalBytes))
				} else {
					status = "[ ]"
					progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
				}
			} else {
				// Not yet started
				status = "[ ]"
				progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
			}

			// Format the line with proper spacing
			s += fmt.Sprintf("%-40s %-15s %-30s %s\n",
				truncateString(asset.Name, 40),
				status,
				truncateString(asset.ReleaseTag, 30),
				progressInfo)
		}

		return s
	}

	// If downloads are finished, display all results
	if m.downloadFinished {
		s := "Download results:\n\n"

		// Display tabular progress for all files in the queue
		headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
		s += headerStyle.Render("Filename                                 Status          Tag                            Progress") + "\n"
		for i, asset := range m.downloadQueue {
			status := "[ ]"
			progressInfo := ""

			if i < len(m.downloadsProgress) {
				progress := m.downloadsProgress[i]
				if progress.completed {
					status = "[✓]"
					progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.totalBytes), formatSize(progress.totalBytes))
				} else if progress.totalBytes > 0 {
					status = "[-]"
					progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.downloadedBytes), formatSize(progress.totalBytes))
				} else {
					status = "[ ]"
					progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
				}
			} else {
				// Not yet started
				status = "[ ]"
				progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
			}

			// Format the line with proper spacing
			s += fmt.Sprintf("%-40s %-15s %-30s %s\n",
				truncateString(asset.Name, 40),
				status,
				truncateString(asset.ReleaseTag, 30),
				progressInfo)
		}

		s += "\n" + m.downloadResult + "\n"
		return s
	}

	// If we are in confirmation state, display confirmation dialog
	if m.confirming && m.confirmAsset != nil {
		s := fmt.Sprintf("\nSelected artifact:\n")
		s += fmt.Sprintf(" Name: %s\n", m.confirmAsset.Name)
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
		// Display tabular progress for all files in the queue
		headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
		s += headerStyle.Render("Filename                                 Status          Tag                            Progress") + "\n"
		for i, asset := range m.downloadQueue {
			status := "[ ]"
			progressInfo := ""

			if i < len(m.downloadsProgress) {
				progress := m.downloadsProgress[i]
				if progress.completed {
					status = "[✓]"
					// For completed downloads, show actual downloaded size
					if progress.totalBytes > 0 {
						progressInfo = fmt.Sprintf("%s / %s", formatSize(progress.totalBytes), formatSize(progress.totalBytes))
					} else if asset.Size > 0 {
						progressInfo = fmt.Sprintf("%s / %s", formatSize(asset.Size), formatSize(asset.Size))
					} else {
						// File downloaded but size was unknown, try to get actual size from filesystem
						progressInfo = formatSize(progress.downloadedBytes) + " / " + formatSize(progress.downloadedBytes)
					}
				} else if progress.downloadedBytes > 0 || progress.totalBytes > 0 {
					status = "[-]"
					// Show progress with the best available size information
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
			} else {
				// Not yet started
				status = "[ ]"
				if asset.Size > 0 {
					progressInfo = fmt.Sprintf("%s / %s", formatSize(0), formatSize(asset.Size))
				} else {
					progressInfo = "0B / Unknown"
				}
			}

			// Format the line with proper spacing
			s += fmt.Sprintf("%-40s %-15s %-30s %s\n",
				truncateString(asset.Name, 40),
				status,
				truncateString(asset.ReleaseTag, 30),
				progressInfo)
		}
	}

	return s
}

// Custom messages
type errorMsg string

type releasesData struct {
	assets   []AssetInfo
	releases []Release
}

type releasesMsg releasesData
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

	var assets []AssetInfo
	for _, release := range releases {
		for _, asset := range release.Assets {
			// Use asset mask from config or default to "*.tag.gz"
			assetMask := config.AssetMask
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

	return releasesMsg{assets: assets, releases: releases}
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
	if _, err := fmt.Sscanf(numberStr, "%f", &number); err != nil {
		// If parsing fails, return 0
		return 0
	}

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
		// Only add authorization header if token is provided
		if config.GitHubToken != "" {
			req.Header.Set("Authorization", "Bearer "+config.GitHubToken)
		}
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

				// Send progress update message
				go func() {
					// Simulate sending a message to update UI
				}()
			},
		}

		// Copy response body to file
		_, err = io.Copy(out, progressReader)
		if err != nil {
			// Check if the error is due to context cancellation
			if downloadContext.Err() == context.Canceled {
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
