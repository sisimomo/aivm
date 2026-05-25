// vmstate.go tracks lightweight VM lifecycle state on the host filesystem.
// The VMCreatedAtFile records the Unix epoch of VM creation and is used for
// age-based recreation prompts.
package vm

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

// VMCreatedAtFile is the state file that records the Unix epoch of VM creation.
const VMCreatedAtFile = "vm-created-at"

// RecordVMCreation writes the VM creation timestamp used for age-based rotation.
func RecordVMCreation(stateDir string) {
	path := filepath.Join(stateDir, VMCreatedAtFile)
	if err := os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644); err != nil {
		aivmlog.Warn("write %s: %v", VMCreatedAtFile, err)
	}
}
