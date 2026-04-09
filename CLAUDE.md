# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

LocalSync syncs video playback between two MPV instances over a local network. The host picks a file, the client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time via WebSocket. Requires MPV and FFmpeg (for transcoding).

## Build Commands

All binaries go in the `bin/` directory (gitignored). **Always build with `-o bin/`**.

```bash
# Build server (host binary)
go build -o bin/localsync ./localsync

# Build client binary
go build -o bin/syncclient ./syncclient

# Build with version tag
go build -ldflags "-X localsync/shared/update.Version=v1.0.0" -o bin/localsync ./localsync

# Cross-compile client for friends
GOOS=windows GOARCH=amd64 go build -o bin/syncclient.exe ./syncclient
GOOS=darwin  GOARCH=arm64 go build -o bin/syncclient-mac ./syncclient
```

There are no tests, no linter config, and no CI beyond the release workflow.

## Architecture

Two separate binaries sharing the `shared/update` package. Uses **Cobra** for CLI, **Chi** for HTTP routing, and **tview** for terminal UI.

### Server (`localsync/`)
- **main.go**: Cobra root command, Chi router wiring, launches host MPV + syncclient subprocess. Routes: `/stream` (video), `/ws` (sync WebSocket), `/variant/{name}` (pre-compressed variants).
- **config.go**: TOML config loader (`config.toml`) for port, quality presets (`QualityPreset` with name, bitrate, resolution, passthrough), and `TranscodeConfig` (video/audio codec, extra encoder args, subtitle passthrough, realtime throttling, container format).
- **internal/model/types.go**: Shared types — `SessionState`, `Variant`, `ClientInfo`, `SyncMessage`, `StatsMessage`.
- **internal/handler/ws.go**: WebSocket upgrade, message routing, stats intake. Filters messages: forwards host events, ready, buffering; stores stats without broadcasting.
- **internal/handler/stream.go**: `/stream` handler (passthrough via `http.ServeContent` or FFmpeg transcode). `/variant/{name}` handler serving pre-compressed files from `.localsync/` folder.
- **internal/service/hub.go**: WebSocket broadcast hub holding `SessionState`. Sends `init` event to newly connected clients. Broadcasts sync messages between peers (never echoes back to sender). Tracks per-client stats.
- **internal/service/variants.go**: Scans `.localsync/` folder next to video for pre-compressed variant files. `CreateMediaFolder` creates the folder structure.
- **internal/tui/host.go**: tview alt-screen TUI — header, live client stats table, toggleable log panel. Replaces log-based stats display.
- **internal/tui/logwriter.go**: `io.Writer` adapter routing `log.Printf` and MPV output into the TUI log panel.
- **internal/tui/styles.go**: tcell color definitions shared across TUI components.

### Client (`syncclient/`)
- **main.go**: Cobra CLI. Connects to host WS, receives `init` message with available variants, launches MPV, bridges MPV IPC <-> WebSocket for bidirectional sync. Sends periodic stats reports (2s) including buffer bytes. Uses atomic `applyingCount` to prevent echo loops.
- **tui.go**: tview TUI components — variant selector (arrow-key list picker), playback monitor (live stats + toggleable log panel). MPV output piped to log panel.
- **ipc_unix.go / ipc_windows.go**: Platform-specific MPV IPC connection (Unix socket vs Windows named pipe). Windows uses `github.com/Microsoft/go-winio` (`DialPipe`) for overlapped I/O — do NOT replace with `os.OpenFile`, which opens the pipe without `FILE_FLAG_OVERLAPPED` and causes `Write()` to block while `Read()` is pending.

### Shared (`shared/update/`)
- Version management, GitHub release checking, and self-update functionality.

## Variant Folder System

Pre-compressed video variants are stored in a `.localsync/` folder next to the source video file:
- `localsync --file movie.mkv --create-media-folder` creates the folder
- Place variant files with any name (e.g., `720p_low.mkv`, `480p.mp4`)
- Clients see an interactive menu to choose source or any variant
- Variants are served via passthrough (seekable, Range-request support)

## Sync Protocol

JSON messages over WebSocket:
- `{"event":"init", "file":"...", "quality":"...", "pos":0, "paused":false, "variants":[...]}` — server to client on connect
- `{"event":"pause", "state":true/false, "pos":142.3, "source":"host"}` — bidirectional
- `{"event":"seek", "pos":300.0, "source":"client"}` — bidirectional
- `{"event":"sync", "pos":142.3, "source":"host"}` — periodic drift correction (every 3s)
- `{"event":"buffering", "state":true, "speed":2500.0, "source":"client"}` — client bandwidth issue; host pauses
- `{"event":"stats", "source":"client", "speed_kbps":2500, "buffer_secs":3.2, "pos":142.3}` — periodic client stats (every 2s)

## Key Design Decisions

- No authentication — assumes Tailscale/VPN handles trust
- Max 1 remote viewer at a time
- Host decides file and quality at startup; clients choose variant interactively
- Quality presets support both bitrate and resolution (e.g., `1080p` = 8000k at 1920x1080)
- Quality presets support per-quality `extra_args` overriding global `[transcode].extra_args`
- Seek debounce: only sends seek if position changes >0.5s with 100ms cooldown
- Drift correction: syncclient auto-seeks if remote position differs by >1s
- Client buffer: MPV launched with `--demuxer-max-bytes=100MiB --cache=yes`
- Bandwidth-aware pause: client observes `paused-for-cache` and `cache-speed` MPV properties; sends `buffering` event with measured speed; host pauses and logs recommended bitrate
- Client stats: periodic reports of download speed, buffer duration, and playback position; host logs formatted summary every 3s
- Version is injected at build time via ldflags; defaults to `"dev"`
- Windows named pipes require overlapped I/O for concurrent read/write — synchronous handles serialize all I/O, causing writes to block until a pending read completes (catastrophic when MPV is paused and produces no IPC output)

## Dependencies

- `github.com/spf13/cobra` — CLI framework (both binaries)
- `github.com/go-chi/chi/v5` — HTTP router (server)
- `github.com/gorilla/websocket` — WebSocket protocol
- `github.com/BurntSushi/toml` — config file parsing
- `github.com/Microsoft/go-winio` — Windows named pipe I/O
- `github.com/rivo/tview` — Terminal UI framework
- `github.com/gdamore/tcell/v2` — Terminal cell library (tview dependency)
