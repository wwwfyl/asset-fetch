package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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

	// URL-based execution
	repoOwner         string
	repoName          string
	tag               string
	assetMask         *string
	startWithReleases bool
}

// Init bubbletea initialization
func (m model) Init() tea.Cmd {
	return fetchReleases(m)
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
		// If a specific tag was requested, go directly to assets
		if m.tag != "" {
			m.listView.SetAssets(msg.assets)
			m.state = StateAssets
			m.loading = false
		} else if len(msg.assets) > 0 && !m.startWithReleases {
			// Show filtered assets directly if ASSET_MASK was used and not overridden by URL
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
		return m, tea.Tick(time.Second, func(tick time.Time) tea.Msg {
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

		return m, tea.Tick(time.Second, func(tick time.Time) tea.Msg {
			if m.downloading {
				return updateDownloadProgressMsg{asset: msg.asset}
			}
			return nil
		})

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

	case checksumVerifiedMsg:
		m.downloading = false

		// Get actual file size from filesystem for completed download
		var actualSize int64
		if fileInfo, err := os.Stat(msg.filename); err == nil {
			actualSize = fileInfo.Size()
		}

		// Mark current download as completed with actual file size
		m.downloadQueue.CompleteCurrentDownload(actualSize)

		// Handle checksum verification result
		if msg.success {
			// Check if there are more downloads in the queue
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
				m.downloadResult = "All files downloaded and verified successfully"
				m.state = StateFinished
				// Exit after showing results
				return m, tea.Quit
			}
		} else {
			// Checksum verification failed
			m.downloadFinished = true
			m.downloadSuccess = false
			m.downloadResult = fmt.Sprintf("Checksum verification failed for %s: %s", msg.filename, msg.err)
			m.state = StateFinished
			// Exit after showing results
			return m, tea.Quit
		}
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
	case StateChecksumVerification:
		s := "Verifying checksums:\n\n"
		s += m.progressFormatter.RenderProgressTable(m.downloadQueue.assets, m.downloadQueue.progress)
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
