# asset-fetch

Interactive CLI tool for downloading GitHub release assets from public and private repositories with fuzzy search. No GitHub CLI required - direct API integration. Features a `bubbletea` TUI, smart filtering, and token-based authentication.

## Features

-   **Interactive TUI:** Navigate releases and assets with a clean, keyboard-driven interface.
-   **Fuzzy Search:** Quickly find the release you're looking for.
-   **Multi-Asset Downloads:** Select and download multiple assets in a single batch operation.
-   **Smart Filtering:** Use glob patterns (`*.zip`, `app-*-amd64`, etc.) to filter assets directly.
-   **URL-Based Fetching:** Pass a GitHub releases URL directly to fetch assets from a specific repository or release.
-   **Configuration File:** Set your GitHub token, default repository, and asset masks in `afetch.conf`.
-   **Progress Tracking:** Monitor download progress with a clean, tabular view.
-   **No Dependencies:** Single, self-contained binary. No need for the GitHub CLI.

## Installation

You can download a pre-compiled binary from the [releases page](https://github.com/wwwfyl/asset-fetch/releases) or build from source.

### Build from Source

You will need Go 1.21+ to build from source.

```bash
go build -o afetch
```

## Quick Start

1.  **Run with a URL:** The fastest way to use `asset-fetch` is by passing a GitHub releases URL.

    ```bash
    # Fetch from the latest release of a repository
    ./afetch https://github.com/charmbracelet/bubbletea/releases

    # Fetch from a specific release tag
    ./afetch https://github.com/charmbracelet/bubbletea/releases/tag/v0.25.0
    ```

2.  **Use the Configuration File:** For repositories you access frequently, create an `afetch.conf` file.

    ```bash
    # Create a config file (see Configuration section for paths)
    cat > ~/.config/afetch/afetch.conf <<EOL
    GITHUB_TOKEN="your_github_token"
    REPO_OWNER="owner"
    REPO_NAME="repo"
    ASSET_MASK="*.zip"
    EOL

    # Run the tool
    ./afetch
    ```

## Usage

The tool operates in two main modes: release selection and asset selection.

### 1. Release Selection

If you run `afetch` with a repository URL or without a specific `ASSET_MASK`, you will be prompted to select a release from a list.

-   **`Up/Down`** or **`j/k`**: Navigate the list.
-   **`Enter`**: Select a release and proceed to asset selection.
-   **`q`** or **`Ctrl+C`**: Exit.

### 2. Asset Selection

Once a release is selected, you can choose which assets to download.

-   **`Up/Down`** or **`j/k`**: Navigate the asset list.
-   **`Space`**: Toggle selection for an asset (for multi-asset downloads).
-   **`Enter`**: Start the download for the selected asset(s).
-   **`q`** or **`Ctrl+C`**: Go back or exit.

### 3. Download Confirmation

Before downloading, you'll be asked to confirm.

-   **`y`**: Confirm and start the download.
-   **`n`** or **`Esc`**: Cancel.

## Configuration

`asset-fetch` can be configured via an `afetch.conf` file. The file is searched for in these locations:

1.  The same directory as the executable.
2.  `~/.config/afetch/afetch.conf` (Linux/macOS)
3.  `%LOCALAPPDATA%\afetch\afetch.conf` (Windows)

### Configuration Options

| Variable       | Description                                                                                                                             |
|----------------|-----------------------------------------------------------------------------------------------------------------------------------------|
| `GITHUB_TOKEN` | Your GitHub Personal Access Token. Required for private repositories and to avoid rate limiting.                                        |
| `REPO_OWNER`   | The owner of the repository (e.g., `wwwfyl`).                                                                                           |
| `REPO_NAME`    | The name of the repository (e.g., `asset-fetch`).                                                                                       |
| `ASSET_MASK`   | An optional glob pattern to filter assets (e.g., `*.zip`). If set, the tool skips release selection and shows matching assets directly. |

### Example `afetch.conf`

```ini
# GitHub API configuration
GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxx"

# Default repository
REPO_OWNER="wwwfyl"
REPO_NAME="asset-fetch"

# Optional: Filter for assets matching a pattern.
# If this is empty or commented out, you will see the release list first.
ASSET_MASK="*_linux_x86_64.tar.gz"

```

## Examples

### Download from a URL

The most direct way to use the tool.

```bash
# Show releases for afetch
./afetch https://github.com/wwwfyl/asset-fetch/releases

# Go directly to a specific afetch release
./afetch https://github.com/wwwfyl/asset-fetch/releases/tag/v0.0.1
```

### Filter Assets with `ASSET_MASK`

Set `ASSET_MASK` in your `afetch.conf` to skip release selection.

```ini
# In afetch.conf
REPO_OWNER="wwwfyl"
REPO_NAME="asset-fetch"
ASSET_MASK="*_linux_x86_64.tar.gz"
```

Running `./afetch` will now immediately show all assets from `wwwfyl/asset-fetch` that match the `*_linux_x86_64.tar.gz` pattern, grouped by release.

### Manual Release Selection

Leave `ASSET_MASK` empty in `afetch.conf` to browse releases interactively.

```ini
# In afetch.conf
REPO_OWNER="wwwfyl"
REPO_NAME="asset-fetch"
# ASSET_MASK is not set
```

Running `./afetch` will first show a list of releases for `wwwfyl/asset-fetch`. After you select one, it will show all assets for that release.
