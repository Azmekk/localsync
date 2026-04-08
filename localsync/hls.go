package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// HLSManager manages multi-variant HLS pre-creation and serving.
// HLS segments are stored next to the video in a .localsync-hls/ directory
// with a flat structure: master.m3u8, stream_0.m3u8, stream_0_00000.ts, etc.
type HLSManager struct {
	cfg        Config
	sourceFile string
	hlsDir     string // <video_dir>/.localsync-hls

	mu       sync.RWMutex
	building bool
}

func NewHLSManager(cfg Config, sourceFile string) *HLSManager {
	return &HLSManager{
		cfg:        cfg,
		sourceFile: sourceFile,
		hlsDir:     filepath.Join(filepath.Dir(sourceFile), ".localsync-hls"),
	}
}

// resolveQualities returns the ordered list of non-passthrough quality presets
// matching hls.qualities config. Falls back to all non-passthrough if empty.
func (m *HLSManager) resolveQualities() []QualityPreset {
	var result []QualityPreset
	if len(m.cfg.HLS.Qualities) > 0 {
		for _, name := range m.cfg.HLS.Qualities {
			if p := m.cfg.FindQuality(name); p != nil && !p.Passthrough {
				result = append(result, *p)
			}
		}
	} else {
		for _, q := range m.cfg.Quality {
			if !q.Passthrough {
				result = append(result, q)
			}
		}
	}
	return result
}

// resolveQualityNames returns the names of resolved HLS qualities.
func (m *HLSManager) resolveQualityNames() []string {
	qualities := m.resolveQualities()
	names := make([]string, len(qualities))
	for i, q := range qualities {
		names[i] = q.Name
	}
	return names
}

// effectiveSegmentType returns the HLS segment type, auto-detecting fmp4 for AV1 codecs.
func (m *HLSManager) effectiveSegmentType() string {
	st := m.cfg.HLS.SegmentType
	if st != "" && st != "mpegts" {
		return st
	}
	codec := strings.ToLower(m.cfg.Transcode.VideoCodec)
	if strings.Contains(codec, "av1") || strings.Contains(codec, "svt") {
		return "fmp4"
	}
	return st
}

// segmentExtension returns the file extension for HLS segments.
func (m *HLSManager) segmentExtension() string {
	if m.effectiveSegmentType() == "fmp4" {
		return ".m4s"
	}
	return ".ts"
}

// HasCompleteCache returns true if a complete, non-stale multi-variant HLS cache exists.
func (m *HLSManager) HasCompleteCache() bool {
	masterPath := filepath.Join(m.hlsDir, "master.m3u8")

	info, err := os.Stat(masterPath)
	if err != nil {
		return false
	}

	// Check staleness
	srcInfo, err := os.Stat(m.sourceFile)
	if err != nil {
		return false
	}
	if srcInfo.ModTime().After(info.ModTime()) {
		return false
	}

	// Parse master.m3u8 for variant playlist references
	data, err := os.ReadFile(masterPath)
	if err != nil {
		return false
	}

	foundVariant := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".m3u8") {
			foundVariant = true
			varPath := filepath.Join(m.hlsDir, line)
			varData, err := os.ReadFile(varPath)
			if err != nil {
				return false
			}
			if !strings.Contains(string(varData), "#EXT-X-ENDLIST") {
				return false
			}
		}
	}
	return foundVariant
}

// qualityVariantIndex returns the variant stream index for a quality name,
// based on the resolved qualities order. Returns -1 if not found.
func (m *HLSManager) qualityVariantIndex(quality string) int {
	for i, q := range m.resolveQualities() {
		if q.Name == quality {
			return i
		}
	}
	return -1
}

// GenerateAll encodes all configured quality variants in a single FFmpeg command.
// Runs synchronously — call in a goroutine for background generation.
func (m *HLSManager) GenerateAll() error {
	qualities := m.resolveQualities()
	if len(qualities) == 0 {
		return fmt.Errorf("no non-passthrough quality presets configured for HLS")
	}

	if m.HasCompleteCache() {
		log.Println("[hls] cache already exists, skipping generation")
		return nil
	}

	m.mu.Lock()
	if m.building {
		m.mu.Unlock()
		return fmt.Errorf("HLS generation already in progress")
	}
	m.building = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.building = false
		m.mu.Unlock()
	}()

	// Clean stale cache
	if err := os.RemoveAll(m.hlsDir); err != nil {
		log.Printf("[hls] warning: failed to clean old cache: %v", err)
	}
	if err := os.MkdirAll(m.hlsDir, 0755); err != nil {
		return fmt.Errorf("failed to create HLS output dir: %w", err)
	}

	args := m.buildFFmpegArgs(qualities)
	log.Printf("[hls] generating %d variants: ffmpeg %s", len(qualities), strings.Join(args, " "))

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg HLS generation failed: %w", err)
	}

	log.Printf("[hls] generation complete for %d variants", len(qualities))
	return nil
}

// buildFFmpegArgs constructs the single FFmpeg command that encodes all variants.
func (m *HLSManager) buildFFmpegArgs(qualities []QualityPreset) []string {
	tc := m.cfg.Transcode
	n := len(qualities)

	var args []string
	args = append(args, "-i", m.sourceFile)

	// Build filter_complex: split source video, scale each variant
	splitLabels := make([]string, n)
	mappedLabels := make([]string, n)
	var filterParts []string

	// Split
	splitExpr := fmt.Sprintf("[0:v]split=%d", n)
	for i := 0; i < n; i++ {
		splitLabels[i] = fmt.Sprintf("[v%d]", i)
		splitExpr += splitLabels[i]
	}
	filterParts = append(filterParts, splitExpr)

	// Scale each variant
	for i, q := range qualities {
		if q.Resolution != "" {
			parts := strings.SplitN(q.Resolution, "x", 2)
			if len(parts) == 2 {
				scaledLabel := fmt.Sprintf("[v%ds]", i)
				filterParts = append(filterParts,
					fmt.Sprintf("%sscale=%s:-2%s", splitLabels[i], parts[0], scaledLabel))
				mappedLabels[i] = scaledLabel
			} else {
				mappedLabels[i] = splitLabels[i]
			}
		} else {
			mappedLabels[i] = splitLabels[i]
		}
	}

	args = append(args, "-filter_complex", strings.Join(filterParts, ";"))

	// Map video + audio streams (one audio per variant to satisfy HLS muxer)
	for i := range qualities {
		args = append(args, "-map", mappedLabels[i])
		args = append(args, "-map", "0:a")
	}

	// Per-variant video encoding
	for i, q := range qualities {
		args = append(args, fmt.Sprintf("-c:v:%d", i), tc.VideoCodec)
		args = append(args, fmt.Sprintf("-b:v:%d", i), q.Bitrate)
		extraArgs := q.ExtraArgs
		if len(extraArgs) == 0 {
			extraArgs = tc.ExtraArgs
		}
		for _, arg := range extraArgs {
			args = append(args, arg)
		}
	}

	// Audio encoding (applied to all audio streams)
	args = append(args, "-c:a", tc.AudioCodec)
	if tc.AudioCodec != "copy" {
		args = append(args, "-b:a", tc.AudioBitrate)
	}

	// HLS output settings
	segType := m.effectiveSegmentType()
	if segType == "fmp4" {
		args = append(args, "-hls_segment_type", "fmp4")
		args = append(args, "-hls_fmp4_init_filename", filepath.Join(m.hlsDir, "init_%v.mp4"))
	}

	segExt := m.segmentExtension()
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", m.cfg.HLS.SegmentDuration),
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(m.hlsDir, "stream_%v_%05d"+segExt),
		"-master_pl_name", "master.m3u8",
	)

	// Build var_stream_map: "v:0,a:0 v:1,a:1 v:2,a:2 ..."
	// Each variant has its own paired audio stream
	var varMap []string
	for i := range qualities {
		varMap = append(varMap, fmt.Sprintf("v:%d,a:%d", i, i))
	}
	args = append(args, "-var_stream_map", strings.Join(varMap, " "))

	args = append(args, filepath.Join(m.hlsDir, "stream_%v.m3u8"))

	return args
}

// ServeHTTP handles requests to /hls/{filename}
func (m *HLSManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/hls/")
	if filename == "" {
		http.NotFound(w, r)
		return
	}

	filePath := filepath.Join(m.hlsDir, filename)

	// Security: ensure resolved path stays within the HLS directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absDir, err := filepath.Abs(m.hlsDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) && absPath != absDir {
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.HasSuffix(filename, ".m3u8"):
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	case strings.HasSuffix(filename, ".ts"):
		w.Header().Set("Content-Type", "video/mp2t")
	case strings.HasSuffix(filename, ".m4s"), strings.HasSuffix(filename, ".mp4"):
		w.Header().Set("Content-Type", "video/mp4")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	http.ServeFile(w, r, filePath)
}
