package main

import (
	"context"
	"strings"
	"testing"
)

func TestSelectRelease(t *testing.T) {
	all := []Release{
		{TagName: "v1.0.0-rc1", Prerelease: true},
		{TagName: "v0.9.0", Prerelease: false},
		{TagName: "v0.9.0-rc1", Prerelease: true},
		{TagName: "v0.8.0", Prerelease: false},
	}
	cases := []struct {
		releaseType string
		want        string
	}{
		{"", "1.0.0-rc1"},
		{"latest", "1.0.0-rc1"},
		{"latest-stable", "0.9.0"},
		{"pre-release", "1.0.0-rc1"},
	}
	for _, c := range cases {
		t.Run(c.releaseType, func(t *testing.T) {
			if got := selectRelease(all, c.releaseType); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSelectReleaseEmptyInputs(t *testing.T) {
	if got := selectRelease(nil, "latest"); got != "" {
		t.Errorf("empty list should return empty string, got %q", got)
	}
	only := []Release{{TagName: "v1.0.0-rc1", Prerelease: true}}
	if got := selectRelease(only, "latest-stable"); got != "" {
		t.Errorf("no stable in list should return empty, got %q", got)
	}
	if got := selectRelease([]Release{{TagName: "v1.0", Prerelease: false}}, "pre-release"); got != "" {
		t.Errorf("no prerelease in list should return empty, got %q", got)
	}
}

func TestMakeTiles(t *testing.T) {
	apps := []AppConfig{
		{Name: "lazygit"},
		{Name: "delta"},
	}
	tiles := makeTiles(apps)
	if len(tiles) != 2 {
		t.Fatalf("got %d tiles, want 2", len(tiles))
	}
	for i, tile := range tiles {
		if tile.Name != apps[i].Name {
			t.Errorf("tile %d name = %q, want %q", i, tile.Name, apps[i].Name)
		}
		if tile.Status != TileStatusLoading {
			t.Errorf("tile %d status = %v, want loading", i, tile.Status)
		}
	}
}

func TestFetchTileDataInvalidRepo(t *testing.T) {
	cmd := fetchTileData(context.Background(), 0, AppConfig{Repo: "no-slash"}, "")
	msg := cmd().(tileUpdatedMsg)
	if msg.err == "" {
		t.Error("expected error for invalid repo")
	}
	if !strings.Contains(msg.err, "no-slash") {
		t.Errorf("error should reference the repo string, got %q", msg.err)
	}
}

func TestTileUpdatedMsgHandler(t *testing.T) {
	m := model{
		tiles: []TileInfo{
			{Name: "a", Status: TileStatusLoading},
			{Name: "b", Status: TileStatusLoading},
		},
	}

	// Successful update.
	out, _ := m.Update(tileUpdatedMsg{index: 0, latestVersion: "v1.0", installedVersion: "v0.9"})
	r := out.(model)
	if r.tiles[0].Status != TileStatusReady {
		t.Errorf("status = %v, want ready", r.tiles[0].Status)
	}
	if r.tiles[0].LatestVersion != "v1.0" || r.tiles[0].InstalledVersion != "v0.9" {
		t.Errorf("versions mismatch: %+v", r.tiles[0])
	}

	// Error update.
	out, _ = r.Update(tileUpdatedMsg{index: 1, err: "boom"})
	r = out.(model)
	if r.tiles[1].Status != TileStatusError {
		t.Errorf("status = %v, want error", r.tiles[1].Status)
	}
	if r.tiles[1].Err != "boom" {
		t.Errorf("err = %q, want boom", r.tiles[1].Err)
	}

	// Out-of-range index is ignored.
	out, _ = r.Update(tileUpdatedMsg{index: 99, latestVersion: "x"})
	if got := len(out.(model).tiles); got != 2 {
		t.Errorf("out-of-range update should not grow tiles, got len=%d", got)
	}
}
