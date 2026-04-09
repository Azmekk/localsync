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

type QualityPreset struct {
	Name        string   `toml:"name"`
	Bitrate     string   `toml:"bitrate"`
	Resolution  string   `toml:"resolution"`
	Passthrough bool     `toml:"passthrough"`
	ExtraArgs   []string `toml:"extra_args"`
}

type Config struct {
	Port       int             `toml:"port"`
	MaxClients int             `toml:"max_clients"`
	Quality    []QualityPreset `toml:"quality"`
	Transcode  TranscodeConfig `toml:"transcode"`
}

func (c *Config) FindQuality(name string) *QualityPreset {
	for i := range c.Quality {
		if c.Quality[i].Name == name {
			return &c.Quality[i]
		}
	}
	return nil
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
	if len(cfg.Quality) == 0 {
		cfg.Quality = []QualityPreset{
			{Name: "source", Passthrough: true},
			{Name: "1080p", Bitrate: "8000k", Resolution: "1920x1080"},
			{Name: "720p", Bitrate: "3000k", Resolution: "1280x720"},
			{Name: "480p", Bitrate: "1000k", Resolution: "854x480"},
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

func hasKey(data []byte, key string) bool {
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

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
