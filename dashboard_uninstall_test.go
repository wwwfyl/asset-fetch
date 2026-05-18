package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoUninstallRunsSteps(t *testing.T) {
	tmp := t.TempDir()
	app := AppConfig{
		InstallDir: tmp,
		Uninstall: UninstallConfig{
			Steps: []InstallStep{
				{Run: `printf done > "$INSTALL_DIR/removed.txt"`},
			},
		},
	}
	if err := doUninstall(app); err != nil {
		t.Fatalf("doUninstall: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(tmp, "removed.txt"))
	if err != nil {
		t.Fatalf("marker file: %v", err)
	}
	if string(got) != "done" {
		t.Errorf("marker content = %q, want %q", got, "done")
	}
}

func TestDoUninstallErrorsWithoutSteps(t *testing.T) {
	if err := doUninstall(AppConfig{}); err == nil {
		t.Error("expected error when uninstall.steps is empty")
	}
}

func TestDoUninstallPropagatesStepError(t *testing.T) {
	app := AppConfig{
		InstallDir: "/tmp",
		Uninstall: UninstallConfig{
			Steps: []InstallStep{{Name: "boom", Run: "exit 3"}},
		},
	}
	err := doUninstall(app)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestYKeyTriggersUninstall(t *testing.T) {
	m := model{
		state:        StateDashboard,
		selectedTile: 0,
		tiles:        []TileInfo{{Name: "x", Status: TileStatusReady}},
		apps: []AppConfig{{
			Name: "x",
			Uninstall: UninstallConfig{
				Steps: []InstallStep{{Run: "true"}},
			},
		}},
		confirmUninstall: true,
	}
	out, cmd := m.handleDashboardInput("y")
	r := out.(model)
	if r.confirmUninstall {
		t.Error("y should clear confirmUninstall")
	}
	if r.tiles[0].Status != TileStatusUninstalling {
		t.Errorf("status = %v, want uninstalling", r.tiles[0].Status)
	}
	if cmd == nil {
		t.Error("y should return a Cmd")
	}
}

func TestYKeyOnBusyTileNoOp(t *testing.T) {
	m := model{
		state:            StateDashboard,
		selectedTile:     0,
		tiles:            []TileInfo{{Name: "x", Status: TileStatusUpdating}},
		apps:             []AppConfig{{Name: "x"}},
		confirmUninstall: true,
	}
	out, cmd := m.handleDashboardInput("y")
	r := out.(model)
	if r.tiles[0].Status != TileStatusUpdating {
		t.Errorf("status changed to %v while busy", r.tiles[0].Status)
	}
	if cmd != nil {
		t.Error("y on busy tile should not return a Cmd")
	}
}

func TestUninstallCompleteMsgHandler(t *testing.T) {
	m := model{
		tiles: []TileInfo{{Name: "x", Status: TileStatusUninstalling, InstalledVersion: "v1"}},
	}

	out, _ := m.Update(uninstallCompleteMsg{index: 0, succeeded: true, newInstalled: ""})
	r := out.(model)
	if r.tiles[0].Status != TileStatusReady {
		t.Errorf("status = %v, want ready", r.tiles[0].Status)
	}
	if r.tiles[0].InstalledVersion != "" {
		t.Errorf("installed = %q, want empty", r.tiles[0].InstalledVersion)
	}

	r.tiles[0].Status = TileStatusUninstalling
	out, _ = r.Update(uninstallCompleteMsg{index: 0, err: "denied"})
	r = out.(model)
	if r.tiles[0].Status != TileStatusError {
		t.Errorf("status = %v, want error", r.tiles[0].Status)
	}
	if r.tiles[0].Err != "denied" {
		t.Errorf("err = %q, want denied", r.tiles[0].Err)
	}
}
