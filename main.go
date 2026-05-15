package main

import (
	"context"
	"errors"
	"fmt"
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
			log.Printf("url mode: owner=%q repo=%q tag=%q startWithReleases=%v", repoOwner, repoName, tag, startWithReleases)
		}
	}

	// Load config once here so no download or fetch function needs to call loadConfig.
	var gitHubToken, configErr string
	if cfg, err := loadConfig(); err == nil {
		gitHubToken = cfg.GitHubToken
		if repoOwner == "" {
			repoOwner = cfg.RepoOwner
		}
		if repoName == "" {
			repoName = cfg.RepoName
		}
		if assetMask == nil && cfg.AssetMask != "" {
			assetMask = &cfg.AssetMask
		}
		log.Printf("loadConfig ok: owner=%q repo=%q tokenSet=%v mask=%q", cfg.RepoOwner, cfg.RepoName, cfg.GitHubToken != "", cfg.AssetMask)
	} else if repoOwner == "" || repoName == "" {
		// No URL supplied and config is missing — surface the error in the TUI.
		configErr = err.Error()
		log.Printf("loadConfig failed and no URL: %v", err)
	} else {
		log.Printf("loadConfig failed but URL provided, continuing: %v", err)
	}
	downloadDir, _ := os.Getwd()
	log.Printf("model init: owner=%q repo=%q tag=%q assetMaskSet=%v downloadDir=%q errorMsg=%q", repoOwner, repoName, tag, assetMask != nil, downloadDir, configErr)

	m := model{
		loading:           configErr == "",
		state:             StateReleases,
		repoOwner:         repoOwner,
		repoName:          repoName,
		tag:               tag,
		assetMask:         assetMask,
		startWithReleases: startWithReleases,
		downloadCtx:       ctx,
		downloadCancel:    cancel,
		gitHubToken:       gitHubToken,
		downloadDir:       downloadDir,
		errorMsg:          configErr,
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
