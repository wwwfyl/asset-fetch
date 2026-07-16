package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickRelease(t *testing.T) {
	all := []Release{
		{TagName: "v2.0-rc1", Prerelease: true},
		{TagName: "v1.0", Prerelease: false},
		{TagName: "v0.9", Prerelease: false},
	}
	cases := []struct {
		releaseType string
		wantTag     string
		wantErr     bool
	}{
		{"", "v2.0-rc1", false},
		{"latest", "v2.0-rc1", false},
		{"latest-stable", "v1.0", false},
		{"pre-release", "v2.0-rc1", false},
	}
	for _, c := range cases {
		t.Run(c.releaseType, func(t *testing.T) {
			r, err := pickRelease(all, c.releaseType)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err == nil && r.TagName != c.wantTag {
				t.Errorf("tag = %q, want %q", r.TagName, c.wantTag)
			}
		})
	}
}

func TestPickReleaseErrors(t *testing.T) {
	if _, err := pickRelease(nil, "latest"); err == nil {
		t.Error("empty list should error")
	}
	stable := []Release{{TagName: "rc", Prerelease: true}}
	if _, err := pickRelease(stable, "latest-stable"); err == nil {
		t.Error("no stable should error")
	}
	pre := []Release{{TagName: "v1", Prerelease: false}}
	if _, err := pickRelease(pre, "pre-release"); err == nil {
		t.Error("no prerelease should error")
	}
}

func TestPickAsset(t *testing.T) {
	assets := []Asset{
		{Name: "lazygit_0.44_linux_x86_64.tar.gz"},
		{Name: "lazygit_0.44_darwin_x86_64.tar.gz"},
		{Name: "checksums.txt"},
	}
	got, err := pickAsset(assets, "*linux_x86_64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Name, "linux") {
		t.Errorf("unexpected asset: %q", got.Name)
	}

	if _, err := pickAsset(assets, "*nonexistent*"); err == nil {
		t.Error("expected error for no match")
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"~":         home,
		"~/bin":     filepath.Join(home, "bin"),
		"~/.config": filepath.Join(home, ".config"),
		"/abs":      "/abs",
		"":          "",
		"relative":  "relative",
		"~user":     "~user", // only ~/ and bare ~ expand; ~user is left alone
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdateCompleteMsgHandler(t *testing.T) {
	m := model{
		tiles: []TileInfo{
			{Name: "a", Status: TileStatusUpdating, LatestVersion: "v2"},
		},
	}

	// Success path updates installed and clears the status.
	out, _ := m.Update(updateCompleteMsg{index: 0, newInstalled: "v2"})
	r := out.(model)
	if r.tiles[0].Status != TileStatusReady {
		t.Errorf("status = %v, want ready", r.tiles[0].Status)
	}
	if r.tiles[0].InstalledVersion != "v2" {
		t.Errorf("installed = %q, want v2", r.tiles[0].InstalledVersion)
	}

	// Error path sets error status and message.
	r.tiles[0].Status = TileStatusUpdating
	out, _ = r.Update(updateCompleteMsg{index: 0, err: "boom"})
	r = out.(model)
	if r.tiles[0].Status != TileStatusError {
		t.Errorf("status = %v, want error", r.tiles[0].Status)
	}
	if r.tiles[0].Err != "boom" {
		t.Errorf("err = %q, want boom", r.tiles[0].Err)
	}
}

func TestUKeyMarksTileUpdating(t *testing.T) {
	m := model{
		state:        StateDashboard,
		selectedTile: 0,
		tiles:        []TileInfo{{Name: "lazygit", Status: TileStatusReady}},
		apps:         []AppConfig{{Name: "lazygit", Repo: "jesseduffield/lazygit", AssetMask: "*linux_x86_64.tar.gz"}},
	}
	out, cmd := m.handleDashboardInput("u")
	r := out.(model)
	if r.tiles[0].Status != TileStatusUpdating {
		t.Errorf("status = %v, want updating", r.tiles[0].Status)
	}
	if cmd == nil {
		t.Error("u should return a Cmd")
	}
}

func TestUKeyIgnoredWhenBusy(t *testing.T) {
	m := model{
		state:        StateDashboard,
		selectedTile: 0,
		tiles:        []TileInfo{{Name: "x", Status: TileStatusUpdating}},
		apps:         []AppConfig{{Name: "x", Repo: "a/b"}},
	}
	_, cmd := m.handleDashboardInput("u")
	if cmd != nil {
		t.Error("u on a busy tile should be a no-op")
	}
}
