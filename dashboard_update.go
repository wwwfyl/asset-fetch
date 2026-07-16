package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// startAppUpdate runs the full update pipeline (fetch → download → unpack →
// install) for the app at idx in a background goroutine and emits an
// tileOpCompleteMsg with the outcome.
func startAppUpdate(ctx context.Context, idx int, app AppConfig, globalToken string) tea.Cmd {
	return func() tea.Msg {
		msg := tileOpCompleteMsg{index: idx}
		if err := doUpdate(ctx, app, globalToken); err != nil {
			msg.err = err.Error()
			return msg
		}
		msg.newInstalled = getInstalledVersion(app.Version)
		return msg
	}
}

// doUpdate validates the app, fetches its latest release, downloads the
// matching asset into a temp directory, then runs install.unpack and
// install.steps with $ASSET_FILE/$WORK_DIR/$INSTALL_DIR/$VERSION set.
func doUpdate(ctx context.Context, app AppConfig, globalToken string) error {
	log.Printf("update[%s]: starting, repo=%s release_type=%q asset_mask=%q", app.Name, app.Repo, app.ReleaseType, app.AssetMask)

	owner, name, ok := splitRepo(app.Repo)
	if !ok {
		return fmt.Errorf("invalid repo: %s", app.Repo)
	}
	if app.AssetMask == "" {
		return fmt.Errorf("asset_mask is required for the update flow")
	}

	token := tokenForApp(app, globalToken)
	log.Printf("update[%s]: tokenSet=%v (per-app=%v)", app.Name, token != "", app.GitHubToken != "")

	releases, err := fetchReleasesFromGitHub(ctx, owner, name, "", token)
	if err != nil {
		log.Printf("update[%s]: fetch releases failed: %v", app.Name, err)
		return err
	}
	log.Printf("update[%s]: fetched %d releases", app.Name, len(releases))

	release, err := pickRelease(releases, app.ReleaseType)
	if err != nil {
		log.Printf("update[%s]: pickRelease failed: %v", app.Name, err)
		return err
	}
	log.Printf("update[%s]: picked release %s (prerelease=%v)", app.Name, release.TagName, release.Prerelease)

	asset, err := pickAsset(release.Assets, app.AssetMask)
	if err != nil {
		log.Printf("update[%s]: pickAsset failed: %v", app.Name, err)
		return err
	}
	log.Printf("update[%s]: matched asset %s (size=%d)", app.Name, asset.Name, asset.Size)

	workDir, err := os.MkdirTemp("", "afetch-update-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(workDir)
	log.Printf("update[%s]: work dir %s", app.Name, workDir)

	formatter := AssetFormatter{}
	assetInfo := formatter.FormatAssetInfo(*asset, *release)
	log.Printf("update[%s]: downloading…", app.Name)
	switch m := downloadAsset(ctx, assetInfo, token, workDir, &ProgressState{})().(type) {
	case downloadErrorMsg:
		log.Printf("update[%s]: download failed: %s", app.Name, string(m))
		return fmt.Errorf("download: %s", string(m))
	case downloadCompleteMsg:
		log.Printf("update[%s]: downloaded to %s", app.Name, m.filename)
	}

	installDir := resolveInstallDir(app)
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("create install dir %s: %w", installDir, err)
	}
	log.Printf("update[%s]: install_dir=%s", app.Name, installDir)

	env := map[string]string{
		"ASSET_FILE":  filepath.Join(workDir, asset.Name),
		"WORK_DIR":    workDir,
		"INSTALL_DIR": installDir,
		"VERSION":     normalizeVersion(release.TagName),
	}

	log.Printf("update[%s]: running %d unpack step(s)", app.Name, len(app.Install.Unpack))
	if err := runSteps(app.Install.Unpack, env); err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	log.Printf("update[%s]: running %d install step(s)", app.Name, len(app.Install.Steps))
	if err := runSteps(app.Install.Steps, env); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	log.Printf("update[%s]: done", app.Name)
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
