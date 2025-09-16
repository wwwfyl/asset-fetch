package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Build-time variables that will be set by GoReleaser
var (
	version     = "dev"
	commit      = "unknown"
	date        = "unknown"
	buildSource = "source"
)

func main() {
	// Create context with cancel function
	downloadContext, downloadCancel = context.WithCancel(context.Background())
	defer downloadCancel()

	var repoOwner, repoName, tag string
	var assetMask *string
	var startWithReleases bool

	if len(os.Args) > 1 {
		arg := os.Args[1]
		// Check for version flag
		if arg == "--version" || arg == "-v" {
			fmt.Printf("afetch version %s\n", version)
			os.Exit(0)
		}

		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			parsedURL, err := url.Parse(arg)
			if err == nil && (parsedURL.Host == "github.com" || parsedURL.Host == "www.github.com") {
				pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
				if len(pathParts) >= 2 {
					repoOwner = pathParts[0]
					repoName = pathParts[1]
					if len(pathParts) > 3 && pathParts[2] == "releases" && pathParts[3] == "tag" {
						tag = pathParts[4]
						emptyString := ""
						assetMask = &emptyString
						startWithReleases = false
					} else {
						startWithReleases = true
					}
				}
			}
		}
	}

	// Initialize unified model
	m := model{
		loading:           true,
		state:             StateReleases,
		repoOwner:         repoOwner,
		repoName:          repoName,
		tag:               tag,
		assetMask:         assetMask,
		startWithReleases: startWithReleases,
	}

	// Run bubbletea
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Check if context was cancelled
	if errors.Is(downloadContext.Err(), context.Canceled) {
		fmt.Println("Download cancelled by user")
		os.Exit(0)
	}
}
