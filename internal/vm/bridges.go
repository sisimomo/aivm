package vm

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ValidateSocketBridgesForStart enforces backend rules before VM create.
func ValidateSocketBridgesForStart(backend string, bridges []SocketBridge, mounts []Mount) error {
	if len(bridges) == 0 {
		return nil
	}
	mountGuests := make(map[string]bool, len(mounts))
	for _, m := range mounts {
		mountGuests[m.GuestPath] = true
	}
	for _, b := range bridges {
		if mountGuests[b.GuestPath] {
			return fmt.Errorf("socket bridge guest_path %q conflicts with a directory mount target", b.GuestPath)
		}
	}
	switch backend {
	case "docker":
		return validateDockerSocketBridges(bridges)
	default: // lima or empty
		return validateLimaSocketBridges(bridges)
	}
}

func validateDockerSocketBridges(bridges []SocketBridge) error {
	for _, b := range bridges {
		fi, err := os.Stat(b.HostPath)
		if err != nil {
			return fmt.Errorf("socket bridge host_path %q must exist before docker create: %w", b.HostPath, err)
		}
		if fi.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket bridge host_path %q must be a Unix socket (not a file or directory)", b.HostPath)
		}
	}
	return nil
}

func validateLimaSocketBridges(bridges []SocketBridge) error {
	for _, b := range bridges {
		parent := filepath.Dir(b.HostPath)
		fi, err := os.Stat(parent)
		if err != nil {
			return fmt.Errorf("socket bridge host_path %q: parent directory %q not accessible: %w", b.HostPath, parent, err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("socket bridge host_path %q: parent %q is not a directory", b.HostPath, parent)
		}
		if sockFi, err := os.Stat(b.HostPath); err != nil {
			slog.Warn(fmt.Sprintf("socket bridge host_path %q does not exist yet; Lima forward may fail until the host daemon creates it", b.HostPath))
		} else if sockFi.Mode()&os.ModeSocket == 0 {
			slog.Warn(fmt.Sprintf("socket bridge host_path %q exists but is not a Unix socket yet", b.HostPath))
		}
	}
	return nil
}
