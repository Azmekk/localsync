package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"localsync/localsync/internal/service"
)

// TranscodeOpts holds the FFmpeg transcode settings needed by the stream handler.
type TranscodeOpts struct {
	VideoCodec   string
	ExtraArgs    []string
	AudioCodec   string
	AudioBitrate string
	Subtitles    bool
	Realtime     bool
	Format       string
}

// QualityPreset defines a named quality level.
type QualityPreset struct {
	Name        string
	Bitrate     string
	Resolution  string
	Passthrough bool
	ExtraArgs   []string
}

// StreamConfig holds what the stream handler needs from the app config.
type StreamConfig struct {
	Qualities []QualityPreset
	Transcode TranscodeOpts
}

func (sc *StreamConfig) FindQuality(name string) *QualityPreset {
	for i := range sc.Qualities {
		if sc.Qualities[i].Name == name {
			return &sc.Qualities[i]
		}
	}
	return nil
}

var mimeTypes = map[string]string{
	".mkv":  "video/x-matroska",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".ts":   "video/mp2t",
	".flv":  "video/x-flv",
}

var formatContentTypes = map[string]string{
	"matroska": "video/x-matroska",
	"mpegts":   "video/mp2t",
	"mp4":      "video/mp4",
	"webm":     "video/webm",
}

// NewStreamHandler returns an HTTP handler that serves video via passthrough or transcode.
func NewStreamHandler(cfg StreamConfig, filePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		quality := r.URL.Query().Get("quality")
		if quality == "" {
			quality = "source"
		}

		preset := cfg.FindQuality(quality)
		if preset == nil {
			http.Error(w, fmt.Sprintf("unknown quality preset: %s", quality), http.StatusBadRequest)
			return
		}

		if preset.Passthrough {
			servePassthrough(w, r, filePath)
			return
		}

		serveTranscode(w, r, filePath, *preset, cfg.Transcode)
	}
}

// NewVariantHandler returns an HTTP handler that serves pre-compressed variant files
// from the .localsync/ folder next to the video file.
func NewVariantHandler(videoPath string) http.HandlerFunc {
	variantDir := service.VariantDir(videoPath)
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			http.Error(w, "variant name required", http.StatusBadRequest)
			return
		}

		// Security: prevent directory traversal
		if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
			http.Error(w, "invalid variant name", http.StatusBadRequest)
			return
		}

		filePath := filepath.Join(variantDir, name)

		// Verify the resolved path is within the variant directory
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		absDir, err := filepath.Abs(variantDir)
		if err != nil {
			http.Error(w, "invalid path", http.StatusInternalServerError)
			return
		}
		if !strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) {
			http.Error(w, "invalid variant name", http.StatusBadRequest)
			return
		}

		servePassthrough(w, r, filePath)
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

func serveTranscode(w http.ResponseWriter, r *http.Request, filePath string, preset QualityPreset, tc TranscodeOpts) {
	var args []string

	if tc.Realtime {
		args = append(args, "-re")
	}

	if start := r.URL.Query().Get("start"); start != "" {
		args = append(args, "-ss", start)
	}

	args = append(args, "-i", filePath)
	args = append(args, "-map", "0:v", "-map", "0:a")
	if tc.Subtitles {
		args = append(args, "-map", "0:s?")
	}

	if preset.Resolution != "" {
		parts := strings.SplitN(preset.Resolution, "x", 2)
		if len(parts) == 2 {
			args = append(args, "-vf", fmt.Sprintf("scale=-2:%s", parts[1]))
		}
	}

	args = append(args, "-b:v", preset.Bitrate, "-c:v", tc.VideoCodec)
	extraArgs := preset.ExtraArgs
	if len(extraArgs) == 0 {
		extraArgs = tc.ExtraArgs
	}
	args = append(args, extraArgs...)

	args = append(args, "-c:a", tc.AudioCodec)
	if tc.AudioCodec != "copy" {
		args = append(args, "-b:a", tc.AudioBitrate)
	}

	if tc.Subtitles {
		args = append(args, "-c:s", "copy")
	}

	args = append(args, "-f", tc.Format, "pipe:1")

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

	contentType := formatContentTypes[tc.Format]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
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
