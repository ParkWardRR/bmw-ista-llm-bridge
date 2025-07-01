package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	Window   WindowConfig  `toml:"window"`
	Capture  CaptureConfig `toml:"capture"`
	Encoding EncodingConfig `toml:"encoding"`
	Output   OutputConfig  `toml:"output"`
	Logging  LoggingConfig `toml:"logging"`
	ISTA     ISTAConfig    `toml:"ista"`
}

type WindowConfig struct {
	Title string `toml:"title"`
}

type CaptureConfig struct {
	PollMs         int     `toml:"poll_ms"`
	DebounceMs     int     `toml:"debounce_ms"`
	Threshold      float64 `toml:"threshold"`
	PHashThreshold int     `toml:"phash_threshold"`
}

type EncodingConfig struct {
	Quality int `toml:"quality"`
	Preset  int `toml:"preset"`
}

type OutputConfig struct {
	Directory      string `toml:"directory"`
	SessionFolders bool   `toml:"session_folders"`
}

type LoggingConfig struct {
	Level      string `toml:"level"`
	MaxSizeMB  int    `toml:"max_size_mb"`
	MaxBackups int    `toml:"max_backups"`
	MaxAgeDays int    `toml:"max_age_days"`
	Compress   bool   `toml:"compress"`
}

type ISTAConfig struct {
	InstallDir string `toml:"install_dir"`
	LogDir     string `toml:"log_dir"`
}

func defaultConfig() Config {
	return Config{
		Window: WindowConfig{
			Title: "ISTA",
		},
		Capture: CaptureConfig{
			PollMs:         200,
			DebounceMs:     400,
			Threshold:      0.005,
			PHashThreshold: 3,
		},
		Encoding: EncodingConfig{
			Quality: 14,
			Preset:  4,
		},
		Output: OutputConfig{
			Directory:      "screenshots",
			SessionFolders: true,
		},
		Logging: LoggingConfig{
			Level:      "info",
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 28,
			Compress:   true,
		},
		ISTA: ISTAConfig{
			InstallDir: `C:\EC-APPS\ISTA`,
			LogDir:     `C:\EC-APPS\ISTA\Logs`,
		},
	}
}

func configDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("APPDATA not set")
	}
	return filepath.Join(appData, "ista-bridge"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func loadConfig() Config {
	cfg := defaultConfig()

	path, err := configPath()
	if err != nil {
		log.Printf("Config path: %v, using defaults", err)
		return cfg
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if writeErr := writeDefaultConfig(path); writeErr != nil {
			log.Printf("Could not write default config: %v", writeErr)
		} else {
			log.Printf("Created default config: %s", path)
		}
		return cfg
	}
	if err != nil {
		log.Printf("Read config: %v, using defaults", err)
		return cfg
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		log.Printf("Parse config: %v, using defaults", err)
		return defaultConfig()
	}

	return cfg
}

const defaultConfigTOML = `# ista-bridge configuration
# Location: %APPDATA%\ista-bridge\config.toml

[window]
# Window title substring to match (scored matching: exact > word boundary > substring)
title = "ISTA"

[capture]
# Polling interval in milliseconds
poll_ms = 200
# Settle time after change detected before capturing (ms)
debounce_ms = 400
# Minimum pixel change fraction to trigger capture (0.005 = 0.5%)
threshold = 0.005
# pHash Hamming distance threshold (0-64, lower=stricter; 0 disables pHash pre-filter)
phash_threshold = 3

[encoding]
# Encoder quality (lower = higher quality; CRF for AVIF, 12-16 optimal for text)
quality = 14
# Encoder speed (libaom: 0-6, lower=slower+better; svtav1: 0-12)
preset = 4

[output]
# Base output directory for screenshots
directory = "screenshots"
# Create date-based subdirectories (YYYY-MM-DD/)
session_folders = true

[logging]
# Log level: debug, info, warn, error
level = "info"
# Max log file size in MB before rotation
max_size_mb = 10
# Number of rotated log files to keep
max_backups = 5
# Max age in days for rotated logs
max_age_days = 28
# Compress rotated log files with gzip
compress = true

[ista]
# ISTA installation directory (for log parsing)
install_dir = 'C:\EC-APPS\ISTA'
# ISTA log directory (auto-detected from install_dir if empty)
log_dir = 'C:\EC-APPS\ISTA\Logs'
`

func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfigTOML), 0644)
}
