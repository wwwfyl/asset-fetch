package main

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
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

// getInstalledVersion runs cfg.Command through `sh -c` (same as install and
// uninstall steps, so ~, quoting, env vars and pipes all work) and returns
// the first capture group of cfg.Regex applied to the command's combined
// output. Returns "" when the command is empty, not found, or the regex does
// not match. The command has a 5-second timeout so dashboard refresh time is
// bounded.
func getInstalledVersion(cfg VersionConfig) string {
	if cfg.Command == "" || cfg.Regex == "" {
		return ""
	}

	re, err := regexp.Compile(cfg.Regex)
	if err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "sh", "-c", cfg.Command).CombinedOutput()

	// 127/126 = command not found / not executable: sh reports that on
	// stderr, which a permissive regex would happily match.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code == 126 || code == 127 {
			return ""
		}
	}

	m := re.FindSubmatch(output)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}
