package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type BatchPreset struct {
	Name       string   `toml:"name"`
	Bitrate    string   `toml:"bitrate"`
	Resolution string   `toml:"resolution"`
	ExtraArgs  []string `toml:"extra_args"`
}

type BatchTranscode struct {
	VideoCodec   string        `toml:"video_codec"`
	ExtraArgs    []string      `toml:"extra_args"`
	AudioCodec   string        `toml:"audio_codec"`
	AudioBitrate string        `toml:"audio_bitrate"`
	Subtitles    bool          `toml:"subtitles"`
	Format       string        `toml:"format"`
	Preset       []BatchPreset `toml:"preset"`
}

func (c *BatchTranscode) FindPreset(name string) *BatchPreset {
	for i := range c.Preset {
		if c.Preset[i].Name == name {
			return &c.Preset[i]
		}
	}
	return nil
}

const localsyncDefaultsSection = `port = 13771

[transcode]
video_codec = "libx264"
extra_args = ["-preset", "ultrafast", "-tune", "zerolatency"]
audio_codec = "aac"
audio_bitrate = "128k"
subtitles = true
realtime = true
format = "matroska"

[[quality]]
name = "source"
passthrough = true

[[quality]]
name = "1080p"
bitrate = "8000k"
resolution = "1920x1080"

[[quality]]
name = "720p"
bitrate = "3000k"
resolution = "1280x720"

[[quality]]
name = "480p"
bitrate = "1000k"
resolution = "854x480"
`

const batchcompressDefaultsSection = `[batchcompress]
video_codec = "libx264"
extra_args = ["-preset", "slow", "-crf", "20"]
audio_codec = "aac"
audio_bitrate = "128k"
subtitles = true
format = "matroska"

[[batchcompress.preset]]
name = "1080p"
bitrate = "8000k"
resolution = "1920x1080"

[[batchcompress.preset]]
name = "720p"
bitrate = "3000k"
resolution = "1280x720"

[[batchcompress.preset]]
name = "480p"
bitrate = "1000k"
resolution = "854x480"
`

const defaultConfigFile = localsyncDefaultsSection + "\n" + batchcompressDefaultsSection

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(dir, "localsync", "config.toml")
}

func defaultBatchTranscode() BatchTranscode {
	return BatchTranscode{
		VideoCodec:   "libx264",
		ExtraArgs:    []string{"-preset", "slow", "-crf", "20"},
		AudioCodec:   "aac",
		AudioBitrate: "128k",
		Subtitles:    true,
		Format:       "matroska",
		Preset: []BatchPreset{
			{Name: "1080p", Bitrate: "8000k", Resolution: "1920x1080"},
			{Name: "720p", Bitrate: "3000k", Resolution: "1280x720"},
			{Name: "480p", Bitrate: "1000k", Resolution: "854x480"},
		},
	}
}

func LoadBatchConfig(path string) (BatchTranscode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return BatchTranscode{}, err
		}
		if err := os.WriteFile(path, []byte(defaultConfigFile), 0644); err != nil {
			return BatchTranscode{}, err
		}
		data = []byte(defaultConfigFile)
	} else if err != nil {
		return BatchTranscode{}, err
	}

	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return BatchTranscode{}, err
	}
	if _, ok := raw["batchcompress"]; !ok {
		if err := appendBatchSection(path, data); err != nil {
			log.Printf("warning: could not append [batchcompress] to %s: %v; using in-memory defaults", path, err)
			return defaultBatchTranscode(), nil
		}
		log.Printf("added [batchcompress] section to %s", path)
		data, err = os.ReadFile(path)
		if err != nil {
			return BatchTranscode{}, err
		}
	}

	var shape struct {
		Batchcompress BatchTranscode `toml:"batchcompress"`
	}
	if err := toml.Unmarshal(data, &shape); err != nil {
		return BatchTranscode{}, err
	}

	bt := shape.Batchcompress
	def := defaultBatchTranscode()
	if bt.VideoCodec == "" {
		bt.VideoCodec = def.VideoCodec
	}
	if bt.ExtraArgs == nil {
		bt.ExtraArgs = def.ExtraArgs
	}
	if bt.AudioCodec == "" {
		bt.AudioCodec = def.AudioCodec
	}
	if bt.AudioBitrate == "" {
		bt.AudioBitrate = def.AudioBitrate
	}
	if bt.Format == "" {
		bt.Format = def.Format
	}
	if !hasBatchKey(data, "subtitles") {
		bt.Subtitles = true
	}
	if len(bt.Preset) == 0 {
		// User has [batchcompress] globals but no [[batchcompress.preset]] blocks.
		// Synthesize a single empty preset so the global settings are used as-is,
		// without injecting any scaling or bitrate caps.
		bt.Preset = []BatchPreset{{Name: "default"}}
	}
	return bt, nil
}

// appendBatchSection appends the default [batchcompress] block to path,
// preserving every other byte of the existing file. A blank-line separator is
// added if the existing content does not already end with one.
func appendBatchSection(path string, existing []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	var prefix string
	switch {
	case len(existing) == 0:
		prefix = ""
	case !bytes.HasSuffix(existing, []byte("\n")):
		prefix = "\n\n"
	case !bytes.HasSuffix(existing, []byte("\n\n")):
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + batchcompressDefaultsSection)
	return err
}

func hasBatchKey(data []byte, key string) bool {
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	bc, ok := raw["batchcompress"]
	if !ok {
		return false
	}
	m, ok := bc.(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = m[key]
	return ok
}
