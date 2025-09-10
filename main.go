package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Create context with cancel function
	downloadContext, downloadCancel = context.WithCancel(context.Background())
	defer downloadCancel()

	// Initialize unified model
	m := model{
		loading: true,
		state:   StateReleases,
	}

	// Run bubbletea
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Check if context was cancelled
	if errors.Is(downloadContext.Err(), context.Canceled) {
		fmt.Println("Download cancelled by user")
		os.Exit(0)
	}
}
