package main

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	releases         []Release
	fromReleasesView bool // true when user navigated from releases list

	// URL-based execution
	repoOwner         string
	repoName          string
	tag               string
	assetMask         *string
	startWithReleases bool

	// Download lifecycle (per-model, not global)
	downloadCtx    context.Context
	downloadCancel context.CancelFunc

	// Injected from config at startup
	gitHubToken string // effective token for the current single-app session
	globalToken string // top-level github_token from the YAML config, used as the per-app fallback
	downloadDir string // directory where assets are saved; defaults to cwd

	// All apps from the YAML config; consumed by the dashboard.
	apps []AppConfig

	// Dashboard state: one tile per app, plus the focused index.
	tiles        []TileInfo
	selectedTile int

	// fromDashboard is set when releases/assets were opened from a dashboard
	// tile, so `q` returns to the dashboard instead of quitting.
	fromDashboard bool

	// confirmUninstall toggles the "Uninstall <name>? [y/N]" overlay.
	confirmUninstall bool

	// Per-download progress shared between the download goroutine and the tick loop.
	currentProgress *ProgressState
}

// Init bubbletea initialization
func (m model) Init() tea.Cmd {
	if m.errorMsg != "" {
		return nil
	}
	if m.state == StateDashboard {
		return initDashboardTiles(m)
	}
	return fetchReleases(m)
}

// Update bubbletea message processing - unified version
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.downloading {
				if m.downloadCancel != nil {
					m.downloadCancel()
				}
				return m, func() tea.Msg {
					return cancelDownloadMsg{}
				}
			} else if m.state == StateAssets && m.fromReleasesView {
				// Go back to releases list
				m.listView.SetReleases(m.releases)
				m.state = StateReleases
				m.fromReleasesView = false
				return m, nil
			} else if m.fromDashboard && (m.state == StateReleases || m.state == StateAssets) {
				// Go back to the dashboard.
				m.state = StateDashboard
				m.fromDashboard = false
				m.fromReleasesView = false
				return m, nil
			} else if m.state == StateDashboard && m.confirmUninstall {
				m.confirmUninstall = false
				return m, nil
			} else {
				m.quitting = true
				return m, tea.Quit
			}
		}

		// Handle state-specific navigation and actions
		switch m.state {
		case StateDashboard:
			return m.handleDashboardInput(msg.String())
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

	case tileUpdatedMsg:
		if msg.index >= 0 && msg.index < len(m.tiles) {
			t := &m.tiles[msg.index]
			if msg.err != "" {
				t.Status = TileStatusError
				t.Err = msg.err
			} else {
				t.LatestVersion = msg.latestVersion
				t.InstalledVersion = msg.installedVersion
				t.Status = TileStatusReady
			}
		}

	case updateCompleteMsg:
		if msg.index >= 0 && msg.index < len(m.tiles) {
			t := &m.tiles[msg.index]
			if msg.err != "" {
				t.Status = TileStatusError
				t.Err = msg.err
			} else {
				t.Status = TileStatusReady
				t.InstalledVersion = msg.newInstalled
				t.Err = ""
			}
		}

	case uninstallCompleteMsg:
		if msg.index >= 0 && msg.index < len(m.tiles) {
			t := &m.tiles[msg.index]
			if msg.err != "" {
				t.Status = TileStatusError
				t.Err = msg.err
			} else {
				t.Status = TileStatusReady
				t.InstalledVersion = msg.newInstalled
				t.Err = ""
			}
		}

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
		downloaded, _ := m.currentProgress.Get()
		m.downloadQueue.UpdateProgress(downloaded, msg.asset.Size)

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
			asset := m.downloadQueue.GetCurrent()
			m.currentProgress = &ProgressState{}
			return m, tea.Batch(
				func() tea.Msg {
					return startDownloadProgressMsg{asset: *asset}
				},
				downloadAsset(m.downloadCtx, *asset, m.gitHubToken, m.downloadDir, m.currentProgress),
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
			if m.downloadQueue.NextDownload() {
				asset := m.downloadQueue.GetCurrent()
				m.currentProgress = &ProgressState{}
				return m, tea.Batch(
					func() tea.Msg {
						return startDownloadProgressMsg{asset: *asset}
					},
					downloadAsset(m.downloadCtx, *asset, m.gitHubToken, m.downloadDir, m.currentProgress),
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
	maxItems := len(m.listView.filteredItems)

	if m.listView.searchActive {
		switch key {
		case "esc":
			m.listView.searchActive = false
			m.listView.SetFilter("")
		case "enter":
			m.listView.searchActive = false
			if selectedRelease := m.listView.GetCurrentRelease(); selectedRelease != nil {
				m.selectRelease(selectedRelease)
			}
		case "up":
			if m.listView.cursor > 0 {
				m.listView.cursor--
			}
		case "down":
			if m.listView.cursor < maxItems-1 {
				m.listView.cursor++
			}
		case "backspace":
			m.listView.BackspaceFilter()
		default:
			if len(key) == 1 {
				m.listView.AddToFilter(key)
			}
		}
		return m, nil
	}

	// Nav mode
	switch key {
	case "/":
		m.listView.ActivateSearch()
	case "up", "k":
		if m.listView.cursor > 0 {
			m.listView.cursor--
		}
	case "down", "j":
		if m.listView.cursor < maxItems-1 {
			m.listView.cursor++
		}
	case "esc":
		if m.listView.filter != "" {
			m.listView.SetFilter("")
		}
	case "enter", " ":
		if selectedRelease := m.listView.GetCurrentRelease(); selectedRelease != nil {
			m.selectRelease(selectedRelease)
		}
	}

	return m, nil
}

func (m *model) selectRelease(selectedRelease *Release) {
	var assets []AssetInfo
	for _, asset := range selectedRelease.Assets {
		assetInfo := m.assetFormatter.FormatAssetInfo(asset, *selectedRelease)
		assetInfo.DisplayLine = m.assetFormatter.createDisplayLineWithoutTag(asset.Name, assetInfo.SizeStr, assetInfo.FormattedDate)
		assets = append(assets, assetInfo)
	}
	m.listView.SetAssets(assets)
	m.state = StateAssets
	m.fromReleasesView = true
}

// Handle input when in assets state
func (m model) handleAssetsInput(key string) (tea.Model, tea.Cmd) {
	maxItems := len(m.listView.filteredItems)

	if m.listView.searchActive {
		switch key {
		case "esc":
			m.listView.searchActive = false
			m.listView.SetFilter("")
		case "enter":
			m.listView.searchActive = false
			return m.startDownload()
		case "up":
			if m.listView.cursor > 0 {
				m.listView.cursor--
			}
		case "down":
			if m.listView.cursor < maxItems-1 {
				m.listView.cursor++
			}
		case "backspace":
			m.listView.BackspaceFilter()
		default:
			if len(key) == 1 {
				m.listView.AddToFilter(key)
			}
		}
		return m, nil
	}

	// Nav mode
	switch key {
	case "/":
		m.listView.ActivateSearch()
	case "up", "k":
		if m.listView.cursor > 0 {
			m.listView.cursor--
		}
	case "down", "j":
		if m.listView.cursor < maxItems-1 {
			m.listView.cursor++
		}
	case "esc":
		if m.listView.filter != "" {
			m.listView.SetFilter("")
		}
	case " ":
		m.listView.ToggleSelection()
	case "enter":
		return m.startDownload()
	}

	return m, nil
}

func (m model) startDownload() (tea.Model, tea.Cmd) {
	selectedAssets := m.listView.GetSelectedAssets()
	if len(selectedAssets) == 0 {
		if currentAsset := m.listView.GetCurrentAsset(); currentAsset != nil {
			selectedAssets = []AssetInfo{*currentAsset}
		}
	}
	if len(selectedAssets) > 0 {
		m.downloadQueue.Reset()
		m.downloadQueue.AddMultiple(selectedAssets)
		if !m.downloadQueue.IsEmpty() {
			asset := m.downloadQueue.GetCurrent()
			m.currentProgress = &ProgressState{}
			return m, tea.Batch(
				func() tea.Msg {
					return startDownloadProgressMsg{asset: *asset}
				},
				downloadAsset(m.downloadCtx, *asset, m.gitHubToken, m.downloadDir, m.currentProgress),
			)
		}
	}
	return m, nil
}

// handleDashboardInput handles key input when the dashboard is the active state.
// Update/uninstall actions defer real work to later commits; this only wires
// the navigation, the confirm overlay and the transition into releases.
func (m model) handleDashboardInput(key string) (tea.Model, tea.Cmd) {
	if m.confirmUninstall {
		switch key {
		case "y", "Y":
			m.confirmUninstall = false
			if m.selectedTile >= 0 && m.selectedTile < len(m.tiles) && m.selectedTile < len(m.apps) {
				t := &m.tiles[m.selectedTile]
				if t.Status == TileStatusUpdating || t.Status == TileStatusUninstalling {
					return m, nil
				}
				t.Status = TileStatusUninstalling
				t.Err = ""
				return m, startAppUninstall(m.selectedTile, m.apps[m.selectedTile])
			}
			return m, nil
		case "n", "N", "esc":
			m.confirmUninstall = false
		}
		return m, nil
	}

	n := len(m.tiles)
	if n == 0 {
		return m, nil
	}

	switch key {
	case "tab", "right", "l":
		m.selectedTile = (m.selectedTile + 1) % n
	case "shift+tab", "left", "h":
		m.selectedTile = (m.selectedTile - 1 + n) % n
	case "enter":
		return m.openAppReleases(m.selectedTile)
	case "u":
		if m.selectedTile >= 0 && m.selectedTile < len(m.tiles) && m.selectedTile < len(m.apps) {
			t := &m.tiles[m.selectedTile]
			if t.Status == TileStatusUpdating || t.Status == TileStatusUninstalling {
				return m, nil
			}
			t.Status = TileStatusUpdating
			t.Err = ""
			return m, startAppUpdate(m.downloadCtx, m.selectedTile, m.apps[m.selectedTile], m.globalToken)
		}
	case "d":
		if m.selectedTile >= 0 && m.selectedTile < len(m.apps) {
			m.confirmUninstall = true
		}
	}
	return m, nil
}

// openAppReleases switches the model to the releases/assets list for the app
// at idx, reusing the existing fetchReleases pipeline. fromDashboard is set so
// `q` later returns to the dashboard.
func (m model) openAppReleases(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.apps) {
		return m, nil
	}
	app := m.apps[idx]
	parts := strings.SplitN(app.Repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		m.errorMsg = "invalid repo: " + app.Repo
		return m, nil
	}
	m.repoOwner = parts[0]
	m.repoName = parts[1]
	m.tag = ""
	m.startWithReleases = false
	if app.AssetMask != "" {
		am := app.AssetMask
		m.assetMask = &am
	} else {
		m.assetMask = nil
	}
	m.gitHubToken = tokenForApp(app, m.globalToken)
	m.fromDashboard = true
	m.loading = true
	return m, fetchReleases(m)
}

// View interface display - unified version
func (m model) View() string {
	// Overlay states take precedence over the in-progress view so that
	// errors and loading spinners are visible before lists are populated.
	switch {
	case m.quitting:
		return "Goodbye!\n"
	case m.errorMsg != "":
		return fmt.Sprintf("Error: %s\n", m.errorMsg)
	case m.loading:
		return "Searching for available artifacts...\n"
	}

	switch m.state {
	case StateDashboard:
		return renderDashboard(m)
	case StateReleases, StateAssets:
		return m.listView.Render()
	case StateDownloading:
		return "Download progress:\n\n" +
			m.progressFormatter.RenderProgressTable(m.downloadQueue.assets, m.downloadQueue.progress)
	case StateFinished:
		return "Download results:\n\n" +
			m.progressFormatter.RenderProgressTable(m.downloadQueue.assets, m.downloadQueue.progress) +
			"\n" + m.downloadResult + "\n"
	}

	return "No artifacts found\n"
}
