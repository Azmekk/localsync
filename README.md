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
localsync --file /path/to/movie.mkv
```

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | *(required)* | Path to the video file to play |
| `--quality`, `-q` | `source` | Quality preset to use (`source`, `1080p`, `720p`, `480p`, or any custom preset from config) |
| `--config`, `-c` | OS config dir | Path to `config.toml` |
| `--create-media-folder` | `false` | Create `.localsync/` variant folder next to the video and exit |
| `--version`, `-v` | | Print version and exit |
| `--update`, `-u` | | Update localsync and syncclient to the latest release |

**Client:**

```bash
syncclient --server ws://<host-ip>:<port>/ws
```

| Flag | Default | Description |
|------|---------|-------------|
| `--server`, `-s` | *(required)* | WebSocket URL of the host (`ws://<host-ip>:<port>/ws`) |
| `--variant`, `-V` | | Variant name to play (skips interactive menu) |
| `--ipc` | `/tmp/mpvsync` (Unix) or `\\.\pipe\mpvsync` (Windows) | Path for the MPV IPC socket |
| `--name`, `-n` | `client` | Identifier sent with sync events (`host` or `client`) |
| `--no-launch` | `false` | Skip launching MPV (used internally by the host) |
| `--version`, `-v` | | Print version and exit |
| `--update`, `-u` | | Update localsync and syncclient to the latest release |

When variants are available, the client shows an interactive TUI selector:

```
> source    movie.mkv
  720p_low  890 MB
  720p_high 2.1 GB

  [j/k] navigate  [enter] select  [esc] source
```

## Configuration

A `config.toml` file is created automatically in your OS config directory on first run. You can also specify a custom path with `--config`.

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

### Pre-Compressed Variants

Instead of live transcoding, you can pre-encode video at different quality levels and let clients choose which version to stream. This gives better quality (multi-pass encoding, no realtime pressure) and zero CPU usage during playback.

**Create the variant folder:**

```bash
localsync --file /path/to/movie.mkv --create-media-folder
```

This creates a `.localsync/` directory next to your video file. Place pre-compressed versions in it with any naming convention:

```
/path/to/
  movie.mkv
  .localsync/
    720p_low.mkv
    720p_high.mkv
    480p.mp4
```

When a client connects, they'll see an interactive menu listing all available variants along with the source file. Clients can also skip the menu with `--variant`:

```bash
syncclient --server ws://host:13771/ws --variant 720p_low
```

Variant files are served via passthrough (no transcoding) with full seek support.

### Batchcompress

`batchcompress` is a sibling CLI that fills `.localsync/` folders by transcoding videos in bulk. Point it at a folder, pick which files to encode, leave it running overnight. Pause (`p`) genuinely freezes the ffmpeg process tree on Windows via `NtSuspendProcess`; skip (`s`) and quit (`q`) discard partial output. Logs go to `batchcompress.log` next to your config.

```bash
batchcompress --input /path/to/folder            # interactive picker
batchcompress --input /path/to/folder --all -p 720p   # encode everything
```

By default `batchcompress` builds an ffmpeg command from `[batchcompress]` globals + a chosen `[[batchcompress.preset]]`. **If you want a fully predictable command, set `command` to an explicit ffmpeg argv** — when this is set, `video_codec`, `extra_args`, `audio_codec`, `audio_bitrate`, `subtitles`, and presets are all ignored. `{input}` is substituted with the source path; the output path is appended automatically. `-y` and `-progress pipe:1 -nostats` are auto-prepended only if absent so the dashboard still gets progress.

```toml
[batchcompress]
command = [
    "-i", "{input}",
    "-map", "0:v", "-map", "0:a", "-map", "0:s?",
    "-c:v", "libsvtav1",
    "-preset", "6",
    "-crf", "31",
    "-pix_fmt", "yuv420p10le",
    "-c:a", "libopus",
    "-b:a", "96k",
    "-ac", "2",
    "-c:s", "copy",
    "-f", "matroska",
]
```

Output extension is derived from `-f <fmt>` in your command (e.g. `-f mp4` → `.mp4`). Files are written as `<source-stem>_<preset>.<ext>` inside the `.localsync/` folder next to each source.

### Bandwidth-Aware Pause

When a client's buffer runs dry due to insufficient bandwidth, the host is automatically paused and notified with the client's download speed. The log message includes a recommended bitrate so you can adjust the quality preset. Once the client's buffer recovers, playback resumes automatically.

Clients buffer up to 100MB of content ahead to absorb short bandwidth dips.

### Terminal UI

Both host and client feature a live-updating terminal UI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

**Host TUI** shows a header with server info, a live client stats table (speed, buffer time, buffer size, position), and a toggleable log panel capturing MPV output and sync events.

**Client TUI** shows playback status (position, buffer, speed) and a toggleable log panel with MPV output.

**Keybindings (both):**

| Key | Action |
|-----|--------|
| `L` | Toggle log panel |
| `j`/`k` | Scroll logs (when panel open) |
| `Q` / `Ctrl+C` | Quit |

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

Requires Go 1.22+.

```bash
go build -o bin/localsync ./localsync
go build -o bin/syncclient ./syncclient
```
