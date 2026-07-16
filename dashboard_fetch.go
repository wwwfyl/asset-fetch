package main

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// makeTiles builds the initial tile slice for the given apps, all marked as
// loading. The caller (main.go) assigns the result to m.tiles before the
// program starts so the loading state is rendered on the first frame.
func makeTiles(apps []AppConfig) []TileInfo {
	tiles := make([]TileInfo, len(apps))
	for i, app := range apps {
		tiles[i] = TileInfo{Name: app.Name, Status: TileStatusLoading}
	}
	return tiles
}

// initDashboardTiles returns a Batch that concurrently fetches each app's
// latest GitHub release and currently-installed version, emitting one
// tileUpdatedMsg per tile.
func initDashboardTiles(m model) tea.Cmd {
	if len(m.apps) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, len(m.apps))
	for i, app := range m.apps {
		cmds[i] = fetchTileData(m.downloadCtx, i, app, m.globalToken)
	}
	return tea.Batch(cmds...)
}

// fetchTileData performs one tile's GitHub fetch and installed-version probe.
// Returns a tileUpdatedMsg describing the outcome (data or error).
func fetchTileData(ctx context.Context, idx int, app AppConfig, globalToken string) tea.Cmd {
	return func() tea.Msg {
		msg := tileUpdatedMsg{index: idx}
		msg.installedVersion = getInstalledVersion(app.Version)

		owner, name, ok := splitRepo(app.Repo)
		if !ok {
			msg.err = "invalid repo: " + app.Repo
			return msg
		}
		releases, err := fetchReleasesFromGitHub(ctx, owner, name, "", tokenForApp(app, globalToken))
		if err != nil {
			msg.err = err.Error()
			return msg
		}
		release, err := pickRelease(releases, app.ReleaseType)
		if err != nil {
			msg.err = err.Error()
			return msg
		}
		msg.latestVersion = normalizeVersion(release.TagName)
		return msg
	}
}
