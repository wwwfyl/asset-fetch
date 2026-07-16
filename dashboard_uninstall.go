package main

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

// startAppUninstall runs the uninstall pipeline for the app at idx in a
// background goroutine and emits a tileOpCompleteMsg with the outcome.
func startAppUninstall(idx int, app AppConfig) tea.Cmd {
	return func() tea.Msg {
		msg := tileOpCompleteMsg{index: idx}
		if err := doUninstall(app); err != nil {
			msg.err = err.Error()
			return msg
		}
		// After a successful uninstall the version probe should now return ""
		// (binary removed), which is exactly what we want on the tile.
		msg.newInstalled = getInstalledVersion(app.Version)
		return msg
	}
}

// doUninstall runs the configured uninstall steps with $INSTALL_DIR and the
// previously-installed $VERSION exposed. $ASSET_FILE and $WORK_DIR are not
// available — uninstall is purely a cleanup phase.
func doUninstall(app AppConfig) error {
	log.Printf("uninstall[%s]: starting", app.Name)
	if len(app.Uninstall.Steps) == 0 {
		log.Printf("uninstall[%s]: no steps configured", app.Name)
		return fmt.Errorf("no uninstall steps configured")
	}
	installDir := app.InstallDir
	if installDir == "" {
		installDir = defaultInstallDir()
	}
	installDir = expandHome(installDir)

	version := getInstalledVersion(app.Version)
	log.Printf("uninstall[%s]: install_dir=%s version=%q running %d step(s)", app.Name, installDir, version, len(app.Uninstall.Steps))

	env := map[string]string{
		"INSTALL_DIR": installDir,
		"VERSION":     version,
	}
	if err := runSteps(app.Uninstall.Steps, env); err != nil {
		log.Printf("uninstall[%s]: failed: %v", app.Name, err)
		return err
	}
	log.Printf("uninstall[%s]: done", app.Name)
	return nil
}
