package main

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// normalizeVersion strips a leading "v" or "V" from a GitHub release tag so
// the result lines up with the bare version numbers reported by most tools.
// The leading character is only dropped when followed by a digit, so tags
// like "valid" or "version-1" stay intact.
func normalizeVersion(tag string) string {
	if len(tag) >= 2 && (tag[0] == 'v' || tag[0] == 'V') && tag[1] >= '0' && tag[1] <= '9' {
		return tag[1:]
	}
	return tag
}

// getInstalledVersion runs cfg.Command and returns the first capture group of
// cfg.Regex applied to the command's combined output. Returns "" when the
// command is empty, fails to start, or the regex does not match. The command
// has a 5-second timeout so dashboard refresh time is bounded.
func getInstalledVersion(cfg VersionConfig) string {
	if cfg.Command == "" || cfg.Regex == "" {
		return ""
	}

	re, err := regexp.Compile(cfg.Regex)
	if err != nil {
		return ""
	}

	parts := strings.Fields(cfg.Command)
	if len(parts) == 0 {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, _ := exec.CommandContext(ctx, parts[0], parts[1:]...).CombinedOutput()

	m := re.FindSubmatch(output)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}
