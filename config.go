package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
