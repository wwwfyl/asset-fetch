package main

import "strings"

// tokenForApp returns the app's per-app GitHub token if set, otherwise the
// global token. This is the canonical "per-app token with global fallback"
// resolution used everywhere a single app is in scope.
func tokenForApp(app AppConfig, globalToken string) string {
	if app.GitHubToken != "" {
		return app.GitHubToken
	}
	return globalToken
}

// tokenForRepo finds an app whose repo equals owner/repo (case-insensitive)
// and returns its effective token via tokenForApp. Falls back to globalToken
// when no app matches.
func tokenForRepo(owner, repo string, apps []AppConfig, globalToken string) string {
	target := owner + "/" + repo
	for _, app := range apps {
		if strings.EqualFold(app.Repo, target) {
			return tokenForApp(app, globalToken)
		}
	}
	return globalToken
}
