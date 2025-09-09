# asset-fetch (Go version)

Interactive CLI tool for downloading GitHub release assets from public and private repositories with fuzzy search. No GitHub CLI required - direct API integration. Features bubbletea TUI, smart formatting, and token-based authentication.

It uses the [bubbletea](https://github.com/charmbracelet/bubbletea) library to create a terminal interface.

## Features

- Reading configuration from `afetch.conf`
- Getting release list from GitHub API
- Displaying artifacts in a convenient TUI with bubbletea
- Keyboard navigation support
- Confirmation before download
- Displaying download progress

## Installation

To build from source, you will need Go 1.21+:

```bash
go build -o afetch
```

## Usage

1. Configure the `afetch.conf` file with your GitHub token and repository information
2. Run the application: `./afetch`
3. Select the desired artifact from the list using arrow keys
4. Press Enter to confirm selection
5. Confirm download by pressing 'y' or cancel by pressing 'n'

## Controls

- Up/Down arrows or j/k - navigate the list
- Enter - select artifact
- y - confirm download
- n - cancel download
- q or Ctrl+C - exit the application

## Configuration

The application reads configuration from the `afetch.conf` file, which should contain:

```bash
# GitHub API configuration
readonly GITHUB_TOKEN="your_github_token_here"
readonly REPO_OWNER="repository_owner"
readonly REPO_NAME="repository_name"
readonly ASSET_MASK="*.tag.gz"
```

The configuration file is searched in the following locations:
1. In the same directory as the executable file
2. In `~/.config/afetch.conf`