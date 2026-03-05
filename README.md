# LocalSync

Sync video playback between two MPV instances over a local network. Host picks a file, client connects and gets MPV launched automatically. Pause/seek/resume syncs in real time.

Requires Go 1.21+, MPV, and FFmpeg (for transcoding).

## Build

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
