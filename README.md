# asset-fetch (afetch)

[![Release](https://img.shields.io/github/v/release/wwwfyl/asset-fetch?style=flat-square)](https://github.com/wwwfyl/asset-fetch/releases)

Interactive CLI for tracking, installing, updating, and uninstalling tools distributed via GitHub release assets. A YAML config lists the apps you care about, and the TUI dashboard shows each tool's installed and latest versions side by side. No GitHub CLI required — direct API integration.

## Features

- **Multi-app dashboard:** one card per app showing local and latest versions; update or uninstall from the dashboard.
- **Interactive TUI:** browse releases and assets with a `bubbletea`-based keyboard interface.
- **Release Search:** type to filter releases by substring (case-insensitive).
- **Multi-Asset Downloads:** select and download multiple assets in a single batch.
- **Smart Filtering:** glob `asset_mask` per app (e.g. `*linux_x86_64.tar.gz`) drives auto-update and pre-filters assets.
- **URL-Based Fetching:** pass a GitHub releases URL directly to browse a specific repo or release tag.
- **Token-Based Auth:** global `github_token` plus optional per-app overrides for private repos and rate limits.
- **No Dependencies:** single self-contained binary.

## Installation

Download a pre-compiled binary from the [releases page](https://github.com/wwwfyl/asset-fetch/releases) or build from source.

### Build from Source

Requires Go 1.21+.

```bash
go build -o afetch
```

## Quick Start

### 1. Browse releases for a single repository

```bash
# Latest release of a repository
./afetch https://github.com/wwwfyl/asset-fetch/releases

# A specific release tag
./afetch https://github.com/wwwfyl/asset-fetch/releases/tag/v0.1.0
```

### 2. Set up the dashboard

Create an `afetch.yaml` (see [Configuration](#configuration) for paths) describing the tools you want to track. Minimal example:

```yaml
github_token: ""

apps:
  - name: afetch
    repo: wwwfyl/asset-fetch
    release_type: latest
    asset_mask: "*linux_x86_64.tar.gz"
    install_dir: ~/bin
    version:
      command: afetch --version
      regex: 'afetch version (\S+)'
    install:
      unpack:
        - run: tar xzf $ASSET_FILE -C $WORK_DIR
      steps:
        - name: Move binary
          run: mv $WORK_DIR/afetch $INSTALL_DIR/afetch
    uninstall:
      steps:
        - name: Remove binary
          run: rm -f $INSTALL_DIR/afetch
```

Run `./afetch` with no arguments to open the dashboard.

Full templates: [afetch.yaml.unix.example](afetch.yaml.unix.example) and [afetch.yaml.windows.example](afetch.yaml.windows.example).

## Usage

### Dashboard

Run `./afetch` without arguments. Each tile shows the app name, latest GitHub version, locally installed version, and a status line (`✓ up to date`, `● update available`, `✗ <error>`, etc.).

| Key               | Action                                                |
|-------------------|-------------------------------------------------------|
| `Tab` / `←` / `→` | Move focus between tiles                              |
| `u`               | Run the update pipeline for the focused tile          |
| `d`               | Uninstall the focused tile (prompts for confirmation) |
| `Enter`           | Open the GitHub releases list for the focused tile    |
| `q` / `Ctrl+C`    | Quit                                                  |

### Releases and assets views

Reached either via `Enter` from the dashboard or by passing a URL on the command line.

| Key                   | Action                                                                          |
|-----------------------|---------------------------------------------------------------------------------|
| `↑` / `↓` / `j` / `k` | Navigate the list                                                               |
| `/`                   | Activate search                                                                 |
| `Backspace`           | Remove last search character                                                    |
| `Esc`                 | Exit search and clear filter                                                    |
| `Enter` / `Space`     | Confirm release; toggle asset for download                                      |
| `q` / `Ctrl+C`        | Back to releases (from assets); back to dashboard; or cancel a running download |

## Configuration

`afetch` looks for `afetch.yaml` in the following locations, in order:

1. The directory containing the `afetch` binary (project-local override).
2. The platform user-config directory:
   - **Linux/macOS:** `~/.config/afetch.yaml`
   - **Windows:** `%LOCALAPPDATA%\afetch\afetch.yaml`

The first file found wins. Legacy `afetch.conf` files (the pre-YAML key=value format) are auto-migrated to `afetch.yaml` on first run; the original is preserved as `<path>.bak`.

### Top-level fields

| Field          | Description                                                                          |
|----------------|--------------------------------------------------------------------------------------|
| `github_token` | Optional. Global token used as a fallback when an app does not set its own.          |
| `apps`         | List of tracked applications (see below).                                            |

### Per-app fields

| Field          | Description                                                                          |
|----------------|--------------------------------------------------------------------------------------|
| `name`         | Display name shown on the dashboard tile.                                            |
| `repo`         | `owner/repo` on GitHub.                                                              |
| `release_type` | `latest` (default), `latest-stable` (skip prereleases), or `pre-release`.            |
| `asset_mask`   | Asset filename glob (e.g. `*windows_x86_64.zip`). Required for `u` (update).         |
| `install_dir`  | Destination directory. Defaults to `/usr/local/bin` for root, `$HOME/bin` otherwise. |
| `github_token` | Per-app override of the top-level token.                                             |
| `version`      | How to detect the locally installed version (see below).                             |
| `install`      | `unpack` and `steps` lists run during install/update.                                |
| `uninstall`    | `steps` list run on `d` from the dashboard.                                          |

### `version`

```yaml
version:
  command: afetch --version
  regex: 'afetch version (\S+)'
```

`command` is run; the first capture group of `regex` becomes the installed version compared against the GitHub release tag (with a leading `v` stripped).

### `install` and `uninstall` steps

Each step's `run` is executed through `sh -c` with the following environment variables:

| Variable       | Meaning                                              |
|----------------|------------------------------------------------------|
| `$ASSET_FILE`  | Path to the downloaded asset.                        |
| `$WORK_DIR`    | Temporary directory; safe to extract into.           |
| `$INSTALL_DIR` | Resolved `install_dir` for the app.                  |
| `$VERSION`     | Release tag of the selected release.                 |

On Windows, a POSIX shell must be available (Git Bash, MSYS2, or WSL); paths are written with forward slashes.

See [afetch.yaml.unix.example](afetch.yaml.unix.example) and [afetch.yaml.windows.example](afetch.yaml.windows.example) for complete, runnable templates.
