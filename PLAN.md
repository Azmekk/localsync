# LocalSync — Build Plan for Claude CLI

## Project Goal
Build a local video sync-streaming server in Go with an MPV Lua client plugin.
One friend connects over Tailscale (or any VPN). Both use MPV. Either side can
pause, seek, or resume and it propagates to the other in real time. The host
decides what file to play and at what quality when starting the server. The client
connects and is automatically handed everything they need — no file selection on
their end. The host serves the video with on-the-fly FFmpeg transcoding at 3
selectable quality tiers, or raw passthrough if connection quality allows.

---

## Constraints & Assumptions
- Go 1.21+
- FFmpeg must be installed and on PATH
- MPV must be installed on all machines
- Max 1 remote viewer at a time
- No UI — CLI flags + config file only
- Host specifies the file and quality at server startup — clients receive this automatically on connect
- Cross-platform syncclient binary (macOS + Windows clients); no Lua plugin needed on client
- Single self-contained binary for the server
- Networking handled externally (Tailscale / VPN) — server binds on 0.0.0.0

---

## Final File Tree

```
localsync/
├── main.go
├── config.go
├── hub.go
├── stream.go
├── go.mod                    (module: localsync)
├── config.toml               (example config, committed)
└── cmd/
    └── syncclient/
        └── main.go
```

No `client/sync.lua` — the Lua plugin is replaced by the `syncclient` binary
which launches MPV automatically after receiving the `init` event.

---

## Dependencies

```
github.com/gorilla/websocket v1.5.1
github.com/BurntSushi/toml   v1.3.2
```

No other external deps. Use only stdlib beyond these two.

---

## config.toml (example, ship this in repo)

```toml
port = 8080

[quality]
source = "passthrough"   # no transcoding — file served as-is via direct byte stream
high   = "8000k"         # passed to ffmpeg -b:v
mid    = "3000k"
low    = "1000k"
```

`media_dir` is gone. The host passes the full file path directly as a CLI flag at
startup. The config only holds port and quality presets, which rarely change.

---

## config.go

Define a `Config` struct and a `LoadConfig(path string) (Config, error)` function
that parses the TOML file using BurntSushi/toml. Struct fields:

```go
type Config struct {
    Port    int               `toml:"port"`
    Quality map[string]string `toml:"quality"`
}
```

Validate that `Quality` map is not nil; fill defaults for any missing keys
(`source -> "passthrough"`, `high -> "8000k"`, `mid -> "3000k"`, `low -> "1000k"`).

---

## hub.go

Implements a simple broadcast WebSocket hub for exactly 2 clients (host + viewer).
The hub also holds the current **session state** so a newly connected client
receives it immediately without waiting for the next event.

```go
type SessionState struct {
    File    string  `json:"file"`
    Quality string  `json:"quality"`
    Pos     float64 `json:"pos"`
    Paused  bool    `json:"paused"`
}

type Hub struct {
    clients    map[*websocket.Conn]bool
    broadcast  chan []byte
    register   chan *websocket.Conn
    unregister chan *websocket.Conn
    state      SessionState
    mu         sync.Mutex
}

func NewHub(initialState SessionState) *Hub
func (h *Hub) Run()
func (h *Hub) Register(c *websocket.Conn)
func (h *Hub) Unregister(c *websocket.Conn)
func (h *Hub) Broadcast(sender *websocket.Conn, msg []byte)
func (h *Hub) UpdateState(msg []byte)
```

`Register` must immediately write an `init` event to the newly connected client
using the current `h.state`:
```json
{ "event": "init", "file": "movie.mkv", "quality": "source", "pos": 142.3, "paused": false }
```

`UpdateState` is called by `SyncHandler` on every inbound message before
broadcasting. It parses the event type and updates only the relevant fields
(`seek` -> `Pos`, `pause` -> `Paused` + `Pos`).

`Broadcast` must NOT echo back to the sender. Identify sender by pointer comparison.
`Run()` should be launched as `go hub.Run()` in main.

---

## stream.go

### Handler signature
```go
func StreamHandler(cfg Config, filePath string) http.HandlerFunc
```

`filePath` is the absolute path to the file the host specified at startup.
It is set once and never changes for the lifetime of the server process.

### Behaviour
1. Parse query param: `quality` (one of `source`, `high`, `mid`, `low`; default `source`).
   The `file` param is **gone** — the served file is fixed at startup.
2. The full path is `filePath` directly. No sanitisation needed (set by the host at startup, not user input).
3. File existence is validated at startup in `main.go`; the handler can assume it exists.
4. Look up bitrate from `cfg.Quality[quality]`. If the value is `"passthrough"`
   or quality is `"source"`, skip FFmpeg entirely and serve the file directly:
   - Detect MIME type from extension (`.mkv -> video/x-matroska`, `.mp4 -> video/mp4`, etc.; fallback to `application/octet-stream`).
   - Set `Content-Type` accordingly and `Cache-Control: no-cache`.
   - Use `http.ServeContent` with the opened `*os.File` — this handles Range
     requests automatically, meaning MPV can seek freely without re-downloading.
   - Return early; do not spawn FFmpeg at all.
5. For all non-passthrough qualities, build FFmpeg args:
```go
args := []string{
    "-re",
    "-i", filePath,
    "-b:v", bitrate,
    "-c:v", "libx264",
    "-preset", "ultrafast",
    "-tune", "zerolatency",
    "-c:a", "aac",
    "-b:a", "128k",
    "-f", "mpegts",
    "pipe:1",
}
```
6. Set response header `Content-Type: video/mp2t` and `Cache-Control: no-cache`.
7. Pipe `cmd.Stdout` directly to `w` using `io.Copy`.
8. On client disconnect (`r.Context().Done()`), kill the FFmpeg process.
9. Handle the case where FFmpeg is not on PATH: return 500 with a clear error message.

---

## main.go

### CLI flags
```
-config   path to config.toml (default: ./config.toml)
-file     absolute path to the video file to serve (required)
-quality  quality preset to use: source|high|mid|low (default: source)
```

Validate at startup: `-file` must be provided and must exist on disk. Exit(1) with
a clear message if not. The resolved basename and quality are stored in the hub's
initial `SessionState`. Also launch MPV locally for the host:
```
mpv --input-ipc-server=<ipc-path> "http://localhost:<port>/stream?quality=<quality>"
```
Then start `syncclient` in-process or as a subprocess pointed at `ws://localhost:<port>/ws`
with `--name host`.

### Routes
```
GET /stream   ?quality=source|high|mid|low   -> StreamHandler
GET /ws       (WebSocket upgrade)             -> SyncHandler
```

`/files` does not exist. The host decides what plays; clients never browse.

### SyncHandler
```go
func SyncHandler(hub *Hub) http.HandlerFunc
```
- Upgrade HTTP to WebSocket using `gorilla/websocket`.
- Call `hub.Register(conn)` — this immediately sends the `init` event to the new client.
- Defer `hub.Unregister(conn)`.
- Read loop: on each message, call `hub.UpdateState(msg)` then `hub.Broadcast(conn, msg)`.
- On read error (disconnect), break loop.

### Startup log (print to stdout)
```
LocalSync running on :8080
Now playing: /home/user/Videos/movie.mkv
Quality:     source
Stream:      http://0.0.0.0:8080/stream?quality=source
Sync WS:     ws://0.0.0.0:8080/ws

Waiting for client to connect...
```

---

## cmd/syncclient/main.go

A second Go binary distributed to friends. They run it once — it connects,
receives the init payload, and launches MPV automatically.

### CLI flags
```
--server   ws://<host-tailscale-ip>:8080/ws  (required)
--ipc      path for MPV IPC socket (default: /tmp/mpvsync on Unix, \.\pipe\mpvsync on Windows)
--name     identifier sent with sync events (default: client)
```

### Behaviour
1. Connect to WebSocket server (retry with exponential backoff, up to 10 attempts).
2. Wait for the `init` message from the server:
   ```json
   { "event": "init", "file": "movie.mkv", "quality": "source", "pos": 142.3, "paused": false }
   ```
3. Once `init` is received, launch MPV as a subprocess:
   ```
   mpv --input-ipc-server=<ipc> --start=<pos> [--pause] "http://<host>:<port>/stream?quality=<quality>"
   ```
   Derive `<host>:<port>` from the `--server` flag (strip `ws://` prefix and `/ws` suffix).
   Pass `--start=<pos>` so playback begins at the current host position.
   Pass `--pause` only if `paused` is true in the `init` message.
4. Wait for the MPV IPC socket to appear (retry every 500ms, timeout 15s).
5. Connect to MPV IPC socket (Unix socket on macOS/Linux, named pipe on Windows).
6. Subscribe to MPV properties on connect:
   ```json
   { "command": ["observe_property", 1, "pause"] }
   { "command": ["observe_property", 2, "time-pos"] }
   ```
7. Two goroutines:
   - **WS -> IPC**: receive messages from server, write MPV IPC commands.
   - **IPC -> WS**: read property-change events from MPV IPC, send to server.
8. MPV IPC commands (sent to MPV):
   ```json
   { "command": ["set_property", "pause", true] }
   { "command": ["set_property", "time-pos", 142.3] }
   ```

---

## Sync message protocol (server <-> syncclient)
All messages are newline-terminated JSON:

```json
{ "event": "pause",  "state": true,  "pos": 142.3, "source": "host" }
{ "event": "pause",  "state": false, "pos": 142.3, "source": "host" }
{ "event": "seek",   "pos": 300.0,   "source": "client" }
{ "event": "init",   "file": "movie.mkv", "quality": "source", "pos": 0.0, "paused": false }
```

`source` is set by syncclient from the `--name` flag (`host` or `client`).
The hub broadcasts to the other peer. `init` is server-to-client only on connect;
it is never rebroadcast between peers.

**Seek debounce**: only send a seek event if `time-pos` changes by more than
2 seconds AND a seek is not already in-flight. Use a 500ms cooldown channel.

**Pause echo prevention**: set a boolean flag `applyingRemote = true` before
writing to MPV IPC, reset it after. If an event comes from MPV IPC while
`applyingRemote` is true, drop it (don't echo back to server).

---

## go.mod

```
module localsync

go 1.21

require (
    github.com/gorilla/websocket v1.5.1
    github.com/BurntSushi/toml   v1.3.2
)
```

---

## Build Instructions to include in README (generate a README.md too)

```bash
# Build server
go build -o localsync .

# Cross-compile syncclient for friends
GOOS=windows GOARCH=amd64 go build -o syncclient.exe ./cmd/syncclient
GOOS=darwin  GOARCH=arm64 go build -o syncclient-mac ./cmd/syncclient

# HOST: start the server with a file — MPV opens automatically on the host too
./localsync -file /home/user/Videos/movie.mkv -quality source

# FRIEND: one command — syncclient connects, receives init, launches MPV automatically
./syncclient --server ws://<host-tailscale-ip>:8080/ws
# (Windows)
syncclient.exe --server ws://<host-tailscale-ip>:8080/ws
```

---

## Error Handling Requirements

- FFmpeg not found -> print actionable error and exit(1) at startup (`exec.LookPath("ffmpeg")` check in main)
- `-file` flag missing or file not found -> exit(1) with message
- WebSocket upgrade failure -> log and return; don't crash
- MPV not found on client (`exec.LookPath("mpv")`) -> exit(1) with clear message before attempting launch
- MPV IPC socket not appearing within 15s -> exit(1) with message
- WS reconnect (syncclient) -> retry with exponential backoff, max 30s interval, indefinitely

---

## What NOT to build
- No authentication (Tailscale handles this)
- No file listing or browsing endpoint
- No HLS playlist generation (direct pipe is simpler for 1 viewer)
- No database or persistent state
- No web UI
- No subtitle handling
- No hardware encoding flags (ultrafast preset is enough for local network)
- No Lua plugin (syncclient handles the client side entirely)
