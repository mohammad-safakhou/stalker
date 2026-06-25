package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigureCodexSetsTopLevelOpenAIBaseURL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CODEX_HOME", tmp)
	configPath := filepath.Join(tmp, "config.toml")
	raw := "model = \"gpt-5\"\n\n[projects.\"/tmp\"]\ntrust_level = \"trusted\"\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, path, err := ConfigureCodex()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("ConfigureCodex() changed = false, want true")
	}
	if path != configPath {
		t.Fatalf("path = %q, want %q", path, configPath)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, "openai_base_url = \"http://127.0.0.1:18080/v1\"\n[projects.") {
		t.Fatalf("config was not updated at top level:\n%s", text)
	}
	if _, err := os.Stat(configPath + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestConfigureCodexIsNoopWhenAlreadyConfigured(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CODEX_HOME", tmp)
	configPath := filepath.Join(tmp, "config.toml")
	raw := "openai_base_url = \"http://127.0.0.1:18080/v1\"\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, _, err := ConfigureCodex()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("ConfigureCodex() changed = true, want false")
	}
}

func TestInstallRejectsUnknownService(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "LocalAppData"))
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg-data"))

	var out bytes.Buffer
	err := Install(InstallOptions{
		In:      strings.NewReader(""),
		Out:     &out,
		Service: "claude",
	})
	if err == nil {
		t.Fatal("Install() error = nil, want unknown service error")
	}
}

func TestInstallRejectsUnknownRunner(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "LocalAppData"))
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg-data"))

	var out bytes.Buffer
	err := Install(InstallOptions{
		In:      strings.NewReader(""),
		Out:     &out,
		Runner:  "systemd",
		Service: ServiceNone,
	})
	if err == nil {
		t.Fatal("Install() error = nil, want unknown runner error")
	}
}

func TestInstallLaunchdRunnerWithoutStarting(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd is macOS-only")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PATH", "/usr/bin:/bin")

	var out bytes.Buffer
	err := Install(InstallOptions{
		In:            strings.NewReader(""),
		Out:           &out,
		Runner:        RunnerLaunchd,
		Service:       ServiceNone,
		NoStartRunner: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	plistPath := filepath.Join(tmp, "Library", "LaunchAgents", "com.mohammad-safakhou.stalker.plist")
	raw, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("read launch agent: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"<key>Label</key>",
		"<string>com.mohammad-safakhou.stalker</string>",
		"<string>serve</string>",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(out.String(), "LaunchAgent start: skipped") {
		t.Fatalf("install output did not mention skipped start:\n%s", out.String())
	}
}
