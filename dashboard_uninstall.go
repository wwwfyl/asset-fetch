package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// startAppUninstall runs the uninstall pipeline for the app at idx in a
// background goroutine and emits an uninstallCompleteMsg with the outcome.
func startAppUninstall(idx int, app AppConfig) tea.Cmd {
	return func() tea.Msg {
		msg := uninstallCompleteMsg{index: idx}
		if err := doUninstall(app); err != nil {
			msg.err = err.Error()
			return msg
		}
		msg.succeeded = true
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
	if len(app.Uninstall.Steps) == 0 {
		return fmt.Errorf("no uninstall steps configured")
	}
	installDir := app.InstallDir
	if installDir == "" {
		installDir = defaultInstallDir()
	}
	installDir = expandHome(installDir)

	env := map[string]string{
		"INSTALL_DIR": installDir,
		"VERSION":     getInstalledVersion(app.Version),
	}
	return runSteps(app.Uninstall.Steps, env)
}
