package main

import (
	"fmt"
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
		if strings.TrimSpace(step.Run) == "" {
			continue
		}
		cmd := exec.Command("sh", "-c", step.Run)
		if workDir != "" {
			cmd.Dir = workDir
		}
		cmd.Env = append(os.Environ(), envSlice(env)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			label := step.Name
			if label == "" {
				label = fmt.Sprintf("step %d", i+1)
			}
			return fmt.Errorf("%s: %w: %s", label, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
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
