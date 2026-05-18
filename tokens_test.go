package main

import "testing"

func TestTokenForApp(t *testing.T) {
	cases := []struct {
		name        string
		app         AppConfig
		globalToken string
		want        string
	}{
		{"per-app set wins", AppConfig{GitHubToken: "per"}, "global", "per"},
		{"per-app empty falls back", AppConfig{}, "global", "global"},
		{"both empty", AppConfig{}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tokenForApp(c.app, c.globalToken); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTokenForRepo(t *testing.T) {
	apps := []AppConfig{
		{Name: "lazygit", Repo: "jesseduffield/lazygit", GitHubToken: "lg-token"},
		{Name: "delta", Repo: "dandavison/delta"},
	}
	cases := []struct {
		name        string
		owner, repo string
		want        string
	}{
		{"matches app with token", "jesseduffield", "lazygit", "lg-token"},
		{"matches app without token falls back", "dandavison", "delta", "global"},
		{"no match falls back", "torvalds", "linux", "global"},
		{"case-insensitive match", "JesseDuffield", "LAZYGIT", "lg-token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tokenForRepo(c.owner, c.repo, apps, "global"); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
