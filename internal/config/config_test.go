package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestAddrUsesEnvOverride(t *testing.T) {
	t.Setenv("STALKER_ADDR", "127.0.0.1:19090")

	if got := Addr(); got != "127.0.0.1:19090" {
		t.Fatalf("Addr() = %q, want env override", got)
	}
}

func TestDataDirUsesEnvOverride(t *testing.T) {
	t.Setenv("STALKER_DATA_DIR", "/tmp/stalker-test")

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/stalker-test" {
		t.Fatalf("DataDir() = %q, want env override", got)
	}
}

func TestDefaultDataDirUsesPlatformAppDataLocation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "LocalAppData"))
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg-data"))

	got, err := DefaultDataDir()
	if err != nil {
		t.Fatal(err)
	}

	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(tmp, "Library", "Application Support", AppName)
	case "windows":
		want = filepath.Join(tmp, "LocalAppData", AppName)
	default:
		want = filepath.Join(tmp, "xdg-data", AppName)
	}
	if got != want {
		t.Fatalf("DefaultDataDir() = %q, want %q", got, want)
	}
}
