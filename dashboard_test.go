package main

import (
	"strings"
	"testing"
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
