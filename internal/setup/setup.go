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
	RunnerLaunchd = "launchd"
	RunnerNone    = "none"
	ServiceCodex  = "codex"
	ServiceNone   = "none"
	proxyBaseURL  = "http://" + config.DefaultAddr + "/v1"
	installTarget = "github.com/mohammad-safakhou/stalker/cmd/stalker@latest"
)

type InstallOptions struct {
	In            io.Reader
	Out           io.Writer
	Runner        string
	Service       string
	Migrate       bool
	NoStartRunner bool
	Yes           bool
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

	runner := opts.Runner
	startRunner := false
	if runner == "" && !opts.Yes {
		var err error
		runner, startRunner, err = promptRunner(in, out)
		if err != nil {
			return err
		}
	}
	if runner == "" {
		if opts.Yes && LaunchAgentSupported() {
			runner = RunnerLaunchd
			startRunner = true
		} else {
			runner = RunnerNone
		}
	} else if runner == RunnerLaunchd {
		startRunner = true
	}
	if opts.NoStartRunner {
		startRunner = false
	}
	switch runner {
	case RunnerNone:
		fmt.Fprintln(out, "Background runner: skipped")
	case RunnerLaunchd:
		if err := InstallLaunchAgent(out, startRunner); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown runner %q; use %q or %q", runner, RunnerLaunchd, RunnerNone)
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

type UpgradeOptions struct {
	Out           io.Writer
	RestartRunner bool
}

func Upgrade(opts UpgradeOptions) error {
	out := opts.Out
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
	if opts.RestartRunner {
		if err := RestartLaunchAgent(out); err != nil {
			return err
		}
	}
	return nil
}

func promptRunner(in io.Reader, out io.Writer) (string, bool, error) {
	if !LaunchAgentSupported() {
		return RunnerNone, false, nil
	}
	fmt.Fprintf(out, "Install Stalker as a background runner? [%s/%s] (%s): ", RunnerLaunchd, RunnerNone, RunnerLaunchd)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", false, err
		}
		return RunnerNone, false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer == "" {
		answer = RunnerLaunchd
	}
	if answer != RunnerLaunchd {
		return answer, false, nil
	}
	fmt.Fprintf(out, "Start the background runner now? [yes/no] (yes): ")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", false, err
		}
		return RunnerLaunchd, true, nil
	}
	start := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return RunnerLaunchd, start == "" || start == "y" || start == "yes", nil
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
