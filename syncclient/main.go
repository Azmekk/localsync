package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"localsync/shared/update"
)

type InitMessage struct {
	Event     string   `json:"event"`
	File      string   `json:"file"`
	Quality   string   `json:"quality"`
	Pos       float64  `json:"pos"`
	Paused    bool     `json:"paused"`
	HLSMode   bool     `json:"hls_mode"`
	Qualities []string `json:"qualities"`
}

type SyncMessage struct {
	Event  string   `json:"event"`
	State  *bool    `json:"state,omitempty"`
	Pos    float64  `json:"pos"`
	Source string   `json:"source"`
	Speed  *float64 `json:"speed,omitempty"`
}

func main() {
	server := flag.String("server", "", "ws://<host>:<port>/ws (required)")
	ipcPath := flag.String("ipc", defaultIPCPath(), "path for MPV IPC socket")
	name := flag.String("name", "client", "identifier sent with sync events")
	quality := flag.String("quality", "", "quality to use in HLS mode (adaptive if empty)")
	noLaunch := flag.Bool("no-launch", false, "skip launching MPV (used by host)")
	showVersion := flag.Bool("version", false, "print version and exit")
	doUpdate := flag.Bool("update", false, "update localsync and syncclient to the latest release")
	flag.Parse()

	if *showVersion {
		fmt.Printf("syncclient %s\n", update.Version)
		return
	}

	if *doUpdate {
		if err := update.SelfUpdate("syncclient"); err != nil {
			fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	updateCh := update.StartBackgroundCheck()

	if *server == "" {
		fmt.Fprintln(os.Stderr, "error: --server flag is required")
		os.Exit(1)
	}

	if !*noLaunch {
		if _, err := exec.LookPath("mpv"); err != nil {
			fmt.Fprintln(os.Stderr, "error: mpv not found on PATH")
			os.Exit(1)
		}
	}

	// Connect to WebSocket with exponential backoff
	var ws *websocket.Conn
	for attempt := 0; attempt < 10; attempt++ {
		var err error
		ws, _, err = websocket.DefaultDialer.Dial(*server, nil)
		if err == nil {
			break
		}
		delay := time.Duration(math.Min(float64(time.Second)*math.Pow(2, float64(attempt)), 30000)) * time.Millisecond / time.Millisecond
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		log.Printf("connection attempt %d failed: %v, retrying in %v", attempt+1, err, delay)
		time.Sleep(delay)
	}
	if ws == nil {
		fmt.Fprintln(os.Stderr, "error: could not connect to server after 10 attempts")
		os.Exit(1)
	}
	defer ws.Close()
	// Drain background update check (wait up to 2s)
	select {
	case info := <-updateCh:
		if info != nil {
			update.PrintUpdateBanner(info)
		}
	case <-time.After(2 * time.Second):
	}

	log.Println("connected to server")

	// Wait for init message
	_, rawMsg, err := ws.ReadMessage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to read init message: %v\n", err)
		os.Exit(1)
	}

	var initMsg InitMessage
	if err := json.Unmarshal(rawMsg, &initMsg); err != nil || initMsg.Event != "init" {
		fmt.Fprintf(os.Stderr, "error: expected init message, got: %s\n", string(rawMsg))
		os.Exit(1)
	}
	log.Printf("received init: file=%s quality=%s pos=%.1f paused=%v hls_mode=%v",
		initMsg.File, initMsg.Quality, initMsg.Pos, initMsg.Paused, initMsg.HLSMode)

	// Derive stream URL and seekability
	var streamBaseURL string
	var isTranscoded bool
	var isSeekable bool

	if initMsg.HLSMode {
		if *quality != "" && *quality != "adaptive" {
			// User pinned a specific quality
			streamBaseURL = deriveStreamURL(*server, *quality)
		} else {
			// Adaptive — load master playlist
			streamBaseURL = deriveHLSURL(*server)
		}
		isSeekable = true
		isTranscoded = false
		if len(initMsg.Qualities) > 0 {
			log.Printf("HLS mode — available qualities: %v", initMsg.Qualities)
			if *quality == "" || *quality == "adaptive" {
				log.Println("using adaptive streaming (tip: use --quality <name> to pin a specific quality)")
			}
		}
	} else {
		streamBaseURL = deriveStreamURL(*server, initMsg.Quality)
		isTranscoded = initMsg.Quality != "source" && initMsg.Quality != "passthrough"
		isSeekable = !isTranscoded
	}

	if !*noLaunch {
		// Launch MPV
		launchURL := streamBaseURL
		mpvArgs := []string{
			fmt.Sprintf("--input-ipc-server=%s", *ipcPath),
			"--demuxer-max-bytes=100MiB",
			"--demuxer-max-back-bytes=50MiB",
			"--cache=yes",
		}
		if !isSeekable && initMsg.Pos > 0 {
			// --start doesn't work on non-seekable transcoded streams; use start param instead
			launchURL = fmt.Sprintf("%s&start=%.1f", streamBaseURL, initMsg.Pos)
		} else if initMsg.Pos > 0 {
			mpvArgs = append(mpvArgs, fmt.Sprintf("--start=%.1f", initMsg.Pos))
		}
		if initMsg.Paused {
			mpvArgs = append(mpvArgs, "--pause")
		}
		mpvArgs = append(mpvArgs, launchURL)

		mpvCmd := exec.Command("mpv", mpvArgs...)
		mpvCmd.Stdout = os.Stdout
		mpvCmd.Stderr = os.Stderr
		if err := mpvCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to launch mpv: %v\n", err)
			os.Exit(1)
		}
		go func() {
			mpvCmd.Wait()
			log.Println("MPV exited")
			os.Exit(0)
		}()
	}

	// Wait for MPV IPC socket
	ipcConn, err := waitForIPC(*ipcPath, 15*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: MPV IPC socket not available: %v\n", err)
		os.Exit(1)
	}
	defer ipcConn.Close()
	log.Println("connected to MPV IPC")

	// Subscribe to properties
	ipcWrite(ipcConn, `{"command":["observe_property",1,"pause"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",2,"time-pos"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",3,"paused-for-cache"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",4,"cache-speed"]}`)

	var (
		applyingCount       int32
		waitingForReady     int32
		waitingForRestart   int32
		waitingForBuffering int32
		restartPauseAfter   int32 // if set, re-pause after playback-restart
		cacheSpeed          float64
		cacheSpeedMu        sync.Mutex
		lastSeekTime        time.Time
		lastSeekPos        float64
		posMu              sync.Mutex
		seekCooldown       = 100 * time.Millisecond
		seekThreshold      = 0.5
		wsMu               sync.Mutex
	)

	wsWrite := func(data []byte) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		return ws.WriteMessage(websocket.TextMessage, data)
	}

	// WS -> IPC goroutine
	go func() {
		for {
			_, rawMsg, err := ws.ReadMessage()
			if err != nil {
				log.Printf("WS read error: %v", err)
				return
			}

			var msg SyncMessage
			if err := json.Unmarshal(rawMsg, &msg); err != nil {
				continue
			}

			switch msg.Event {
			case "seek":
				atomic.AddInt32(&applyingCount, 1)
				posMu.Lock()
				lastSeekPos = msg.Pos
				posMu.Unlock()
				if !isSeekable && *name != "host" {
					// Reload stream at new position for transcoded content
					loadURL := fmt.Sprintf("%s&start=%.1f", streamBaseURL, msg.Pos)
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["loadfile","%s","replace"]}`, loadURL))
					// Wait for playback-restart event to signal ready
					atomic.StoreInt32(&restartPauseAfter, 0)
					atomic.StoreInt32(&waitingForRestart, 1)
					// Fallback timeout in case playback-restart never fires
					time.AfterFunc(10*time.Second, func() {
						if atomic.CompareAndSwapInt32(&waitingForRestart, 1, 0) {
							log.Println("playback-restart timeout, sending ready anyway")
							atomic.AddInt32(&applyingCount, -1)
							readyMsg, _ := json.Marshal(SyncMessage{Event: "ready", Source: *name})
							wsWrite(readyMsg)
						}
					})
				} else {
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
					if *name == "host" && !isSeekable {
						// Host pauses and waits for client to be ready
						ipcWrite(ipcConn, `{"command":["set_property","pause",true]}`)
						atomic.StoreInt32(&waitingForReady, 1)
					}
					time.AfterFunc(200*time.Millisecond, func() {
						atomic.AddInt32(&applyingCount, -1)
					})
				}
			case "pause":
				atomic.AddInt32(&applyingCount, 1)
				if msg.State != nil {
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","pause",%v]}`, *msg.State))
				}
				if msg.Pos > 0 {
					posMu.Lock()
					lastSeekPos = msg.Pos
					posMu.Unlock()
					if !isSeekable && *name != "host" {
						loadURL := fmt.Sprintf("%s&start=%.1f", streamBaseURL, msg.Pos)
						ipcWrite(ipcConn, fmt.Sprintf(`{"command":["loadfile","%s","replace"]}`, loadURL))
						// Re-pause after restart if needed
						if msg.State != nil && *msg.State {
							atomic.StoreInt32(&restartPauseAfter, 1)
						} else {
							atomic.StoreInt32(&restartPauseAfter, 0)
						}
						atomic.StoreInt32(&waitingForRestart, 1)
						// Fallback timeout in case playback-restart never fires
						time.AfterFunc(10*time.Second, func() {
							if atomic.CompareAndSwapInt32(&waitingForRestart, 1, 0) {
								log.Println("playback-restart timeout, sending ready anyway")
								if atomic.LoadInt32(&restartPauseAfter) == 1 {
									ipcWrite(ipcConn, `{"command":["set_property","pause",true]}`)
								}
								atomic.AddInt32(&applyingCount, -1)
								readyMsg, _ := json.Marshal(SyncMessage{Event: "ready", Source: *name})
								wsWrite(readyMsg)
							}
						})
					} else {
						ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
						time.AfterFunc(200*time.Millisecond, func() {
							atomic.AddInt32(&applyingCount, -1)
						})
					}
				} else {
					time.AfterFunc(200*time.Millisecond, func() {
						atomic.AddInt32(&applyingCount, -1)
					})
				}
			case "sync":
				// Skip drift correction for non-seekable clients (live transcoded streams).
				// HLS and source streams are seekable and support drift correction.
				if !isSeekable && *name != "host" {
					break
				}
				// Compare remote position with local, seek if drift > 1s
				posMu.Lock()
				localPos := lastSeekPos
				posMu.Unlock()
				if math.Abs(msg.Pos-localPos) > 1.0 {
					atomic.AddInt32(&applyingCount, 1)
					posMu.Lock()
					lastSeekPos = msg.Pos
					posMu.Unlock()
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
					time.AfterFunc(200*time.Millisecond, func() {
						atomic.AddInt32(&applyingCount, -1)
					})
				}
			case "ready":
				// Client is ready after rebuffering — unpause host
				if *name == "host" && atomic.CompareAndSwapInt32(&waitingForReady, 1, 0) {
					atomic.AddInt32(&applyingCount, 1)
					ipcWrite(ipcConn, `{"command":["set_property","pause",false]}`)
					time.AfterFunc(200*time.Millisecond, func() {
						atomic.AddInt32(&applyingCount, -1)
					})
					log.Println("client ready, resuming playback")
				}
			case "buffering":
				if *name == "host" {
					if msg.State != nil && *msg.State {
						// Client is buffering — pause host
						atomic.AddInt32(&applyingCount, 1)
						atomic.StoreInt32(&waitingForBuffering, 1)
						ipcWrite(ipcConn, `{"command":["set_property","pause",true]}`)
						speed := float64(0)
						if msg.Speed != nil {
							speed = *msg.Speed
						}
						log.Printf("client buffering — pausing (client download speed: %.0f kbps, recommended bitrate: <= %.0f kbps)", speed, speed)
						time.AfterFunc(200*time.Millisecond, func() {
							atomic.AddInt32(&applyingCount, -1)
						})
					} else {
						// Client finished buffering — resume host
						if atomic.CompareAndSwapInt32(&waitingForBuffering, 1, 0) {
							atomic.AddInt32(&applyingCount, 1)
							ipcWrite(ipcConn, `{"command":["set_property","pause",false]}`)
							time.AfterFunc(200*time.Millisecond, func() {
								atomic.AddInt32(&applyingCount, -1)
							})
							log.Println("client buffer recovered, resuming")
						}
					}
				}
			}
		}
	}()

	// IPC event loop: track local position for drift correction (host-only control)
	if *name == "host" {
		// Host: send periodic sync and forward IPC events to WS
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				posMu.Lock()
				pos := lastSeekPos
				posMu.Unlock()
				if pos > 0 {
					msg := SyncMessage{
						Event:  "sync",
						Pos:    pos,
						Source: *name,
					}
					data, _ := json.Marshal(msg)
					wsWrite(data)
				}
			}
		}()

		scanner := bufio.NewScanner(ipcConn)
		for scanner.Scan() {
			line := scanner.Text()

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			if event["event"] != "property-change" {
				continue
			}

			if ac := atomic.LoadInt32(&applyingCount); ac > 0 {
				continue
			}
			if atomic.LoadInt32(&waitingForReady) != 0 {
				continue
			}
			if atomic.LoadInt32(&waitingForBuffering) != 0 {
				continue
			}

			propName, _ := event["name"].(string)
			switch propName {
			case "pause":
				paused, ok := event["data"].(bool)
				if !ok {
					continue
				}
				posMu.Lock()
				pos := lastSeekPos
				posMu.Unlock()
				msg := SyncMessage{
					Event:  "pause",
					State:  &paused,
					Pos:    pos,
					Source: *name,
				}
				data, _ := json.Marshal(msg)
				wsWrite(data)

			case "time-pos":
				pos, ok := event["data"].(float64)
				if !ok {
					continue
				}

				now := time.Now()
				posMu.Lock()
				diff := math.Abs(pos - lastSeekPos)
				if diff > seekThreshold && now.Sub(lastSeekTime) > seekCooldown {
					lastSeekTime = now
					lastSeekPos = pos
					posMu.Unlock()
					msg := SyncMessage{
						Event:  "seek",
						Pos:    pos,
						Source: *name,
					}
					data, _ := json.Marshal(msg)
					wsWrite(data)
				} else {
					lastSeekPos = pos
					posMu.Unlock()
				}
			}
		}
	} else {
		// Client: track local position and detect playback-restart
		scanner := bufio.NewScanner(ipcConn)
		for scanner.Scan() {
			line := scanner.Text()

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			eventName, _ := event["event"].(string)

			// Detect playback-restart after loadfile for transcoded seeks
			if eventName == "playback-restart" {
				if atomic.CompareAndSwapInt32(&waitingForRestart, 1, 0) {
					log.Println("playback-restart received, signaling ready")
					if atomic.CompareAndSwapInt32(&restartPauseAfter, 1, 0) {
						ipcWrite(ipcConn, `{"command":["set_property","pause",true]}`)
					}
					atomic.AddInt32(&applyingCount, -1)
					readyMsg, _ := json.Marshal(SyncMessage{Event: "ready", Source: *name})
					wsWrite(readyMsg)
				}
				continue
			}

			if eventName != "property-change" {
				continue
			}

			propName, _ := event["name"].(string)
			switch propName {
			case "time-pos":
				if pos, ok := event["data"].(float64); ok {
					posMu.Lock()
					lastSeekPos = pos
					posMu.Unlock()
				}
			case "cache-speed":
				if speed, ok := event["data"].(float64); ok {
					cacheSpeedMu.Lock()
					cacheSpeed = speed
					cacheSpeedMu.Unlock()
				}
			case "paused-for-cache":
				paused, ok := event["data"].(bool)
				if !ok {
					continue
				}
				if paused {
					cacheSpeedMu.Lock()
					speedKbps := cacheSpeed * 8 / 1000 // bytes/s to kbps
					cacheSpeedMu.Unlock()
					bufMsg := SyncMessage{
						Event:  "buffering",
						State:  &paused,
						Speed:  &speedKbps,
						Source: *name,
					}
					data, _ := json.Marshal(bufMsg)
					wsWrite(data)
					log.Printf("buffering — download speed: %.0f kbps", speedKbps)
				} else {
					notPaused := false
					bufMsg := SyncMessage{
						Event:  "buffering",
						State:  &notPaused,
						Source: *name,
					}
					data, _ := json.Marshal(bufMsg)
					wsWrite(data)
					log.Println("buffer recovered")
				}
			}
		}
	}
}

func defaultIPCPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\mpvsync`
	}
	return "/tmp/mpvsync"
}

func deriveStreamURL(serverWS string, quality string) string {
	u, err := url.Parse(serverWS)
	if err != nil {
		log.Fatalf("invalid server URL: %v", err)
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/stream?quality=%s", scheme, u.Host, quality)
}

func deriveHLSURL(serverWS string) string {
	u, err := url.Parse(serverWS)
	if err != nil {
		log.Fatalf("invalid server URL: %v", err)
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/hls/master.m3u8", scheme, u.Host)
}

func waitForIPC(path string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := connectIPC(path)
		if err == nil {
			return conn, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout waiting for IPC socket at %s", path)
}

func ipcWrite(conn net.Conn, msg string) {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	conn.Write([]byte(msg))
}

