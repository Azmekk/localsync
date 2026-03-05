# LocalSync

Sync video playback between two MPV instances over a local network. Host picks a file, client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time.

Requires MPV and FFmpeg (for transcoding).

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
