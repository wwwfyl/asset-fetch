package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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

// truncateString truncates a string to the specified length and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
