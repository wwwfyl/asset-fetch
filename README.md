# asset-fetch (Go version)

Interactive CLI tool for downloading GitHub release assets from public and private repositories with fuzzy search. No GitHub CLI required - direct API integration. Features bubbletea TUI, smart formatting, and token-based authentication.

It uses the [bubbletea](https://github.com/charmbracelet/bubbletea) library to create a terminal interface.

## Features

- Reading configuration from `afetch.conf`
- Getting release list from GitHub API
- Displaying artifacts in a convenient TUI with bubbletea
- Multi-release support with release selection
- Multi-asset selection and batch downloading
- Keyboard navigation support
- Displaying download progress with tabular view
- Smart asset filtering with configurable masks
- Downloads with progress tracking

## Installation

To build from source, you will need Go 1.21+:

```bash
go build -o afetch
```

## Usage

1. Configure the `afetch.conf` file with your GitHub token and repository information
2. Run the application: `./afetch`
3. If no `ASSET_MASK` is configured, select a release from the list
4. Select the desired artifact(s) from the list using arrow keys
5. Use space to select/deselect multiple assets (in multi-select mode)
6. Press Enter to confirm selection
7. Confirm download by pressing 'y' or cancel by pressing 'n'

## Controls

### Release Selection Mode (when ASSET_MASK is empty)
- Up/Down arrows or j/k - navigate the release list
- Enter or Space - select release and show its assets
- q or Ctrl+C - exit the application

### Asset Selection Mode
- Up/Down arrows or j/k - navigate the asset list
- Space - toggle asset selection (select/deselect)
- Enter - start downloading selected assets (or current asset if none selected)
- q or Ctrl+C - exit the application

### Download Confirmation
- y or Y - confirm download
- n, N, Esc, q, or Ctrl+C - cancel download

### During Download
- q or Ctrl+C - cancel current download

### Download Results
- Application automatically exits after all downloads complete
- Shows success/failure status for each downloaded file

## Configuration

The application reads configuration from the `afetch.conf` file, which should contain:

```bash
# GitHub API configuration
GITHUB_TOKEN="your_github_token_here"
REPO_OWNER="repository_owner"
REPO_NAME="repository_name"
ASSET_MASK="*.tag.gz"
```

### Configuration Options

- `GITHUB_TOKEN` - Your GitHub personal access token (required for private repos, optional for public)
- `REPO_OWNER` - Repository owner (username or organization name)
- `REPO_NAME` - Repository name
- `ASSET_MASK` - Asset filename pattern (optional)
  - If empty or not specified, the application will show a release selection interface
  - If specified, only assets matching the pattern will be shown directly
  - Supports wildcard patterns like `*.zip`, `myapp-*`, etc.

The configuration file is searched in the following locations:
1. In the same directory as the executable file
2. In `~/.config/afetch.conf` (Linux/macOS)
3. In `%LOCALAPPDATA%\afetch\afetch.conf` (Windows)

### Setting up Configuration

#### Linux/macOS
```bash
# Create config directory if it doesn't exist
mkdir -p ~/.config

# Copy example configuration
cp afetch.conf.unix.example ~/.config/afetch.conf

# Edit the configuration file
nano ~/.config/afetch.conf
```

#### Windows
```cmd
# Create config directory if it doesn't exist
mkdir "%LOCALAPPDATA%\afetch"

# Copy example configuration
copy afetch.conf.windows.example "%LOCALAPPDATA%\afetch\afetch.conf"

# Edit the configuration file with notepad
notepad "%LOCALAPPDATA%\afetch\afetch.conf"
```

## Examples

### Direct Asset Filtering
```bash
# Show only .zip files from all releases
ASSET_MASK="*.zip"

# Show only files starting with "myapp-"
ASSET_MASK="myapp-*"

# Show only specific file pattern
ASSET_MASK="*-linux-x64.tar.gz"
```

### Release Selection Mode
```bash
# Leave ASSET_MASK empty or comment it out to enable release selection
# ASSET_MASK=""
```

This will show a list of all releases, allowing you to select a specific release and then choose from all its assets.
