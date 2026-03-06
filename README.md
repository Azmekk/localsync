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

[transcode]
video_codec = "libx264"
extra_args = ["-preset", "ultrafast", "-tune", "zerolatency"]
audio_codec = "aac"
audio_bitrate = "128k"
subtitles = true
realtime = true
format = "matroska"

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
| `transcode.video_codec` | `libx264` | FFmpeg video codec (e.g. `hevc_nvenc`, `h264_vaapi`) |
| `transcode.extra_args` | `["-preset", "ultrafast", "-tune", "zerolatency"]` | Additional encoder-specific FFmpeg flags |
| `transcode.audio_codec` | `aac` | Audio codec — set to `copy` to passthrough original audio |
| `transcode.audio_bitrate` | `128k` | Audio bitrate (ignored when `audio_codec = "copy"`) |
| `transcode.subtitles` | `true` | Pass through subtitle streams in transcoded output |
| `transcode.realtime` | `true` | Enable `-re` (realtime throttling) — disable for hardware encoders that can buffer ahead |
| `transcode.format` | `matroska` | Container format (`matroska`, `mpegts`, `mp4`, etc.) |

### Hardware encoding examples

<details>
<summary>NVIDIA RTX 2000 series (Turing)</summary>

Turing NVENC is solid for H.264; it supports HEVC encode too, but H.264 is more broadly compatible. No AV1 support.

```toml
[transcode]
video_codec = "h264_nvenc"
extra_args = ["-preset", "p4", "-tune", "ll", "-rc", "vbr"]
audio_codec = "copy"
subtitles = true
realtime = false
format = "matroska"
```
</details>

<details>
<summary>NVIDIA RTX 3000 series (Ampere)</summary>

Ampere has improved NVENC with better B-frame support. HEVC is a good default here.

```toml
[transcode]
video_codec = "hevc_nvenc"
extra_args = ["-preset", "p5", "-tune", "ll", "-rc", "vbr"]
audio_codec = "copy"
subtitles = true
realtime = false
format = "matroska"
```
</details>

<details>
<summary>NVIDIA RTX 4000 series (Ada Lovelace)</summary>

4000 series introduced AV1 hardware encode — best quality/bitrate ratio. Fall back to `hevc_nvenc` if the client doesn't support AV1.

```toml
[transcode]
video_codec = "av1_nvenc"
extra_args = ["-preset", "p4", "-rc", "vbr"]
audio_codec = "copy"
subtitles = true
realtime = false
format = "matroska"
```
</details>

<details>
<summary>AMD Radeon RX 6000 series (RDNA 2)</summary>

AMF encoding via `h264_amf` or `hevc_amf`. No AV1 encode on RDNA 2.

```toml
[transcode]
video_codec = "hevc_amf"
extra_args = ["-quality", "balanced", "-rc", "vbr_latency"]
audio_codec = "copy"
subtitles = true
realtime = false
format = "matroska"
```
</details>

<details>
<summary>AMD Radeon RX 7000 series (RDNA 3)</summary>

RDNA 3 added AV1 hardware encode via `av1_amf`. Fall back to `hevc_amf` if needed.

```toml
[transcode]
video_codec = "av1_amf"
extra_args = ["-quality", "balanced", "-rc", "vbr_latency"]
audio_codec = "copy"
subtitles = true
realtime = false
format = "matroska"
```
</details>

## Build (from source)

Requires Go 1.21+.

```bash
go build -o localsync .
go build -o syncclient ./cmd/syncclient
```
