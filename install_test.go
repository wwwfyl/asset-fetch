package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStepsHappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.txt")
	env := map[string]string{
		"MY_VAR": "hello",
		"OUT":    out,
	}
	steps := []InstallStep{
		{Name: "write", Run: `printf %s "$MY_VAR" > "$OUT"`},
	}
	if err := runSteps(steps, env); err != nil {
		t.Fatalf("runSteps: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRunStepsUsesWorkDir(t *testing.T) {
	tmpDir := t.TempDir()
	steps := []InstallStep{
		{Run: `pwd > pwd.txt`},
	}
	if err := runSteps(steps, map[string]string{"WORK_DIR": tmpDir}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(tmpDir, "pwd.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), filepath.Base(tmpDir)) {
		t.Errorf("pwd output %q does not contain workdir basename %q", got, filepath.Base(tmpDir))
	}
}

func TestRunStepsFailurePropagatesName(t *testing.T) {
	err := runSteps([]InstallStep{{Name: "boom", Run: "exit 7"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q should mention step name", err)
	}
}

func TestRunStepsEmpty(t *testing.T) {
	if err := runSteps(nil, nil); err != nil {
		t.Errorf("nil steps should not error: %v", err)
	}
	if err := runSteps([]InstallStep{{Run: "   "}}, nil); err != nil {
		t.Errorf("blank Run should not error: %v", err)
	}
}

func TestDefaultInstallDir(t *testing.T) {
	dir := defaultInstallDir()
	if dir == "" {
		t.Fatal("defaultInstallDir returned empty string")
	}
	if os.Geteuid() == 0 {
		if dir != "/usr/local/bin" {
			t.Errorf("root: got %q, want /usr/local/bin", dir)
		}
		return
	}
	home, _ := os.UserHomeDir()
	if dir != filepath.Join(home, "bin") {
		t.Errorf("non-root: got %q, want %s/bin", dir, home)
	}
}
