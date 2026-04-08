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

	"localsync/shared/update"
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
				event, _ := raw["event"].(string)
				if source != "host" && event != "ready" && event != "buffering" {
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
	quality := flag.String("quality", "source", "quality preset: source|1080p|720p|480p")
	precreateHLS := flag.Bool("precreate-hls", false, "generate HLS segments for the given quality and exit")
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

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Printf("warning: could not load config (%v), using defaults", err)
		cfg = Config{
			Port:       13771,
			MaxClients: 1,
			Quality: []QualityPreset{
				{Name: "source", Passthrough: true},
				{Name: "1080p", Bitrate: "8000k", Resolution: "1920x1080"},
				{Name: "720p", Bitrate: "3000k", Resolution: "1280x720"},
				{Name: "480p", Bitrate: "1000k", Resolution: "854x480"},
			},
		}
	}

	preset := cfg.FindQuality(*quality)
	if preset == nil {
		fmt.Fprintf(os.Stderr, "error: unknown quality preset: %s\n", *quality)
		os.Exit(1)
	}

	if !preset.Passthrough {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			fmt.Fprintln(os.Stderr, "error: ffmpeg not found on PATH (required for transcoding)")
			os.Exit(1)
		}
	}

	// HLS manager setup
	var hlsMgr *HLSManager
	if cfg.HLS.Enabled {
		hlsMgr = NewHLSManager(cfg, absFile)
	}

	// One-shot HLS generation mode
	if *precreateHLS {
		if hlsMgr == nil {
			hlsMgr = NewHLSManager(cfg, absFile)
		}
		if err := hlsMgr.GenerateAll(); err != nil {
			fmt.Fprintf(os.Stderr, "HLS generation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("HLS generation complete for all variants")
		return
	}

	var hlsQualities []string
	if hlsMgr != nil {
		hlsQualities = hlsMgr.resolveQualityNames()
	}

	initialState := SessionState{
		File:      filepath.Base(absFile),
		Quality:   *quality,
		Pos:       0,
		Paused:    false,
		HLSMode:   hlsMgr != nil && hlsMgr.HasCompleteCache(),
		Qualities: hlsQualities,
	}

	hub := NewHub(initialState, cfg.MaxClients)
	go hub.Run()

	// Background HLS auto-generation
	if hlsMgr != nil && cfg.HLS.AutoGenerate {
		go func() {
			if err := hlsMgr.GenerateAll(); err != nil {
				log.Printf("[hls] generation failed: %v", err)
			} else {
				hub.SetHLSReady(true, hlsQualities)
				log.Println("[hls] all variants ready")
			}
		}()
	}

	http.HandleFunc("/stream", StreamHandler(cfg, absFile, hlsMgr))
	http.HandleFunc("/ws", SyncHandler(hub))
	if hlsMgr != nil {
		http.HandleFunc("/hls/", hlsMgr.ServeHTTP)
	}

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

	mpvCmd := exec.Command("mpv",
		fmt.Sprintf("--input-ipc-server=%s", ipcPath),
		filePath,
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
