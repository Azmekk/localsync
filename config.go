package main

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Port       int               `toml:"port"`
	MaxClients int               `toml:"max_clients"`
	Quality    map[string]string `toml:"quality"`
}

const defaultConfig = `port = 13771

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
