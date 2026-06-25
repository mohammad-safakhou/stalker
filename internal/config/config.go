package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	AppName         = "stalker"
	DefaultAddr     = "127.0.0.1:18080"
	DefaultSyncAddr = "0.0.0.0:18081"
)

func Addr() string {
	if addr := os.Getenv("STALKER_ADDR"); addr != "" {
		return addr
	}
	return DefaultAddr
}

func SyncAddr() string {
	if addr, ok := os.LookupEnv("STALKER_SYNC_ADDR"); ok {
		return addr
	}
	return DefaultSyncAddr
}

func DataDir() (string, error) {
	if dir := os.Getenv("STALKER_DATA_DIR"); dir != "" {
		return dir, nil
	}
	return DefaultDataDir()
}

func DefaultDataDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", AppName), nil
	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return filepath.Join(dir, AppName), nil
		}
		if dir := os.Getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		return filepath.Join(home, "AppData", "Local", AppName), nil
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		return filepath.Join(home, ".local", "share", AppName), nil
	}
}
