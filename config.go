package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type TranscodeConfig struct {
	VideoCodec   string   `toml:"video_codec"`
	ExtraArgs    []string `toml:"extra_args"`
	AudioCodec   string   `toml:"audio_codec"`
	AudioBitrate string   `toml:"audio_bitrate"`
	Subtitles    bool     `toml:"subtitles"`
	Realtime     bool     `toml:"realtime"`
	Format       string   `toml:"format"`
}

type Config struct {
	Port       int               `toml:"port"`
	MaxClients int               `toml:"max_clients"`
	Quality    map[string]string `toml:"quality"`
	Transcode  TranscodeConfig   `toml:"transcode"`
}

const defaultConfig = `port = 13771

[transcode]
video_codec = "libx264"
extra_args = ["-preset", "ultrafast", "-tune", "zerolatency"]
audio_codec = "aac"
audio_bitrate = "128k"
subtitles = true
realtime = true
format = "matroska"

[quality]
source = "passthrough"
high = "8000k"
mid = "3000k"
low = "1000k"
`

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return cfg, err
		}
		if err := os.WriteFile(path, []byte(defaultConfig), 0644); err != nil {
			return cfg, err
		}
		data = []byte(defaultConfig)
	} else if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	if cfg.Port == 0 {
		cfg.Port = 13771
	}
	if cfg.MaxClients == 0 && !hasKey(data, "max_clients") {
		cfg.MaxClients = 1
	}
	if cfg.Quality == nil {
		cfg.Quality = make(map[string]string)
	}

	defaults := map[string]string{
		"source": "passthrough",
		"high":   "8000k",
		"mid":    "3000k",
		"low":    "1000k",
	}
	for k, v := range defaults {
		if _, ok := cfg.Quality[k]; !ok {
			cfg.Quality[k] = v
		}
	}

	if cfg.Transcode.VideoCodec == "" {
		cfg.Transcode.VideoCodec = "libx264"
	}
	if cfg.Transcode.ExtraArgs == nil {
		cfg.Transcode.ExtraArgs = []string{"-preset", "ultrafast", "-tune", "zerolatency"}
	}
	if cfg.Transcode.AudioCodec == "" {
		cfg.Transcode.AudioCodec = "aac"
	}
	if cfg.Transcode.AudioBitrate == "" {
		cfg.Transcode.AudioBitrate = "128k"
	}
	if !hasTranscodeKey(data, "subtitles") {
		cfg.Transcode.Subtitles = true
	}
	if !hasTranscodeKey(data, "realtime") {
		cfg.Transcode.Realtime = true
	}
	if cfg.Transcode.Format == "" {
		cfg.Transcode.Format = "matroska"
	}

	return cfg, nil
}

// hasKey checks whether a TOML key is explicitly present in the raw data.
func hasKey(data []byte, key string) bool {
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

// hasTranscodeKey checks whether a key is explicitly set inside the [transcode] table.
func hasTranscodeKey(data []byte, key string) bool {
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	tc, ok := raw["transcode"]
	if !ok {
		return false
	}
	m, ok := tc.(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = m[key]
	return ok
}
