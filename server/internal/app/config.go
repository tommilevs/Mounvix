package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Root — опубликованная папка, к которой имеют доступ пользователи.
type Root struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}

// Config — конфиг сервера (config.json).
type Config struct {
	Port    int    `json:"port"`
	DataDir string `json:"data_dir"`
	Lang    string `json:"lang"`
	Roots   []Root `json:"roots"`
}

func DefaultConfig() Config {
	return Config{
		Port:    48100,
		DataDir: "data",
		Lang:    "ru",
		Roots:   []Root{{Name: "home", Path: "/", ReadOnly: false}},
	}
}

// LoadConfig читает config.json; если файла нет — создаёт с дефолтами.
func LoadConfig(path string) (Config, string, error) {
	cfg := DefaultConfig()
	p := path
	if p == "" {
		p = filepath.Join(cfg.DataDir, "config.json")
	}
	b, err := os.ReadFile(p)
	if err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, p, err
		}
		if cfg.Port == 0 {
			cfg.Port = 48100
		}
		if cfg.Lang == "" {
			cfg.Lang = "ru"
		}
		return cfg, p, nil
	}
	if !os.IsNotExist(err) {
		return cfg, p, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return cfg, p, err
	}
	if err := SaveConfig(p, cfg); err != nil {
		return cfg, p, err
	}
	return cfg, p, nil
}

func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
