package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mohammad-safakhou/stalker/internal/config"
)

const (
	LegacyDir     = ".stalker"
	ServiceCodex  = "codex"
	ServiceNone   = "none"
	proxyBaseURL  = "http://" + config.DefaultAddr + "/v1"
	installTarget = "github.com/mohammad-safakhou/stalker/cmd/stalker@latest"
)

type InstallOptions struct {
	In      io.Reader
	Out     io.Writer
	Service string
	Migrate bool
	Yes     bool
}

func Install(opts InstallOptions) error {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	dataDir, err := config.DefaultDataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	fmt.Fprintf(out, "Data directory: %s\n", dataDir)

	if err := migrateLegacyData(out, dataDir, opts.Migrate); err != nil {
		return err
	}

	service := opts.Service
	if service == "" && !opts.Yes {
		var err error
		service, err = promptService(in, out)
		if err != nil {
			return err
		}
	}
	if service == "" {
		service = ServiceNone
	}
	switch service {
	case ServiceNone:
		fmt.Fprintln(out, "Service setup: skipped")
	case ServiceCodex:
		changed, path, err := ConfigureCodex()
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(out, "Codex configured: %s\n", path)
		} else {
			fmt.Fprintf(out, "Codex already configured: %s\n", path)
		}
	default:
		return fmt.Errorf("unknown service %q; use %q or %q", service, ServiceCodex, ServiceNone)
	}
	return nil
}

func Upgrade(out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	cmd := exec.Command("go", "install", installTarget)
	cmd.Stdout = out
	cmd.Stderr = out
	fmt.Fprintf(out, "Upgrading with: %s\n", strings.Join(cmd.Args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	fmt.Fprintln(out, "Upgrade complete")
	return nil
}

func promptService(in io.Reader, out io.Writer) (string, error) {
	fmt.Fprintf(out, "Configure a service to use Stalker? [%s/%s] (%s): ", ServiceCodex, ServiceNone, ServiceNone)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return ServiceNone, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer == "" {
		return ServiceNone, nil
	}
	return answer, nil
}

func migrateLegacyData(out io.Writer, dataDir string, force bool) error {
	legacyDB := filepath.Join(LegacyDir, "stalker.sqlite")
	if !fileExists(legacyDB) {
		return nil
	}
	destDB := filepath.Join(dataDir, "stalker.sqlite")
	if fileExists(destDB) && !force {
		fmt.Fprintf(out, "Legacy data found at %s, but %s already exists. Re-run with --migrate to back up the destination and move legacy data.\n", LegacyDir, dataDir)
		return nil
	}

	if fileExists(destDB) {
		backup := dataDir + ".backup-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.Rename(dataDir, backup); err != nil {
			return fmt.Errorf("back up existing data directory: %w", err)
		}
		fmt.Fprintf(out, "Backed up existing data directory: %s\n", backup)
	}
	if err := os.MkdirAll(filepath.Dir(dataDir), 0o755); err != nil {
		return fmt.Errorf("create app data parent: %w", err)
	}
	if err := os.Rename(LegacyDir, dataDir); err != nil {
		return fmt.Errorf("move legacy data from %s to %s: %w", LegacyDir, dataDir, err)
	}
	fmt.Fprintf(out, "Moved legacy data from %s to %s\n", LegacyDir, dataDir)
	return nil
}

func ConfigureCodex() (bool, string, error) {
	path, err := codexConfigPath()
	if err != nil {
		return false, "", err
	}
	changed, err := setTopLevelString(path, "openai_base_url", proxyBaseURL)
	return changed, path, err
}

func codexConfigPath() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

func setTopLevelString(path, key, value string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create config directory: %w", err)
	}
	mode := os.FileMode(0o600)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("read config: %w", err)
		}
		line := fmt.Sprintf("%s = %q\n", key, value)
		return true, os.WriteFile(path, []byte(line), mode)
	}
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode()
	}

	lines := strings.SplitAfter(string(raw), "\n")
	keyRE := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	replacement := fmt.Sprintf("%s = %q\n", key, value)
	inTopLevel := true
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTopLevel = false
		}
		if inTopLevel && keyRE.MatchString(line) {
			if line == replacement {
				return false, nil
			}
			lines[i] = replacement
			if err := backupFile(path, raw, mode); err != nil {
				return false, err
			}
			return true, os.WriteFile(path, []byte(strings.Join(lines, "")), mode)
		}
	}

	insertAt := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			insertAt = i
			break
		}
	}
	updated := append([]string{}, lines[:insertAt]...)
	if insertAt > 0 && strings.TrimSpace(lines[insertAt-1]) != "" {
		updated = append(updated, "\n")
	}
	updated = append(updated, replacement)
	if insertAt < len(lines) {
		updated = append(updated, lines[insertAt:]...)
	}
	if err := backupFile(path, raw, mode); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(strings.Join(updated, "")), mode)
}

func backupFile(path string, raw []byte, mode os.FileMode) error {
	backup := path + ".bak"
	if err := os.WriteFile(backup, raw, mode); err != nil {
		return fmt.Errorf("write config backup: %w", err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
