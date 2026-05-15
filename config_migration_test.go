package main

import (
	"os"
	"strings"
	"testing"
)

func TestMigrateLegacyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := tmpDir + "/afetch.conf"
	legacy := []byte(`GITHUB_TOKEN="ghp_test123"
REPO_OWNER="jesseduffield"
REPO_NAME="lazygit"
ASSET_MASK="*linux_x86_64.tar.gz"
`)
	if err := os.WriteFile(confPath, legacy, 0600); err != nil {
		t.Fatal(err)
	}

	if !isLegacyFormat(legacy) {
		t.Fatal("isLegacyFormat returned false for legacy content")
	}

	newContent, err := migrateLegacyConfig(confPath, legacy)
	if err != nil {
		t.Fatalf("migrateLegacyConfig: %v", err)
	}

	if isLegacyFormat(newContent) {
		t.Fatal("new content still detected as legacy")
	}

	bak, err := os.ReadFile(confPath + ".bak")
	if err != nil || string(bak) != string(legacy) {
		t.Fatalf("backup mismatch: %v / %q", err, bak)
	}

	disk, _ := os.ReadFile(confPath)
	for _, want := range []string{"github_token: ghp_test123", "repo: jesseduffield/lazygit", "name: lazygit", "release_type: latest"} {
		if !strings.Contains(string(disk), want) {
			t.Errorf("migrated YAML missing %q in:\n%s", want, disk)
		}
	}
}
