# LocalSync

Sync video playback between two MPV instances over a local network. Host picks a file, client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time.

## Install

> **You need [MPV](https://mpv.io) and [FFmpeg](https://ffmpeg.org) installed first.** See Step 1 below.

### Step 1: Install MPV & FFmpeg

<details>
<summary><b>macOS</b></summary>

```bash
# Install Homebrew if you don't have it
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

brew install mpv ffmpeg
```

</details>

<details>
<summary><b>Linux</b></summary>

```bash
# Debian / Ubuntu
sudo apt update && sudo apt install mpv ffmpeg

# Fedora
sudo dnf install mpv ffmpeg

# Arch
sudo pacman -S mpv ffmpeg

# Or via Homebrew on Linux
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install mpv ffmpeg
```

</details>

<details>
<summary><b>Windows</b></summary>

```powershell
# winget (preinstalled on Windows 11; on Windows 10, install "App Installer" from the Microsoft Store)
winget install mpv ffmpeg

# Chocolatey (https://chocolatey.org/install)
choco install mpv ffmpeg

# Scoop (https://scoop.sh)
scoop bucket add extras
scoop install mpv ffmpeg
```

</details>

### Step 2: Install LocalSync

```bash
# Unix (Linux / macOS)
curl -fsSL https://raw.githubusercontent.com/Azmekk/localsync/master/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/Azmekk/localsync/master/install.ps1 | iex
```

## Usage

```bash
# Host
./localsync -file /path/to/movie.mkv

# Client
./syncclient --server ws://<host-ip>:13771/ws
```

## Configuration

A `config.toml` file is created automatically next to the executable on first run. You can also specify a custom path with `-config`.

Default config location by OS:

| OS | Path |
|----|------|
| **Windows** | `%LOCALAPPDATA%\localsync\config.toml` |
| **macOS / Linux** | `/usr/local/bin/config.toml` |

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
