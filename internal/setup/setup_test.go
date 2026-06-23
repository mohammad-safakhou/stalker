package setup

import (
	"bytes"
	"os"
	"path/filepath"
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
