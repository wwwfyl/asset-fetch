package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tileWidth is the inner width of one dashboard card (excluding border).
const tileWidth = 24

// renderDashboard returns the multi-app dashboard view, laying out one card
// per app side by side and a key-hint bar at the bottom.
func renderDashboard(m model) string {
	if len(m.tiles) == 0 {
		return "No apps configured.\n"
	}

	boxes := make([]string, len(m.tiles))
	for i, t := range m.tiles {
		boxes[i] = renderTile(t, i == m.selectedTile)
	}

	grid := lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
	bar := dashboardBarStyle.Render(dashboardBarText(m))

	header := dashboardHeaderStyle.Render("afetch")
	return header + "\n\n" + grid + "\n" + bar + "\n"
}

// dashboardBarText returns the bottom hint line; the uninstall confirm prompt
// replaces the default key hints while it is active.
func dashboardBarText(m model) string {
	if m.confirmUninstall && m.selectedTile >= 0 && m.selectedTile < len(m.tiles) {
		return confirmPromptStyle.Render("Uninstall "+m.tiles[m.selectedTile].Name+"? [y/N]")
	}
	return "Tab/←/→ navigate · u update · d uninstall · Enter releases · q quit"
}

// renderTile renders one card. selected toggles the accent border.
func renderTile(t TileInfo, selected bool) string {
	lines := []string{
		tileNameStyle.Render(t.Name),
		"GitHub: " + dashVersion(t.LatestVersion),
		"Local:  " + dashVersion(t.InstalledVersion),
		renderTileStatus(t),
	}
	body := strings.Join(lines, "\n")
	if selected {
		return tileSelectedStyle.Render(body)
	}
	return tileStyle.Render(body)
}

// renderTileStatus formats the bottom status line based on the tile state.
func renderTileStatus(t TileInfo) string {
	switch t.Status {
	case TileStatusLoading:
		return statusLoadingStyle.Render("loading…")
	case TileStatusError:
		msg := t.Err
		if msg == "" {
			msg = "error"
		}
		return statusErrorStyle.Render("✗ " + truncateStatus(msg, tileWidth-2))
	case TileStatusUpdating:
		return statusLoadingStyle.Render("↻ updating…")
	case TileStatusUninstalling:
		return statusLoadingStyle.Render("↻ removing…")
	case TileStatusReady:
		if t.InstalledVersion == "" {
			return statusLoadingStyle.Render("(not installed)")
		}
		if t.LatestVersion != "" && t.InstalledVersion != t.LatestVersion {
			return statusUpdateStyle.Render("● update available")
		}
		return statusUpToDateStyle.Render("✓ up to date")
	}
	return ""
}

// dashVersion replaces an empty version string with a dash placeholder so the
// card stays aligned visually.
func dashVersion(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// truncateStatus trims s to at most max runes, appending an ellipsis when cut.
func truncateStatus(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

var (
	tileStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1).
			Width(tileWidth)

	tileSelectedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(0, 1).
				Width(tileWidth)

	tileNameStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))

	statusLoadingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	statusErrorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	statusUpdateStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	statusUpToDateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))

	dashboardHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))
	dashboardBarStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).PaddingTop(1)
	confirmPromptStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)
