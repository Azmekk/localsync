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

**Host:**

```bash
./localsync -file /path/to/movie.mkv
```

| Flag | Default | Description |
|------|---------|-------------|
| `-file` | *(required)* | Path to the video file to play |
| `-quality` | `source` | Quality preset to use (`source`, `1080p`, `720p`, `480p`, or any custom preset from config) |
| `-config` | OS config dir | Path to `config.toml` |
| `-precreate-hls` | `false` | Generate HLS segments for the given quality and exit |
| `-version` | | Print version and exit |
| `-update` | | Update localsync and syncclient to the latest release |

**Client:**

```bash
./syncclient --server ws://<host-ip>:<port>/ws
```

| Flag | Default | Description |
|------|---------|-------------|
| `--server` | *(required)* | WebSocket URL of the host (`ws://<host-ip>:<port>/ws`) |
| `--quality` | *(adaptive)* | Quality to use in HLS mode (`adaptive`, or a preset name like `720p`) |
| `--ipc` | `/tmp/mpvsync` (Unix) or `\\.\pipe\mpvsync` (Windows) | Path for the MPV IPC socket |
| `--name` | `client` | Identifier sent with sync events (`host` or `client`) |
| `--no-launch` | `false` | Skip launching MPV (used internally by the host) |
| `--version` | | Print version and exit |
| `--update` | | Update localsync and syncclient to the latest release |

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

[[quality]]
name = "source"
passthrough = true

[[quality]]
name = "1080p"
bitrate = "8000k"
resolution = "1920x1080"

[[quality]]
name = "720p"
bitrate = "3000k"
resolution = "1280x720"

[[quality]]
name = "480p"
bitrate = "1000k"
resolution = "854x480"

[hls]
enabled = false
segment_duration = 4
auto_generate = false
qualities = ["1080p", "720p", "480p"]
segment_type = "mpegts"
```

| Key | Default | Description |
|-----|---------|-------------|
| `port` | `13771` | HTTP/WebSocket server port |
| `max_clients` | `1` | Max simultaneous remote viewers (`0` = unlimited) |
| `quality[].name` | | Preset name (e.g., `source`, `1080p`, `720p`) |
| `quality[].bitrate` | | FFmpeg video bitrate (e.g., `8000k`) — required unless `passthrough = true` |
| `quality[].resolution` | | Target resolution (e.g., `1920x1080`) — optional, omit to keep source resolution |
| `quality[].passthrough` | `false` | Serve source file directly without transcoding |
| `quality[].extra_args` | | Per-quality FFmpeg args — overrides `transcode.extra_args` for this preset |
| `transcode.video_codec` | `libx264` | FFmpeg video codec (e.g. `hevc_nvenc`, `h264_vaapi`) |
| `transcode.extra_args` | `["-preset", "ultrafast", "-tune", "zerolatency"]` | Default encoder-specific FFmpeg flags (overridden by per-quality `extra_args`) |
| `transcode.audio_codec` | `aac` | Audio codec — set to `copy` to passthrough original audio |
| `transcode.audio_bitrate` | `128k` | Audio bitrate (ignored when `audio_codec = "copy"`) |
| `transcode.subtitles` | `true` | Pass through subtitle streams in transcoded output |
| `transcode.realtime` | `true` | Enable `-re` (realtime throttling) — disable for hardware encoders that can buffer ahead |
| `transcode.format` | `matroska` | Container format (`matroska`, `mpegts`, `mp4`, etc.) |
| `hls.enabled` | `false` | Enable adaptive HLS mode |
| `hls.segment_duration` | `4` | Duration of each HLS segment in seconds |
| `hls.auto_generate` | `false` | Automatically generate HLS on server startup |
| `hls.qualities` | `[]` | Quality presets to encode (empty = all non-passthrough) |
| `hls.segment_type` | `mpegts` | Segment format: `mpegts` (`.ts`) or `fmp4` (`.m4s`, required for AV1) |

### Adaptive HLS

LocalSync can pre-create a multi-variant HLS stream with a single FFmpeg command that encodes all quality variants simultaneously. This produces a `master.m3u8` playlist that MPV uses for adaptive bitrate switching.

**One-shot generation** (generates all configured variants):

```bash
./localsync -file /path/to/movie.mkv -precreate-hls
```

**Auto-generation on startup** — set `hls.enabled = true` and `hls.auto_generate = true` in config. The server starts immediately while HLS generates in the background; clients fall back to live transcoding until generation completes.

**Client quality selection** — when the server is in HLS mode, clients use adaptive streaming by default. Pin a specific quality with `--quality`:

```bash
./syncclient --server ws://host:13771/ws --quality 720p
```

HLS segments are stored in `.localsync-hls/` next to the video file. HLS streams are natively seekable in MPV. For AV1 codecs, set `segment_type = "fmp4"` to use fMP4 segments (`.m4s`) instead of MPEG-TS.

### Bandwidth-Aware Pause

When a client's buffer runs dry due to insufficient bandwidth, the host is automatically paused and notified with the client's download speed. The log message includes a recommended bitrate so you can adjust the quality preset. Once the client's buffer recovers, playback resumes automatically.

Clients buffer up to 100MB of content ahead to absorb short bandwidth dips.

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
go build -o localsync ./localsync
go build -o syncclient ./syncclient
```
