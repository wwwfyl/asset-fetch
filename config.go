package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadConfig loads configuration from file with Windows support
func loadConfig() (*Config, error) {
	scriptDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return nil, err
	}

	configFile := filepath.Join(scriptDir, "afetch.conf")

	// Determine home config file path based on OS
	var homeConfigFile string
	if runtime.GOOS == "windows" {
		// For Windows use %LOCALAPPDATA%\afetch\afetch.conf
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			homeConfigFile = filepath.Join(localAppData, "afetch", "afetch.conf")
		}
	} else {
		// For Linux/macOS use ~/.config/afetch.conf
		homeConfigFile = filepath.Join(os.Getenv("HOME"), ".config", "afetch.conf")
	}

	// Check if configuration file exists
	var fileToRead string
	if _, err := os.Stat(configFile); err == nil {
		fileToRead = configFile
	} else if homeConfigFile != "" {
		if _, err := os.Stat(homeConfigFile); err == nil {
			fileToRead = homeConfigFile
		}
	}

	if fileToRead == "" {
		if homeConfigFile != "" {
			return nil, fmt.Errorf("configuration file not found in %s or %s", configFile, homeConfigFile)
		} else {
			return nil, fmt.Errorf("configuration file not found in %s", configFile)
		}
	}

	// Read configuration file
	content, err := os.ReadFile(fileToRead)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	lines := strings.Split(string(content), "\n")

	for _, line := range lines {
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

		// Remove quotes
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}

		switch key {
		case "GITHUB_TOKEN":
			config.GitHubToken = value
		case "REPO_OWNER":
			config.RepoOwner = value
		case "REPO_NAME":
			config.RepoName = value
		case "ASSET_MASK":
			config.AssetMask = value
		}
	}

	return config, nil
}

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

// findConfigFile returns the path to the first existing afetch.conf in either
// the binary's directory or the platform-specific home location.
func findConfigFile() (string, error) {
	scriptDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "", err
	}
	configFile := filepath.Join(scriptDir, "afetch.conf")

	var homeConfigFile string
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			homeConfigFile = filepath.Join(localAppData, "afetch", "afetch.conf")
		}
	} else {
		homeConfigFile = filepath.Join(os.Getenv("HOME"), ".config", "afetch.conf")
	}

	if _, err := os.Stat(configFile); err == nil {
		return configFile, nil
	}
	if homeConfigFile != "" {
		if _, err := os.Stat(homeConfigFile); err == nil {
			return homeConfigFile, nil
		}
		return "", fmt.Errorf("configuration file not found in %s or %s", configFile, homeConfigFile)
	}
	return "", fmt.Errorf("configuration file not found in %s", configFile)
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

// migrateLegacyConfig converts a key=value config to YAML in-place, keeping the
// original at <path>.bak. The new YAML content is returned for immediate parsing.
func migrateLegacyConfig(path string, oldContent []byte) ([]byte, error) {
	legacy := parseLegacyContent(oldContent)

	backupPath := path + ".bak"
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

	if err := os.WriteFile(path, yamlContent, 0600); err != nil {
		return nil, fmt.Errorf("writing migrated config %s: %w", path, err)
	}

	fmt.Fprintf(os.Stderr, "migrated %s to YAML format (backup: %s)\n", path, backupPath)
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
