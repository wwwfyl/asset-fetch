package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runSteps executes a list of install/uninstall steps in order. Each step is
// run as `sh -c "<step.Run>"`. The env map is merged into the child process
// environment, and env["WORK_DIR"] (if set) becomes the working directory.
// On the first failing step, runSteps returns an error annotated with the
// step name (or its 1-based index) and the captured combined output.
func runSteps(steps []InstallStep, env map[string]string) error {
	workDir := env["WORK_DIR"]
	for i, step := range steps {
		label := step.Name
		if label == "" {
			label = fmt.Sprintf("step %d", i+1)
		}
		if strings.TrimSpace(step.Run) == "" {
			log.Printf("step[%s]: skipped (empty run)", label)
			continue
		}
		log.Printf("step[%s]: running: %s", label, step.Run)
		cmd := exec.Command("sh", "-c", step.Run)
		if workDir != "" {
			cmd.Dir = workDir
		}
		cmd.Env = append(os.Environ(), envSlice(env)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("step[%s]: failed: %v: %s", label, err, strings.TrimSpace(string(out)))
			return fmt.Errorf("%s: %w: %s", label, err, strings.TrimSpace(string(out)))
		}
		log.Printf("step[%s]: ok", label)
	}
	return nil
}

// resolveInstallDir returns the app's install directory: the configured one
// (with ~ expanded) or the euid-based default.
func resolveInstallDir(app AppConfig) string {
	dir := app.InstallDir
	if dir == "" {
		dir = defaultInstallDir()
	}
	return expandHome(dir)
}

// defaultInstallDir returns the install directory used when an app omits one:
// /usr/local/bin for root, $HOME/bin for any other user.
func defaultInstallDir() string {
	if os.Geteuid() == 0 {
		return "/usr/local/bin"
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "bin")
	}
	return "bin"
}

// envSlice converts a map into the KEY=VALUE slice form expected by exec.Cmd.Env.
func envSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}

// expandHome replaces a leading "~/" or a bare "~" in p with the user's
// home directory. yaml.v3 does not expand the tilde, and neither does sh
// when the tilde is inside a parameter expansion, so we resolve it here.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
