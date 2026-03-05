# LocalSync

Sync video playback between two MPV instances over a local network. Host picks a file, client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time.

## Install

> **You need [MPV](https://mpv.io) and [FFmpeg](https://ffmpeg.org) installed first.** See Step 1 below.

### Step 1: Install MPV & FFmpeg

**Windows** — via **winget** (preinstalled on Windows 11; on Windows 10, install "App Installer" from the Microsoft Store):

```powershell
winget install mpv ffmpeg
```

Alternatively, you can use [Chocolatey](https://chocolatey.org/install) (separate package manager):

```powershell
choco install mpv ffmpeg
```

Or [Scoop](https://scoop.sh) (separate package manager):

```powershell
scoop bucket add extras
scoop install mpv ffmpeg
```

**macOS** — via [Homebrew](https://brew.sh):

```bash
brew install mpv ffmpeg
```

If you don't have Homebrew, install it first:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Alternatively, you can install via [MacPorts](https://www.macports.org) (separate package manager):

```bash
sudo port install mpv ffmpeg
```

**Linux:**

```bash
# Debian / Ubuntu
sudo apt update && sudo apt install mpv ffmpeg

# Fedora
sudo dnf install mpv ffmpeg

# Arch
sudo pacman -S mpv ffmpeg
```

### Step 2: Install LocalSync

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/Azmekk/localsync/master/install.ps1 | iex
```

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/Azmekk/localsync/master/install.sh | sh
```

## Usage

```bash
# Host
./localsync -file /path/to/movie.mkv

# Client
./syncclient --server ws://<host-ip>:<port>/ws
```

## Configuration

A `config.toml` file is created automatically in your OS config directory on first run. You can also specify a custom path with `-config`.

| OS | Default path |
|----|------|
| **Windows** | `%APPDATA%\localsync\config.toml` |
| **macOS** | `~/Library/Application Support/localsync/config.toml` |
| **Linux** | `~/.config/localsync/config.toml` |

```toml
port = 13771

# Maximum number of remote clients allowed at once.
# Default is 1. Set to 0 for unlimited.
max_clients = 1

[quality]
source = "passthrough"
high = "8000k"
mid = "3000k"
low = "1000k"
```

| Key | Default | Description |
|-----|---------|-------------|
| `port` | `13771` | HTTP/WebSocket server port |
| `max_clients` | `1` | Max simultaneous remote viewers (`0` = unlimited) |
| `quality.*` | see above | Named quality presets — `source` streams the file directly, others transcode via FFmpeg at the given bitrate |

## Build (from source)

Requires Go 1.21+.

```bash
go build -o localsync .
go build -o syncclient ./cmd/syncclient
```
