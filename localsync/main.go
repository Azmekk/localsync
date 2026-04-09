package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/cobra"

	"localsync/localsync/internal/handler"
	"localsync/localsync/internal/model"
	"localsync/localsync/internal/service"
	"localsync/shared/update"
)

var rootCmd = &cobra.Command{
	Use:   "localsync",
	Short: "Sync video playback between MPV instances over a local network",
	RunE:  run,
}

func init() {
	rootCmd.Flags().StringP("config", "c", defaultConfigPath(), "path to config.toml")
	rootCmd.Flags().StringP("file", "f", "", "path to video file")
	rootCmd.Flags().String("quality", "source", "quality preset: source|1080p|720p|480p")
	rootCmd.Flags().Bool("create-media-folder", false, "create .localsync/ variant folder next to video and exit")
	rootCmd.Flags().Bool("version", false, "print version and exit")
	rootCmd.Flags().Bool("update", false, "update to the latest release")
	rootCmd.MarkFlagRequired("file")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	showVersion, _ := cmd.Flags().GetBool("version")
	if showVersion {
		fmt.Printf("localsync %s\n", update.Version)
		return nil
	}

	doUpdate, _ := cmd.Flags().GetBool("update")
	if doUpdate {
		return update.SelfUpdate("localsync")
	}

	updateCh := update.StartBackgroundCheck()

	filePath, _ := cmd.Flags().GetString("file")
	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("cannot resolve file path: %w", err)
	}
	if _, err := os.Stat(absFile); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", absFile)
	}

	// Handle --create-media-folder
	createFolder, _ := cmd.Flags().GetBool("create-media-folder")
	if createFolder {
		return service.CreateMediaFolder(absFile)
	}

	configPath, _ := cmd.Flags().GetString("config")
	cfg, err := LoadConfig(configPath)
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

	quality, _ := cmd.Flags().GetString("quality")
	preset := cfg.FindQuality(quality)
	if preset == nil {
		return fmt.Errorf("unknown quality preset: %s", quality)
	}

	if !preset.Passthrough {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return fmt.Errorf("ffmpeg not found on PATH (required for transcoding)")
		}
	}

	// Scan for pre-compressed variants
	variants := service.ScanVariants(absFile)
	if len(variants) > 0 {
		log.Printf("found %d variant(s) in .localsync/:", len(variants))
		for _, v := range variants {
			log.Printf("  %s (%s, %.1f MB)", v.Name, v.Filename, float64(v.Size)/(1024*1024))
		}
	}

	initialState := model.SessionState{
		File:     filepath.Base(absFile),
		Quality:  quality,
		Pos:      0,
		Paused:   false,
		Variants: variants,
	}

	hub := service.NewHub(initialState, cfg.MaxClients)
	go hub.Run()

	// Start stats display
	statsDone := make(chan struct{})
	service.StartStatsDisplay(hub, 3*time.Second, statsDone)

	// Build chi router
	r := chi.NewRouter()
	r.Get("/stream", handler.NewStreamHandler(toStreamConfig(cfg), absFile))
	r.HandleFunc("/ws", handler.NewWSHandler(hub))
	r.Get("/variant/{name}", handler.NewVariantHandler(absFile))

	// Drain background update check
	select {
	case info := <-updateCh:
		if info != nil {
			update.PrintUpdateBanner(info)
		}
	case <-time.After(2 * time.Second):
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	streamURL := fmt.Sprintf("http://0.0.0.0:%d/stream?quality=%s", cfg.Port, quality)
	wsURL := fmt.Sprintf("ws://0.0.0.0:%d/ws", cfg.Port)

	fmt.Printf("LocalSync %s running on %s\n", update.Version, addr)
	fmt.Printf("Now playing: %s\n", absFile)
	fmt.Printf("Quality:     %s\n", quality)
	fmt.Printf("Stream:      %s\n", streamURL)
	fmt.Printf("Sync WS:     %s\n", wsURL)
	fmt.Println()
	fmt.Println("Waiting for client to connect...")

	go launchHostMPV(cfg.Port, quality, absFile)

	return http.ListenAndServe(addr, r)
}

func toStreamConfig(cfg Config) handler.StreamConfig {
	sc := handler.StreamConfig{
		Transcode: handler.TranscodeOpts{
			VideoCodec:   cfg.Transcode.VideoCodec,
			ExtraArgs:    cfg.Transcode.ExtraArgs,
			AudioCodec:   cfg.Transcode.AudioCodec,
			AudioBitrate: cfg.Transcode.AudioBitrate,
			Subtitles:    cfg.Transcode.Subtitles,
			Realtime:     cfg.Transcode.Realtime,
			Format:       cfg.Transcode.Format,
		},
	}
	for _, q := range cfg.Quality {
		sc.Qualities = append(sc.Qualities, handler.QualityPreset{
			Name:        q.Name,
			Bitrate:     q.Bitrate,
			Resolution:  q.Resolution,
			Passthrough: q.Passthrough,
			ExtraArgs:   q.ExtraArgs,
		})
	}
	return sc
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(dir, "localsync", "config.toml")
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

	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
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
