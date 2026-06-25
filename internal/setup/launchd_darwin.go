//go:build darwin

package setup

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const launchAgentLabel = "com.mohammad-safakhou.stalker"

func LaunchAgentSupported() bool {
	return true
}

func InstallLaunchAgent(out io.Writer, start bool) error {
	if out == nil {
		out = os.Stdout
	}
	plistPath, err := LaunchAgentPath()
	if err != nil {
		return err
	}
	logDir, stdoutPath, stderrPath, err := launchAgentLogPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	program, err := installedExecutablePath()
	if err != nil {
		return err
	}
	plist := renderLaunchAgentPlist(program, stdoutPath, stderrPath)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write launch agent: %w", err)
	}
	fmt.Fprintf(out, "LaunchAgent installed: %s\n", plistPath)
	fmt.Fprintf(out, "LaunchAgent logs: %s\n", logDir)
	if !start {
		fmt.Fprintln(out, "LaunchAgent start: skipped")
		return nil
	}
	return StartLaunchAgent(out)
}

func UninstallLaunchAgent(out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	_ = runLaunchctl(out, "bootout", launchAgentDomain()+"/"+launchAgentLabel)
	plistPath, err := LaunchAgentPath()
	if err != nil {
		return err
	}
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launch agent: %w", err)
	}
	fmt.Fprintf(out, "LaunchAgent removed: %s\n", plistPath)
	return nil
}

func StartLaunchAgent(out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	plistPath, err := LaunchAgentPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("launch agent is not installed at %s: %w", plistPath, err)
	}
	domain := launchAgentDomain()
	if err := runLaunchctl(out, "bootstrap", domain, plistPath); err != nil {
		if !strings.Contains(err.Error(), "Bootstrap failed: 5") && !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	if err := runLaunchctl(out, "enable", domain+"/"+launchAgentLabel); err != nil {
		return err
	}
	if err := runLaunchctl(out, "kickstart", domain+"/"+launchAgentLabel); err != nil {
		return err
	}
	fmt.Fprintln(out, "LaunchAgent started")
	return nil
}

func StopLaunchAgent(out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	if err := runLaunchctl(out, "bootout", launchAgentDomain()+"/"+launchAgentLabel); err != nil {
		return err
	}
	fmt.Fprintln(out, "LaunchAgent stopped")
	return nil
}

func RestartLaunchAgent(out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	_ = runLaunchctl(out, "bootout", launchAgentDomain()+"/"+launchAgentLabel)
	if err := StartLaunchAgent(out); err != nil {
		return err
	}
	fmt.Fprintln(out, "LaunchAgent restarted")
	return nil
}

func LaunchAgentStatus(out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	return runLaunchctl(out, "print", launchAgentDomain()+"/"+launchAgentLabel)
}

func LaunchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}

func installedExecutablePath() (string, error) {
	if path, err := exec.LookPath("stalker"); err == nil {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved, nil
		}
		return path, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path, nil
}

func launchAgentLogPaths() (string, string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", fmt.Errorf("find home directory: %w", err)
	}
	logDir := filepath.Join(home, "Library", "Logs", "stalker")
	return logDir, filepath.Join(logDir, "stalker.out.log"), filepath.Join(logDir, "stalker.err.log"), nil
}

func launchAgentDomain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func runLaunchctl(out io.Writer, args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), msg)
	}
	if stderr.Len() > 0 {
		fmt.Fprint(out, stderr.String())
	}
	return nil
}

func renderLaunchAgentPlist(program, stdoutPath, stderrPath string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	plistKeyString(&b, "Label", launchAgentLabel)
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	plistString(&b, program)
	plistString(&b, "serve")
	b.WriteString("\t</array>\n")
	plistKeyString(&b, "StandardOutPath", stdoutPath)
	plistKeyString(&b, "StandardErrorPath", stderrPath)
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Background</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func plistKeyString(b *strings.Builder, key, value string) {
	b.WriteString("\t<key>")
	_ = xml.EscapeText(b, []byte(key))
	b.WriteString("</key>\n")
	plistString(b, value)
}

func plistString(b *strings.Builder, value string) {
	b.WriteString("\t<string>")
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</string>\n")
}
