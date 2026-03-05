# LocalSync

Sync video playback between two MPV instances over a local network. Host picks a file, client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time.

Requires MPV and FFmpeg (for transcoding). See [Prerequisites](#prerequisites) for install instructions.

## Prerequisites

### macOS

```bash
# Install Homebrew if you don't have it
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

brew install mpv ffmpeg
```

### Linux

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

### Windows

```powershell
# Install Homebrew (requires WSL — or use the native options below)
# Native option 1: winget (preinstalled on Windows 11; on Windows 10, install "App Installer" from the Microsoft Store)
winget install mpv ffmpeg

# Native option 2: Chocolatey (https://chocolatey.org/install)
choco install mpv ffmpeg

# Native option 3: Scoop (https://scoop.sh)
scoop bucket add extras
scoop install mpv ffmpeg
```

## Install

```bash
# Unix (Linux / macOS)
curl -fsSL https://raw.githubusercontent.com/Azmekk/localsync/master/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/Azmekk/localsync/master/install.ps1 | iex
```

## Build (from source)

Requires Go 1.21+.

```bash
go build -o localsync .
go build -o syncclient ./cmd/syncclient
```

## Usage

```bash
# Host
./localsync -file /path/to/movie.mkv

# Client
./syncclient --server ws://<host-ip>:8080/ws
```
