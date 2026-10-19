package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is read from <install dir>\config.json (shipped by the installer)
// and then from %APPDATA%\ImprentaLabs\config.json (user overrides).
type Config struct {
	// Sites allowed without asking. Wildcards allowed: "https://*.miempresa.com"
	AllowedOrigins []string `json:"allowed_origins"`
	// Optional logo (PNG/JPG) printed at the top of tickets that request "logo": true
	Logo string `json:"logo"`
	// 576 = 80 mm paper, 384 = 58 mm paper
	WidthDots         int `json:"width_dots"`
	RetryEverySeconds int `json:"retry_every_seconds"`
	MaxAgeHours       int `json:"max_age_hours"`
	// Optional path to msedge.exe / chrome.exe used to render HTML tickets
	BrowserPath string `json:"browser_path"`
}

var cfg = Config{WidthDots: 576, RetryEverySeconds: 15, MaxAgeHours: 24}

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func mergeConfig(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c Config
	if json.Unmarshal(b, &c) != nil {
		return
	}
	cfg.AllowedOrigins = append(cfg.AllowedOrigins, c.AllowedOrigins...)
	if c.Logo != "" {
		cfg.Logo = c.Logo
	}
	if c.WidthDots > 0 {
		cfg.WidthDots = c.WidthDots
	}
	if c.RetryEverySeconds > 0 {
		cfg.RetryEverySeconds = c.RetryEverySeconds
	}
	if c.MaxAgeHours > 0 {
		cfg.MaxAgeHours = c.MaxAgeHours
	}
	if c.BrowserPath != "" {
		cfg.BrowserPath = c.BrowserPath
	}
}

func loadConfig() {
	mergeConfig(filepath.Join(exeDir(), "config.json"))
	user := filepath.Join(dataDir(), "config.json")
	if _, err := os.Stat(user); err != nil {
		tpl := Config{AllowedOrigins: []string{}, WidthDots: 576, RetryEverySeconds: 15, MaxAgeHours: 24}
		b, _ := json.MarshalIndent(tpl, "", "  ")
		_ = os.WriteFile(user, b, 0o644)
	}
	mergeConfig(user)
	if cfg.Logo != "" && !filepath.IsAbs(cfg.Logo) {
		cfg.Logo = filepath.Join(exeDir(), cfg.Logo)
	}
}
