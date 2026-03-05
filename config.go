package main

import (
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Port    int               `toml:"port"`
	Quality map[string]string `toml:"quality"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	if cfg.Port == 0 {
		cfg.Port = 8080
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
