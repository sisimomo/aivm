package mountspec

import (
	"os"
	"path/filepath"
	"runtime"
)

const dockerGuestHome = "/home/user"

// DefaultGuestHome returns the default guest user home directory for mount
// target expansion when vm.guest_home is not set.
func DefaultGuestHome(backend, hostHome string) string {
	switch backend {
	case "docker":
		return dockerGuestHome
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
