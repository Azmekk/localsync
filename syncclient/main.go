package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/spf13/cobra"

	"localsync/shared/logging"
	"localsync/shared/update"
)

type Variant struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type InitMessage struct {
	Event    string    `json:"event"`
	File     string    `json:"file"`
	Quality  string    `json:"quality"`
	Pos      float64   `json:"pos"`
	Paused   bool      `json:"paused"`
	Variants []Variant `json:"variants"`
}

type SyncMessage struct {
	Event  string   `json:"event"`
	State  *bool    `json:"state,omitempty"`
	Pos    float64  `json:"pos"`
	Source string   `json:"source"`
	Speed  *float64 `json:"speed,omitempty"`
}

type StatsMessage struct {
	Event       string  `json:"event"`
	Source      string  `json:"source"`
	SpeedKbps   float64 `json:"speed_kbps"`
	BufferSecs  float64 `json:"buffer_secs"`
	BufferBytes int64   `json:"buffer_bytes"`
	Pos         float64 `json:"pos"`
}

var rootCmd = &cobra.Command{
	Use:   "syncclient",
	Short: "Connect to a localsync host and sync video playback",
	RunE:  run,
}

func init() {
	rootCmd.Flags().StringP("server", "s", "", "ws://<host>:<port>/ws (required)")
	rootCmd.Flags().String("ipc", defaultIPCPath(), "path for MPV IPC socket")
	rootCmd.Flags().StringP("name", "n", "client", "identifier sent with sync events")
	rootCmd.Flags().StringP("variant", "V", "", "variant name (skip interactive menu)")
	rootCmd.Flags().Bool("no-launch", false, "skip launching MPV (used by host)")
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")
	rootCmd.Flags().BoolP("update", "u", false, "update to the latest release")
	rootCmd.MarkFlagRequired("server")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	showVersion, _ := cmd.Flags().GetBool("version")
	if showVersion {
		fmt.Printf("syncclient %s\n", update.Version)
		return nil
	}

	doUpdate, _ := cmd.Flags().GetBool("update")
	if doUpdate {
		return update.SelfUpdate("syncclient")
	}

	// Set up file logging
	logFile, logCleanup, err := logging.Setup("syncclient")
	if err != nil {
		log.Printf("warning: could not set up log file: %v", err)
	} else {
		defer logCleanup()
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	}

	updateCh := update.StartBackgroundCheck()

	server, _ := cmd.Flags().GetString("server")
	ipcPath, _ := cmd.Flags().GetString("ipc")
	name, _ := cmd.Flags().GetString("name")
	variantFlag, _ := cmd.Flags().GetString("variant")
	noLaunch, _ := cmd.Flags().GetBool("no-launch")

	if !noLaunch {
		if _, err := exec.LookPath("mpv"); err != nil {
			return fmt.Errorf("mpv not found on PATH")
		}
	}

	// Connect with exponential backoff
	var ws *websocket.Conn
	for attempt := 0; attempt < 10; attempt++ {
		var err error
		ws, _, err = websocket.DefaultDialer.Dial(server, nil)
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
		return fmt.Errorf("could not connect to server after 10 attempts")
	}
	defer ws.Close()

	// Drain background update check
	select {
	case info := <-updateCh:
		if info != nil {
			update.PrintUpdateBanner(info)
		}
	case <-time.After(2 * time.Second):
	}

	log.Println("connected to server")

	// Read init message
	_, rawMsg, err := ws.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read init message: %w", err)
	}

	var initMsg InitMessage
	if err := json.Unmarshal(rawMsg, &initMsg); err != nil || initMsg.Event != "init" {
		return fmt.Errorf("expected init message, got: %s", string(rawMsg))
	}
	log.Printf("received init: file=%s quality=%s pos=%.1f paused=%v variants=%d",
		initMsg.File, initMsg.Quality, initMsg.Pos, initMsg.Paused, len(initMsg.Variants))

	// Determine stream URL based on variant selection
	var streamBaseURL string
	var isSeekable bool

	var selectedVariantName string
	if !noLaunch && len(initMsg.Variants) > 0 && variantFlag == "" {
		// Interactive variant selection via TUI
		selected, err := SelectVariant(initMsg)
		if err != nil {
			return fmt.Errorf("variant selection failed: %w", err)
		}
		if selected != nil {
			streamBaseURL = deriveVariantURL(server, selected.Filename)
			isSeekable = true
			selectedVariantName = selected.Name
			log.Printf("selected variant: %s", selected.Name)
		} else {
			streamBaseURL = deriveStreamURL(server, initMsg.Quality)
			isSeekable = initMsg.Quality == "source" || initMsg.Quality == "passthrough"
		}
	} else if variantFlag != "" {
		// Variant specified via flag
		streamBaseURL = deriveVariantURL(server, findVariantFilename(initMsg.Variants, variantFlag))
		isSeekable = true
		selectedVariantName = variantFlag
		log.Printf("using variant: %s", variantFlag)
	} else {
		streamBaseURL = deriveStreamURL(server, initMsg.Quality)
		isSeekable = initMsg.Quality == "source" || initMsg.Quality == "passthrough"
	}

	// Playback TUI (created before MPV so we can pipe output)
	var pbTUI *PlaybackTUI
	if !noLaunch {
		pbTUI = NewPlaybackTUI(initMsg.File, server, selectedVariantName)
	}

	if !noLaunch {
		launchURL := streamBaseURL
		mpvArgs := []string{
			fmt.Sprintf("--input-ipc-server=%s", ipcPath),
			"--demuxer-max-bytes=100MiB",
			"--demuxer-max-back-bytes=50MiB",
			"--demuxer-readahead-secs=9999",
			"--cache=yes",
		}
		if !isSeekable && initMsg.Pos > 0 {
			launchURL = fmt.Sprintf("%s&start=%.1f", streamBaseURL, initMsg.Pos)
		} else if initMsg.Pos > 0 {
			mpvArgs = append(mpvArgs, fmt.Sprintf("--start=%.1f", initMsg.Pos))
		}
		if initMsg.Paused {
			mpvArgs = append(mpvArgs, "--pause")
		}
		mpvArgs = append(mpvArgs, launchURL)

		mpvCmd := exec.Command("mpv", mpvArgs...)
		// Pipe MPV output to TUI log panel + log file
		var mpvWriters []io.Writer
		if pbTUI != nil {
			mpvWriters = append(mpvWriters, pbTUI.LogWriter)
		}
		if logFile != nil {
			mpvWriters = append(mpvWriters, logFile)
		}
		if len(mpvWriters) > 0 {
			w := io.MultiWriter(mpvWriters...)
			mpvCmd.Stdout = w
			mpvCmd.Stderr = w
		}
		if err := mpvCmd.Start(); err != nil {
			return fmt.Errorf("failed to launch mpv: %w", err)
		}
		go func() {
			mpvCmd.Wait()
			if pbTUI != nil {
				pbTUI.ShowSyncEvent("MPV exited")
				time.Sleep(500 * time.Millisecond)
				pbTUI.App.Stop()
			}
		}()
	}

	// Wait for MPV IPC socket
	ipcConn, err := waitForIPC(ipcPath, 15*time.Second)
	if err != nil {
		return fmt.Errorf("MPV IPC socket not available: %w", err)
	}
	defer ipcConn.Close()

	// Redirect log output to TUI + log file
	if pbTUI != nil {
		if logFile != nil {
			log.SetOutput(io.MultiWriter(pbTUI.LogWriter, logFile))
		} else {
			log.SetOutput(pbTUI.LogWriter)
		}
	}
	log.Println("connected to MPV IPC")

	// Subscribe to properties
	ipcWrite(ipcConn, `{"command":["observe_property",1,"pause"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",2,"time-pos"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",3,"paused-for-cache"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",4,"cache-speed"]}`)
	ipcWrite(ipcConn, `{"command":["observe_property",5,"demuxer-cache-duration"]}`)

	var (
		applyingCount       int32
		waitingForReady     int32
		waitingForRestart   int32
		waitingForBuffering int32
		restartPauseAfter   int32
		cacheSpeed          float64
		cacheDuration       float64
		cacheBytes          int64
		cacheSpeedMu        sync.Mutex
		lastSeekTime        time.Time
		lastSeekPos         float64
		posMu               sync.Mutex
		seekCooldown        = 100 * time.Millisecond
		seekThreshold       = 0.5
		wsMu                sync.Mutex
	)

	wsWrite := func(data []byte) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		return ws.WriteMessage(websocket.TextMessage, data)
	}

	// Stats reporting goroutine (client only)
	statsDone := make(chan struct{})
	if name != "host" {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-statsDone:
					return
				case <-ticker.C:
					// Poll for buffer bytes
					ipcWrite(ipcConn, `{"command":["get_property","demuxer-cache-state"],"request_id":100}`)
					posMu.Lock()
					pos := lastSeekPos
					posMu.Unlock()
					cacheSpeedMu.Lock()
					speedKbps := cacheSpeed * 8 / 1000
					bufSecs := cacheDuration
					bufBytes := cacheBytes
					cacheSpeedMu.Unlock()
					msg := StatsMessage{
						Event:       "stats",
						Source:      name,
						SpeedKbps:   speedKbps,
						BufferSecs:  bufSecs,
						BufferBytes: bufBytes,
						Pos:         pos,
					}
					data, _ := json.Marshal(msg)
					wsWrite(data)
					// Update TUI
					if pbTUI != nil {
						pbTUI.UpdateStats(statsUpdate{
							Pos:         pos,
							BufferSecs:  bufSecs,
							BufferBytes: bufBytes,
							SpeedKbps:   speedKbps,
						})
					}
				}
			}
		}()
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
				if !isSeekable && name != "host" {
					loadURL := fmt.Sprintf("%s&start=%.1f", streamBaseURL, msg.Pos)
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["loadfile","%s","replace"]}`, loadURL))
					atomic.StoreInt32(&restartPauseAfter, 0)
					atomic.StoreInt32(&waitingForRestart, 1)
					time.AfterFunc(10*time.Second, func() {
						if atomic.CompareAndSwapInt32(&waitingForRestart, 1, 0) {
							log.Println("playback-restart timeout, sending ready anyway")
							atomic.AddInt32(&applyingCount, -1)
							readyMsg, _ := json.Marshal(SyncMessage{Event: "ready", Source: name})
							wsWrite(readyMsg)
						}
					})
				} else {
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
					if name == "host" && !isSeekable {
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
					if !isSeekable && name != "host" {
						loadURL := fmt.Sprintf("%s&start=%.1f", streamBaseURL, msg.Pos)
						ipcWrite(ipcConn, fmt.Sprintf(`{"command":["loadfile","%s","replace"]}`, loadURL))
						if msg.State != nil && *msg.State {
							atomic.StoreInt32(&restartPauseAfter, 1)
						} else {
							atomic.StoreInt32(&restartPauseAfter, 0)
						}
						atomic.StoreInt32(&waitingForRestart, 1)
						time.AfterFunc(10*time.Second, func() {
							if atomic.CompareAndSwapInt32(&waitingForRestart, 1, 0) {
								log.Println("playback-restart timeout, sending ready anyway")
								if atomic.LoadInt32(&restartPauseAfter) == 1 {
									ipcWrite(ipcConn, `{"command":["set_property","pause",true]}`)
								}
								atomic.AddInt32(&applyingCount, -1)
								readyMsg, _ := json.Marshal(SyncMessage{Event: "ready", Source: name})
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
				if !isSeekable && name != "host" {
					break
				}
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
				if name == "host" && atomic.CompareAndSwapInt32(&waitingForReady, 1, 0) {
					atomic.AddInt32(&applyingCount, 1)
					ipcWrite(ipcConn, `{"command":["set_property","pause",false]}`)
					time.AfterFunc(200*time.Millisecond, func() {
						atomic.AddInt32(&applyingCount, -1)
					})
					log.Println("client ready, resuming playback")
				}
			case "buffering":
				if name == "host" {
					if msg.State != nil && *msg.State {
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

	// IPC event loop
	if name == "host" {
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
						Source: name,
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
					Source: name,
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
						Source: name,
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
		clientIPCLoop := func() {
			scanner := bufio.NewScanner(ipcConn)
			for scanner.Scan() {
				line := scanner.Text()

				var event map[string]interface{}
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					continue
				}

				// Handle demuxer-cache-state response (request_id: 100)
				if reqID, ok := event["request_id"].(float64); ok && int(reqID) == 100 {
					if data, ok := event["data"].(map[string]interface{}); ok {
						if fwBytes, ok := data["fw-bytes"].(float64); ok {
							cacheSpeedMu.Lock()
							cacheBytes = int64(fwBytes)
							cacheSpeedMu.Unlock()
						}
					}
					continue
				}

				eventName, _ := event["event"].(string)

				if eventName == "playback-restart" {
					if atomic.CompareAndSwapInt32(&waitingForRestart, 1, 0) {
						log.Println("playback-restart received, signaling ready")
						if atomic.CompareAndSwapInt32(&restartPauseAfter, 1, 0) {
							ipcWrite(ipcConn, `{"command":["set_property","pause",true]}`)
						}
						atomic.AddInt32(&applyingCount, -1)
						readyMsg, _ := json.Marshal(SyncMessage{Event: "ready", Source: name})
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
				case "demuxer-cache-duration":
					if dur, ok := event["data"].(float64); ok {
						cacheSpeedMu.Lock()
						cacheDuration = dur
						cacheSpeedMu.Unlock()
					}
				case "paused-for-cache":
					paused, ok := event["data"].(bool)
					if !ok {
						continue
					}
					if paused {
						cacheSpeedMu.Lock()
						speedKbps := cacheSpeed * 8 / 1000
						cacheSpeedMu.Unlock()
						bufMsg := SyncMessage{
							Event:  "buffering",
							State:  &paused,
							Speed:  &speedKbps,
							Source: name,
						}
						data, _ := json.Marshal(bufMsg)
						wsWrite(data)
						log.Printf("buffering — download speed: %.0f kbps", speedKbps)
						if pbTUI != nil {
							pbTUI.ShowSyncEvent(fmt.Sprintf("Buffering (%.0f kbps)", speedKbps))
						}
					} else {
						notPaused := false
						bufMsg := SyncMessage{
							Event:  "buffering",
							State:  &notPaused,
							Source: name,
						}
						data, _ := json.Marshal(bufMsg)
						wsWrite(data)
						log.Println("buffer recovered")
					}
				}
			}
		}

		if pbTUI != nil {
			// Run IPC loop in background, TUI blocks
			go clientIPCLoop()
		} else {
			// No TUI — IPC loop blocks
			clientIPCLoop()
		}
	}

	// Run playback TUI if active (blocking call)
	if pbTUI != nil {
		return pbTUI.App.Run()
	}

	close(statsDone)
	return nil
}


func findVariantFilename(variants []Variant, name string) string {
	for _, v := range variants {
		if v.Name == name || v.Filename == name {
			return v.Filename
		}
	}
	// Fall back to treating it as a filename directly
	return name
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

func deriveVariantURL(serverWS string, filename string) string {
	u, err := url.Parse(serverWS)
	if err != nil {
		log.Fatalf("invalid server URL: %v", err)
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/variant/%s", scheme, u.Host, filename)
}

func defaultIPCPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\mpvsync`
	}
	return "/tmp/mpvsync"
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
