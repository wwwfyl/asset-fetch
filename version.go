package main

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

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
