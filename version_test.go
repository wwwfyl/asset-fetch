package main

import "testing"

func TestGetInstalledVersion(t *testing.T) {
	tests := []struct {
		name string
		cfg  VersionConfig
		want string
	}{
		{"empty command", VersionConfig{}, ""},
		{"empty regex", VersionConfig{Command: "echo hi"}, ""},
		{"match", VersionConfig{Command: "echo version=1.2.3", Regex: `version=([0-9.]+)`}, "1.2.3"},
		{"no match", VersionConfig{Command: "echo hello", Regex: `version=([0-9.]+)`}, ""},
		{"bad regex", VersionConfig{Command: "echo hi", Regex: `[`}, ""},
		{"missing binary", VersionConfig{Command: "definitely_not_a_command_xyz", Regex: `(.+)`}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getInstalledVersion(tc.cfg); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
