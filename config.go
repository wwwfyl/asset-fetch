package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadGlobalConfig loads the multi-app YAML config. If the existing config file
// is in the legacy key=value format, it is migrated in-place; the original is
// preserved as <path>.bak.
func loadGlobalConfig() (*GlobalConfig, error) {
	fileToRead, err := findConfigFile()
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(fileToRead)
	if err != nil {
		return nil, err
	}

	if isLegacyFormat(content) {
		content, err = migrateLegacyConfig(fileToRead, content)
		if err != nil {
			return nil, err
		}
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML config: %w", err)
	}
	return &cfg, nil
}

// findConfigFile returns the first existing config file. It checks afetch.yaml
// (canonical) and afetch.conf (legacy) in both the binary's directory and the
// platform-specific home location.
func findConfigFile() (string, error) {
	scriptDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "", err
	}

	var homeDir string
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			homeDir = filepath.Join(localAppData, "afetch")
		}
	} else {
		homeDir = filepath.Join(os.Getenv("HOME"), ".config")
	}

	// Probe order: script dir (.yaml, .conf) then home dir (.yaml, .conf).
	candidates := []string{
		filepath.Join(scriptDir, "afetch.yaml"),
		filepath.Join(scriptDir, "afetch.conf"),
	}
	if homeDir != "" {
		candidates = append(candidates,
			filepath.Join(homeDir, "afetch.yaml"),
			filepath.Join(homeDir, "afetch.conf"),
		)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("configuration file not found (checked: %s)", strings.Join(candidates, ", "))
}

// isLegacyFormat reports whether the content contains REPO_OWNER= or REPO_NAME=
// lines, indicating the pre-YAML key=value format.
func isLegacyFormat(content []byte) bool {
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "REPO_OWNER=") || strings.HasPrefix(line, "REPO_NAME=") {
			return true
		}
	}
	return false
}

// migrateLegacyConfig converts a key=value config to YAML next to the original,
// changing the extension to .yaml. The original file is preserved as <oldPath>.bak.
// Returns the new YAML content.
func migrateLegacyConfig(oldPath string, oldContent []byte) ([]byte, error) {
	legacy := parseLegacyContent(oldContent)

	backupPath := oldPath + ".bak"
	if err := os.WriteFile(backupPath, oldContent, 0600); err != nil {
		return nil, fmt.Errorf("creating backup %s: %w", backupPath, err)
	}

	app := AppConfig{
		Name:        legacy.RepoName,
		ReleaseType: "latest",
		AssetMask:   legacy.AssetMask,
	}
	if legacy.RepoOwner != "" && legacy.RepoName != "" {
		app.Repo = legacy.RepoOwner + "/" + legacy.RepoName
	}
	if app.Name == "" {
		app.Name = "app"
	}

	newCfg := GlobalConfig{
		GitHubToken: legacy.GitHubToken,
		Apps:        []AppConfig{app},
	}

	yamlContent, err := yaml.Marshal(&newCfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling YAML: %w", err)
	}

	newPath := strings.TrimSuffix(oldPath, filepath.Ext(oldPath)) + ".yaml"
	if err := os.WriteFile(newPath, yamlContent, 0600); err != nil {
		return nil, fmt.Errorf("writing migrated config %s: %w", newPath, err)
	}

	if newPath != oldPath {
		if err := os.Remove(oldPath); err != nil {
			return nil, fmt.Errorf("removing legacy file %s: %w", oldPath, err)
		}
	}

	fmt.Fprintf(os.Stderr, "migrated %s to %s (backup: %s)\n", oldPath, newPath, backupPath)
	return yamlContent, nil
}

// parseLegacyContent parses the legacy key=value config from in-memory bytes.
func parseLegacyContent(content []byte) *Config {
	cfg := &Config{}
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}
		switch key {
		case "GITHUB_TOKEN":
			cfg.GitHubToken = value
		case "REPO_OWNER":
			cfg.RepoOwner = value
		case "REPO_NAME":
			cfg.RepoName = value
		case "ASSET_MASK":
			cfg.AssetMask = value
		}
	}
	return cfg
}
