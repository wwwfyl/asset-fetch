package main

import "testing"

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v0.61.1":     "0.61.1",
		"V1.0":        "1.0",
		"0.9.0":       "0.9.0",
		"v1.0.0-rc1":  "1.0.0-rc1",
		"":            "",
		"v":           "v",     // only "v", nothing after
		"valid":       "valid", // not followed by digit
		"version-1":   "version-1",
		"release-1.0": "release-1.0",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

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
