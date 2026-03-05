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
)

type InitMessage struct {
	Event   string  `json:"event"`
	File    string  `json:"file"`
	Quality string  `json:"quality"`
	Pos     float64 `json:"pos"`
	Paused  bool    `json:"paused"`
}

type SyncMessage struct {
	Event  string  `json:"event"`
	State  *bool   `json:"state,omitempty"`
	Pos    float64 `json:"pos"`
	Source string  `json:"source"`
}

func main() {
	server := flag.String("server", "", "ws://<host>:<port>/ws (required)")
	ipcPath := flag.String("ipc", defaultIPCPath(), "path for MPV IPC socket")
	name := flag.String("name", "client", "identifier sent with sync events")
	noLaunch := flag.Bool("no-launch", false, "skip launching MPV (used by host)")
	flag.Parse()

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
	log.Printf("received init: file=%s quality=%s pos=%.1f paused=%v",
		initMsg.File, initMsg.Quality, initMsg.Pos, initMsg.Paused)

	// Derive stream URL from server flag
	streamURL := deriveStreamURL(*server, initMsg.Quality)

	if !*noLaunch {
		// Launch MPV
		mpvArgs := []string{
			fmt.Sprintf("--input-ipc-server=%s", *ipcPath),
			fmt.Sprintf("--start=%.1f", initMsg.Pos),
		}
		if initMsg.Paused {
			mpvArgs = append(mpvArgs, "--pause")
		}
		mpvArgs = append(mpvArgs, streamURL)

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

	var (
		applyingCount int32
		lastSeekTime  time.Time
		lastSeekPos   float64
		posMu         sync.Mutex
		seekCooldown  = 100 * time.Millisecond
		seekThreshold = 0.5
	)

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

			atomic.AddInt32(&applyingCount, 1)

			switch msg.Event {
			case "seek":
				posMu.Lock()
				lastSeekPos = msg.Pos
				posMu.Unlock()
				ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
			case "pause":
				if msg.State != nil {
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","pause",%v]}`, *msg.State))
				}
				if msg.Pos > 0 {
					posMu.Lock()
					lastSeekPos = msg.Pos
					posMu.Unlock()
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
				}
			case "sync":
				// Compare remote position with local, seek if drift > 1s
				posMu.Lock()
				localPos := lastSeekPos
				posMu.Unlock()
				if math.Abs(msg.Pos-localPos) > 1.0 {
					posMu.Lock()
					lastSeekPos = msg.Pos
					posMu.Unlock()
					ipcWrite(ipcConn, fmt.Sprintf(`{"command":["set_property","time-pos",%f]}`, msg.Pos))
				}
			}

			// Small delay to let MPV process the command before decrementing
			time.AfterFunc(200*time.Millisecond, func() {
				atomic.AddInt32(&applyingCount, -1)
			})
		}
	}()

	// Periodic position sync (drift safety net)
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
				ws.WriteMessage(websocket.TextMessage, data)
			}
		}
	}()

	// IPC -> WS goroutine (runs on main goroutine)
	scanner := bufio.NewScanner(ipcConn)
	for scanner.Scan() {
		line := scanner.Text()

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Only handle property-change events
		if event["event"] != "property-change" {
			continue
		}

		if atomic.LoadInt32(&applyingCount) > 0 {
			continue
		}

		propName, _ := event["name"].(string)
		switch propName {
		case "pause":
			paused, ok := event["data"].(bool)
			if !ok {
				continue
			}
			// Get current position
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
			ws.WriteMessage(websocket.TextMessage, data)

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
				ws.WriteMessage(websocket.TextMessage, data)
			} else {
				lastSeekPos = pos
				posMu.Unlock()
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
