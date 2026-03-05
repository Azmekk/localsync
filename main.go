package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"

	"localsync/internal/update"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func SyncHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !hub.CanRegister() {
			http.Error(w, "max clients reached", http.StatusServiceUnavailable)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade failed: %v", err)
			return
		}
		hub.Register(conn)
		defer hub.Unregister(conn)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var raw map[string]interface{}
			if err := json.Unmarshal(msg, &raw); err == nil {
				source, _ := raw["source"].(string)
				if source != "host" {
					continue
				}
			}
			hub.UpdateState(msg)
			hub.Broadcast(conn, msg)
		}
	}
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(dir, "localsync", "config.toml")
}

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config.toml")
	filePath := flag.String("file", "", "absolute path to video file (required)")
	quality := flag.String("quality", "source", "quality preset: source|high|mid|low")
	showVersion := flag.Bool("version", false, "print version and exit")
	doUpdate := flag.Bool("update", false, "update localsync and syncclient to the latest release")
	flag.Parse()

	if *showVersion {
		fmt.Printf("localsync %s\n", update.Version)
		return
	}

	if *doUpdate {
		if err := update.SelfUpdate("localsync"); err != nil {
			fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	updateCh := update.StartBackgroundCheck()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "error: -file flag is required")
		os.Exit(1)
	}

	absFile, err := filepath.Abs(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot resolve file path: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(absFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: file not found: %s\n", absFile)
		os.Exit(1)
	}

	if *quality != "source" {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			fmt.Fprintln(os.Stderr, "error: ffmpeg not found on PATH (required for transcoding)")
			os.Exit(1)
		}
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Printf("warning: could not load config (%v), using defaults", err)
		cfg = Config{
			Port:       13771,
			MaxClients: 1,
			Quality: map[string]string{
				"source": "passthrough",
				"high":   "8000k",
				"mid":    "3000k",
				"low":    "1000k",
			},
		}
	}

	if _, ok := cfg.Quality[*quality]; !ok {
		fmt.Fprintf(os.Stderr, "error: unknown quality preset: %s\n", *quality)
		os.Exit(1)
	}

	initialState := SessionState{
		File:    filepath.Base(absFile),
		Quality: *quality,
		Pos:     0,
		Paused:  false,
	}

	hub := NewHub(initialState, cfg.MaxClients)
	go hub.Run()

	http.HandleFunc("/stream", StreamHandler(cfg, absFile))
	http.HandleFunc("/ws", SyncHandler(hub))

	// Drain background update check (wait up to 2s)
	select {
	case info := <-updateCh:
		if info != nil {
			update.PrintUpdateBanner(info)
		}
	case <-time.After(2 * time.Second):
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	streamURL := fmt.Sprintf("http://0.0.0.0:%d/stream?quality=%s", cfg.Port, *quality)
	wsURL := fmt.Sprintf("ws://0.0.0.0:%d/ws", cfg.Port)

	fmt.Printf("LocalSync %s running on %s\n", update.Version, addr)
	fmt.Printf("Now playing: %s\n", absFile)
	fmt.Printf("Quality:     %s\n", *quality)
	fmt.Printf("Stream:      %s\n", streamURL)
	fmt.Printf("Sync WS:     %s\n", wsURL)
	fmt.Println()
	fmt.Println("Waiting for client to connect...")

	// Launch host MPV
	go launchHostMPV(cfg.Port, *quality, absFile)

	log.Fatal(http.ListenAndServe(addr, nil))
}

func launchHostMPV(port int, quality string, filePath string) {
	ipcPath := getHostIPCPath()
	streamURL := fmt.Sprintf("http://localhost:%d/stream?quality=%s", port, quality)

	mpvCmd := exec.Command("mpv",
		fmt.Sprintf("--input-ipc-server=%s", ipcPath),
		streamURL,
	)
	mpvCmd.Stdout = os.Stdout
	mpvCmd.Stderr = os.Stderr

	if err := mpvCmd.Start(); err != nil {
		log.Printf("warning: could not launch host MPV: %v", err)
		return
	}

	// Launch syncclient in-process style via subprocess
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
	clientCmd := exec.Command(os.Args[0]+"_syncclient_not_used")
	_ = clientCmd // syncclient is a separate binary; host uses direct WS

	// Instead, run the syncclient binary if available, or just connect via WS
	syncClient := exec.Command("syncclient",
		"--server", wsURL,
		"--ipc", ipcPath,
		"--name", "host",
		"--no-launch",
	)
	syncClient.Stdout = os.Stdout
	syncClient.Stderr = os.Stderr
	if err := syncClient.Start(); err != nil {
		log.Printf("note: syncclient not found, host sync not active. Build syncclient and add to PATH for host-side sync.")
	}

	mpvCmd.Wait()
}

func getHostIPCPath() string {
	if os.PathSeparator == '\\' {
		return `\\.\pipe\mpvsync-host`
	}
	return "/tmp/mpvsync-host"
}
