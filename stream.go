package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var mimeTypes = map[string]string{
	".mkv":  "video/x-matroska",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".ts":   "video/mp2t",
	".flv":  "video/x-flv",
}

func StreamHandler(cfg Config, filePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		quality := r.URL.Query().Get("quality")
		if quality == "" {
			quality = "source"
		}

		bitrate, ok := cfg.Quality[quality]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown quality preset: %s", quality), http.StatusBadRequest)
			return
		}

		if quality == "source" || bitrate == "passthrough" {
			servePassthrough(w, r, filePath)
			return
		}

		serveTranscode(w, r, filePath, bitrate)
	}
}

func servePassthrough(w http.ResponseWriter, r *http.Request, filePath string) {
	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "failed to stat file", http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := mimeTypes[ext]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(filePath), stat.ModTime(), f)
}

func serveTranscode(w http.ResponseWriter, r *http.Request, filePath string, bitrate string) {
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

	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "failed to create ffmpeg pipe", http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf("failed to start ffmpeg: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache")

	done := r.Context().Done()
	go func() {
		<-done
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	if _, err := io.Copy(w, stdout); err != nil {
		log.Printf("stream copy ended: %v", err)
	}

	cmd.Wait()
}
