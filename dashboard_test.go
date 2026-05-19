package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRenderDashboardEmpty(t *testing.T) {
	got := renderDashboard(model{})
	if !strings.Contains(got, "No apps configured") {
		t.Errorf("empty dashboard should mention no apps; got: %q", got)
	}
}

func TestRenderDashboardTiles(t *testing.T) {
	m := model{
		selectedTile: 1,
		tiles: []TileInfo{
			{Name: "lazygit", LatestVersion: "v0.44.1", InstalledVersion: "v0.43.0", Status: TileStatusReady},
			{Name: "delta", LatestVersion: "0.18.2", InstalledVersion: "0.18.2", Status: TileStatusReady},
			{Name: "fd", LatestVersion: "v10.2.0", Status: TileStatusReady},
			{Name: "broken", Status: TileStatusError, Err: "fetch failed"},
			{Name: "loading", Status: TileStatusLoading},
		},
	}
	got := renderDashboard(m)

	for _, want := range []string{
		"lazygit", "v0.44.1", "v0.43.0", "update available",
		"delta", "up to date",
		"fd", "not installed",
		"broken", "fetch failed",
		"loading…",
		"Tab/←/→", "q quit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dashboard missing %q in:\n%s", want, got)
		}
	}
}

func TestHandleDashboardInputNavigation(t *testing.T) {
	base := func() model {
		return model{
			state: StateDashboard,
			tiles: []TileInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			apps:  []AppConfig{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		}
	}
	cases := []struct {
		name string
		key  string
		from int
		want int
	}{
		{"tab next", "tab", 0, 1},
		{"tab wraps", "tab", 2, 0},
		{"right", "right", 0, 1},
		{"l vim", "l", 1, 2},
		{"shift+tab prev", "shift+tab", 1, 0},
		{"shift+tab wraps", "shift+tab", 0, 2},
		{"left", "left", 2, 1},
		{"h vim", "h", 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base()
			m.selectedTile = c.from
			out, _ := m.handleDashboardInput(c.key)
			got := out.(model).selectedTile
			if got != c.want {
				t.Errorf("from %d via %q: got %d, want %d", c.from, c.key, got, c.want)
			}
		})
	}
}

func TestHandleDashboardInputConfirmFlow(t *testing.T) {
	m := model{
		state: StateDashboard,
		tiles: []TileInfo{{Name: "a"}, {Name: "b"}},
		apps:  []AppConfig{{Name: "a"}, {Name: "b"}},
	}

	// d toggles the confirm prompt.
	out, _ := m.handleDashboardInput("d")
	m = out.(model)
	if !m.confirmUninstall {
		t.Fatal("d should activate confirmUninstall")
	}

	// n clears it.
	out, _ = m.handleDashboardInput("n")
	m = out.(model)
	if m.confirmUninstall {
		t.Fatal("n should clear confirmUninstall")
	}

	// d again, then y also clears it (placeholder for commit 17).
	out, _ = m.handleDashboardInput("d")
	m = out.(model)
	out, _ = m.handleDashboardInput("y")
	m = out.(model)
	if m.confirmUninstall {
		t.Fatal("y should clear confirmUninstall")
	}

	// During confirm, navigation keys should not move the selection.
	out, _ = m.handleDashboardInput("d")
	m = out.(model)
	m.selectedTile = 0
	out, _ = m.handleDashboardInput("tab")
	if got := out.(model).selectedTile; got != 0 {
		t.Errorf("tab during confirm changed selection to %d", got)
	}
}

func TestOpenAppReleasesPopulatesModel(t *testing.T) {
	m := model{
		apps: []AppConfig{
			{
				Name:        "lazygit",
				Repo:        "jesseduffield/lazygit",
				AssetMask:   "*linux_x86_64.tar.gz",
				GitHubToken: "per_app_token",
			},
		},
		gitHubToken: "global_token",
	}
	out, cmd := m.openAppReleases(0)
	r := out.(model)
	if r.repoOwner != "jesseduffield" || r.repoName != "lazygit" {
		t.Errorf("repo split: got %q/%q", r.repoOwner, r.repoName)
	}
	if r.assetMask == nil || *r.assetMask != "*linux_x86_64.tar.gz" {
		t.Errorf("assetMask not set from app")
	}
	if r.gitHubToken != "per_app_token" {
		t.Errorf("per-app token not applied: got %q", r.gitHubToken)
	}
	if !r.fromDashboard {
		t.Error("fromDashboard should be true")
	}
	if cmd == nil {
		t.Error("expected fetchReleases cmd")
	}
}

func TestOpenAppReleasesRejectsInvalidRepo(t *testing.T) {
	m := model{apps: []AppConfig{{Name: "x", Repo: "no-slash"}}}
	out, _ := m.openAppReleases(0)
	if out.(model).errorMsg == "" {
		t.Error("expected errorMsg for malformed repo")
	}
}

func TestRenderDashboardConfirmOverlay(t *testing.T) {
	m := model{
		tiles:            []TileInfo{{Name: "lazygit"}},
		apps:             []AppConfig{{Name: "lazygit"}},
		selectedTile:     0,
		confirmUninstall: true,
	}
	got := renderDashboard(m)
	if !strings.Contains(got, "Uninstall lazygit?") {
		t.Errorf("expected confirm prompt, got:\n%s", got)
	}
	if strings.Contains(got, "Tab/←/→") {
		t.Error("confirm prompt should replace the key-hint bar")
	}
}

// countTileRows counts grid rows by looking for the rounded top-left corner.
// A single grid row produces one line containing one or more "╭" characters;
// vertically stacked rows each contribute a separate "╭"-bearing line.
func countTileRows(rendered string) int {
	rows := 0
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "╭") {
			rows++
		}
	}
	return rows
}

func TestRenderDashboardWrapsToWidth(t *testing.T) {
	cases := []struct {
		name     string
		width    int
		numTiles int
		wantRows int
	}{
		{"zero width falls back to single row", 0, 3, 1},
		{"wide terminal keeps single row", 200, 4, 1},
		{"80 cols fits 3 of 4 tiles per row", 80, 4, 2},
		{"50 cols fits 1 per row", 50, 4, 4},
		{"narrower than one tile still renders one per row", 25, 2, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tiles := make([]TileInfo, c.numTiles)
			for i := range tiles {
				tiles[i] = TileInfo{Name: "x", Status: TileStatusReady}
			}
			m := model{tiles: tiles, width: c.width}
			got := countTileRows(renderDashboard(m))
			if got != c.wantRows {
				t.Errorf("width=%d tiles=%d: rows=%d, want %d", c.width, c.numTiles, got, c.wantRows)
			}
		})
	}
}

func TestUpdateCapturesWindowSize(t *testing.T) {
	m := model{}
	out, _ := m.Update(tea.WindowSizeMsg{Width: 123, Height: 40})
	if got := out.(model).width; got != 123 {
		t.Errorf("width = %d, want 123", got)
	}
}

func TestRenderTileStatus(t *testing.T) {
	cases := []struct {
		name string
		tile TileInfo
		want string
	}{
		{"loading", TileInfo{Status: TileStatusLoading}, "loading"},
		{"updating", TileInfo{Status: TileStatusUpdating}, "updating"},
		{"uninstalling", TileInfo{Status: TileStatusUninstalling}, "removing"},
		{"error empty", TileInfo{Status: TileStatusError}, "error"},
		{"error msg", TileInfo{Status: TileStatusError, Err: "boom"}, "boom"},
		{"ready not installed", TileInfo{Status: TileStatusReady, LatestVersion: "v1"}, "not installed"},
		{"ready up to date", TileInfo{Status: TileStatusReady, LatestVersion: "v1", InstalledVersion: "v1"}, "up to date"},
		{"ready update avail", TileInfo{Status: TileStatusReady, LatestVersion: "v2", InstalledVersion: "v1"}, "update available"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderTileStatus(c.tile)
			if !strings.Contains(got, c.want) {
				t.Errorf("got %q, want substring %q", got, c.want)
			}
		})
	}
}
