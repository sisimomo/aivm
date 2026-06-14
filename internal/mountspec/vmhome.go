package mountspec

import (
	"os"
	"path/filepath"
	"runtime"
)

const dockerVMHome = "/home/user"

// DefaultVMHome returns the VM user home directory used when expanding ~ in
// mount target paths.
func DefaultVMHome(backend, hostHome string) string {
	switch backend {
	case "docker":
		return dockerVMHome
	default:
		u := os.Getenv("USER")
		if u == "" {
			return hostHome
		}
		if runtime.GOOS == "darwin" {
			// Lima's instance user home is /home/$USER.guest (not .linux).
			return filepath.Join("/home", u+".guest")
		}
		return filepath.Join("/home", u)
	}
}
