package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// startAppUpdate runs the full update pipeline (fetch → download → unpack →
// install) for the app at idx in a background goroutine and emits an
// updateCompleteMsg with the outcome.
func startAppUpdate(ctx context.Context, idx int, app AppConfig, globalToken string) tea.Cmd {
	return func() tea.Msg {
		msg := updateCompleteMsg{index: idx}
		if err := doUpdate(ctx, app, globalToken); err != nil {
			msg.err = err.Error()
			return msg
		}
		msg.succeeded = true
		msg.newInstalled = getInstalledVersion(app.Version)
		return msg
	}
}

// doUpdate validates the app, fetches its latest release, downloads the
// matching asset into a temp directory, then runs install.unpack and
// install.steps with $ASSET_FILE/$WORK_DIR/$INSTALL_DIR/$VERSION set.
func doUpdate(ctx context.Context, app AppConfig, globalToken string) error {
	parts := strings.SplitN(app.Repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repo: %s", app.Repo)
	}
	if app.AssetMask == "" {
		return fmt.Errorf("asset_mask is required for the update flow")
	}

	token := tokenForApp(app, globalToken)

	releases, err := fetchReleasesFromGitHub(ctx, parts[0], parts[1], "", token)
	if err != nil {
		return err
	}
	release, err := pickRelease(releases, app.ReleaseType)
	if err != nil {
		return err
	}
	asset, err := pickAsset(release.Assets, app.AssetMask)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "afetch-update-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	formatter := AssetFormatter{}
	assetInfo := formatter.FormatAssetInfo(*asset, *release)
	switch m := downloadAsset(ctx, assetInfo, token, workDir, &ProgressState{})().(type) {
	case downloadErrorMsg:
		return fmt.Errorf("download: %s", string(m))
	case checksumVerifiedMsg:
		if !m.success {
			return fmt.Errorf("download: %s", m.err)
		}
	}

	installDir := app.InstallDir
	if installDir == "" {
		installDir = defaultInstallDir()
	}
	installDir = expandHome(installDir)
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("create install dir %s: %w", installDir, err)
	}

	env := map[string]string{
		"ASSET_FILE":  filepath.Join(workDir, asset.Name),
		"WORK_DIR":    workDir,
		"INSTALL_DIR": installDir,
		"VERSION":     release.TagName,
	}

	if err := runSteps(app.Install.Unpack, env); err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	if err := runSteps(app.Install.Steps, env); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	return nil
}

// pickRelease picks one release matching releaseType:
//
//	""/"latest"     → first release (most recent by date)
//	"latest-stable" → first non-prerelease
//	"pre-release"   → first prerelease
//
// Returns an error when no release matches.
func pickRelease(releases []Release, releaseType string) (*Release, error) {
	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases")
	}
	switch releaseType {
	case "latest-stable":
		for i, r := range releases {
			if !r.Prerelease {
				return &releases[i], nil
			}
		}
		return nil, fmt.Errorf("no stable release")
	case "pre-release":
		for i, r := range releases {
			if r.Prerelease {
				return &releases[i], nil
			}
		}
		return nil, fmt.Errorf("no prerelease")
	default:
		return &releases[0], nil
	}
}

// pickAsset returns the first asset whose filename matches the glob mask.
func pickAsset(assets []Asset, mask string) (*Asset, error) {
	for i, a := range assets {
		matched, err := path.Match(mask, a.Name)
		if err == nil && matched {
			return &assets[i], nil
		}
	}
	return nil, fmt.Errorf("no asset matches %s", mask)
}
