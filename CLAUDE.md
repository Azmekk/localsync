# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

LocalSync syncs video playback between two MPV instances over a local network. The host picks a file, the client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time via WebSocket. Requires MPV and FFmpeg (for transcoding).

## Build Commands

All binaries go in the `bin/` directory (gitignored).

```bash
# Build server (host binary)
go build -o bin/localsync .

# Build client binary
go build -o bin/syncclient ./cmd/syncclient

# Build with version tag
go build -ldflags "-X localsync/internal/update.Version=v1.0.0" -o bin/localsync .

# Cross-compile client for friends
GOOS=windows GOARCH=amd64 go build -o bin/syncclient.exe ./cmd/syncclient
GOOS=darwin  GOARCH=arm64 go build -o bin/syncclient-mac ./cmd/syncclient
```

There are no tests, no linter config, and no CI beyond the release workflow.

## Architecture

Two separate binaries sharing the `internal/update` package:

### Server (`localsync` — root package)
- **main.go**: CLI flags (`-file`, `-quality`, `-config`), HTTP server setup, launches host MPV + syncclient subprocess. Routes: `/stream` (video) and `/ws` (sync WebSocket).
- **hub.go**: WebSocket broadcast hub holding `SessionState` (file, quality, pos, paused). Sends `init` event to newly connected clients. Broadcasts sync messages between peers (never echoes back to sender).
- **stream.go**: HTTP handler serving video. `source` quality = passthrough via `http.ServeContent` (supports Range/seek). Other qualities = FFmpeg transcoding piped to response as MPEG-TS.
- **config.go**: TOML config loader (`config.toml`) for port and quality presets.

### Client (`syncclient` — `cmd/syncclient/`)
- **main.go**: Connects to host WS, receives `init` message, launches MPV, bridges MPV IPC <-> WebSocket for bidirectional sync. Uses atomic `applyingCount` to prevent echo loops.
- **ipc_unix.go / ipc_windows.go**: Platform-specific MPV IPC connection (Unix socket vs Windows named pipe). Windows uses `github.com/Microsoft/go-winio` (`DialPipe`) for overlapped I/O — do NOT replace with `os.OpenFile`, which opens the pipe without `FILE_FLAG_OVERLAPPED` and causes `Write()` to block while `Read()` is pending.

### Shared (`internal/update/`)
- Version management, GitHub release checking, and self-update functionality.

## Sync Protocol

JSON messages over WebSocket:
- `{"event":"init", "file":"...", "quality":"...", "pos":0, "paused":false}` — server to client on connect
- `{"event":"pause", "state":true/false, "pos":142.3, "source":"host"}` — bidirectional
- `{"event":"seek", "pos":300.0, "source":"client"}` — bidirectional
- `{"event":"sync", "pos":142.3, "source":"host"}` — periodic drift correction (every 3s)

## Key Design Decisions

- No authentication — assumes Tailscale/VPN handles trust
- Max 1 remote viewer at a time
- Host decides file and quality at startup; clients never browse files
- Seek debounce: only sends seek if position changes >0.5s with 100ms cooldown
- Drift correction: syncclient auto-seeks if remote position differs by >1s
- Version is injected at build time via ldflags; defaults to `"dev"`
- Windows named pipes require overlapped I/O for concurrent read/write — synchronous handles serialize all I/O, causing writes to block until a pending read completes (catastrophic when MPV is paused and produces no IPC output)
