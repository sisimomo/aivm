package vm

import (
	"os/exec"
)

// ExitCodeFromError returns the exit code from err, or 0 if err is nil.
// Non-exit errors return -1 and the original error.
func ExitCodeFromError(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}
