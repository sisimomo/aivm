package vm

import (
	"os"
	"path/filepath"
	"runtime"
)

const dockerVMHome = "/home/user"

// DefaultUserHome returns the VM user home directory used when expanding ~ in
// mount target paths.
func DefaultUserHome(backend, hostHome string) string {
	switch backend {
	case "docker":
		return dockerVMHome
	default:
		u := os.Getenv("USER")
		if u == "" {
			u = filepath.Base(hostHome)
			if u == "" || u == "." || u == string(filepath.Separator) {
				u = "user"
			}
		}
		if runtime.GOOS == "darwin" {
			// Lima's instance user home is /home/$USER.guest (not .linux).
			return filepath.Join("/home", u+".guest")
		}
		return filepath.Join("/home", u)
	}
}
