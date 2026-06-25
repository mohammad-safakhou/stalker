//go:build !darwin

package setup

import (
	"fmt"
	"io"
)

func LaunchAgentSupported() bool {
	return false
}

func InstallLaunchAgent(io.Writer, bool) error {
	return fmt.Errorf("launchd runner is only supported on macOS")
}

func UninstallLaunchAgent(io.Writer) error {
	return fmt.Errorf("launchd runner is only supported on macOS")
}

func StartLaunchAgent(io.Writer) error {
	return fmt.Errorf("launchd runner is only supported on macOS")
}

func StopLaunchAgent(io.Writer) error {
	return fmt.Errorf("launchd runner is only supported on macOS")
}

func RestartLaunchAgent(io.Writer) error {
	return fmt.Errorf("launchd runner is only supported on macOS")
}

func LaunchAgentStatus(io.Writer) error {
	return fmt.Errorf("launchd runner is only supported on macOS")
}

func LaunchAgentPath() (string, error) {
	return "", fmt.Errorf("launchd runner is only supported on macOS")
}
