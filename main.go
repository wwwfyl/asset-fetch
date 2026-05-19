package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var repoOwner, repoName, tag string
	var assetMask *string
	var startWithReleases bool
	var debug bool

	// Strip --debug from args; treat the first remaining arg as a possible URL.
	args := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		switch a {
		case "--debug":
			debug = true
		case "--version", "-v":
			fmt.Printf("afetch version %s\n", version)
			os.Exit(0)
		default:
			args = append(args, a)
		}
	}

	if debug {
		f, err := tea.LogToFile("afetch-debug.log", "afetch")
		if err != nil {
			fmt.Fprintf(os.Stderr, "debug log error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		log.Printf("debug logging enabled; args=%v", os.Args)
	} else {
		log.SetOutput(io.Discard)
	}

	if len(args) > 0 {
		arg := args[0]
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			parsedURL, err := url.Parse(arg)
			if err == nil && (parsedURL.Host == "github.com" || parsedURL.Host == "www.github.com") {
				pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
				if len(pathParts) >= 2 {
					repoOwner = pathParts[0]
					repoName = pathParts[1]
					if len(pathParts) > 4 && pathParts[2] == "releases" && pathParts[3] == "tag" {
						tag = pathParts[4]
						emptyString := ""
						assetMask = &emptyString
						startWithReleases = false
					} else {
						startWithReleases = true
					}
				}
			}
			log.Printf("url mode: owner=%q repo=%q tag=%q startWithReleases=%v", repoOwner, repoName, tag, startWithReleases)
		}
	}

	urlMode := repoOwner != "" && repoName != ""

	// Load the YAML multi-app config (auto-migrates legacy key=value files).
	var globalToken, configErr string
	var apps []AppConfig
	if cfg, err := loadGlobalConfig(); err == nil {
		globalToken = cfg.GitHubToken
		apps = cfg.Apps
		log.Printf("loadGlobalConfig ok: apps=%d tokenSet=%v", len(apps), globalToken != "")
	} else if !urlMode {
		configErr = err.Error()
		log.Printf("loadGlobalConfig failed and no URL: %v", err)
	} else {
		log.Printf("loadGlobalConfig failed but URL provided, continuing: %v", err)
	}

	// URL mode keeps the single-target flow and prefers the matching app's
	// token. A YAML config with a single app that has asset_mask set behaves
	// like the legacy single-app flow — jump straight to filtered assets,
	// skipping the dashboard. Everything else starts on the dashboard with
	// one tile per app.
	gitHubToken := globalToken
	startState := StateReleases
	var tiles []TileInfo
	loading := false
	singleFilterApp := len(apps) == 1 && apps[0].AssetMask != ""
	switch {
	case configErr != "":
		// View() will surface the error; no need to fetch anything.
	case urlMode:
		gitHubToken = tokenForRepo(repoOwner, repoName, apps, globalToken)
		loading = true
	case singleFilterApp:
		app := apps[0]
		parts := strings.SplitN(app.Repo, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			repoOwner = parts[0]
			repoName = parts[1]
			am := app.AssetMask
			assetMask = &am
			startWithReleases = false
			gitHubToken = tokenForApp(app, globalToken)
			loading = true
		} else {
			// Malformed repo falls back to the dashboard so the user can fix it.
			startState = StateDashboard
			tiles = makeTiles(apps)
		}
	default:
		startState = StateDashboard
		tiles = makeTiles(apps)
	}

	downloadDir, _ := os.Getwd()
	log.Printf("model init: urlMode=%v startState=%v apps=%d errorMsg=%q", urlMode, startState, len(apps), configErr)

	m := model{
		loading:           loading,
		state:             startState,
		repoOwner:         repoOwner,
		repoName:          repoName,
		tag:               tag,
		assetMask:         assetMask,
		startWithReleases: startWithReleases,
		downloadCtx:       ctx,
		downloadCancel:    cancel,
		gitHubToken:       gitHubToken,
		globalToken:       globalToken,
		downloadDir:       downloadDir,
		errorMsg:          configErr,
		apps:              apps,
		tiles:             tiles,
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		fmt.Println("Download cancelled by user")
		os.Exit(0)
	}
}
