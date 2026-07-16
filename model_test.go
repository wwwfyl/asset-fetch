package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestSearchModeAllowsTypingQ(t *testing.T) {
	m := model{state: StateAssets}
	m.listView.SetAssets([]AssetInfo{{Name: "quick.tar.gz", DisplayLine: "quick.tar.gz"}})
	m.listView.ActivateSearch()

	out, _ := m.Update(keyMsg("q"))
	got := out.(model)

	if got.quitting {
		t.Fatal("q typed in search mode must not quit")
	}
	if got.listView.filter != "q" {
		t.Errorf("filter = %q, want %q", got.listView.filter, "q")
	}
}

func TestNavModeQStillQuits(t *testing.T) {
	m := model{state: StateAssets}
	m.listView.SetAssets([]AssetInfo{{Name: "a.tar.gz", DisplayLine: "a.tar.gz"}})

	out, _ := m.Update(keyMsg("q"))
	got := out.(model)

	if !got.quitting {
		t.Error("q outside search mode must quit")
	}
}
