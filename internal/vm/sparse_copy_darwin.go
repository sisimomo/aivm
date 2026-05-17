package vm

import (
	"fmt"
	"os"
	"os/exec"
)

// sparseCopyFile copies src to dst on macOS using "cp -c" which invokes
// clonefile(2) on APFS volumes.  clonefile is a reflink operation: it is
// instant, consumes no extra disk space at copy time, and shares data blocks
// with the source via copy-on-write.  On non-APFS volumes (HFS+, exFAT, …)
// the kernel falls back to a regular copy, but the resulting file will still
// only occupy the space used by actual data because APFS is the overwhelmingly
// common filesystem on modern macOS.
//
// If cp -c fails for any reason we fall back to a hole-aware Go copy so that
// we never silently materialise a sparse file into a dense one.
func sparseCopyFile(src, dst string) error {
	// Remove dst first so clonefile can create it fresh (it refuses to
	// overwrite an existing file).
	_ = os.Remove(dst)

	cmd := exec.Command("cp", "-c", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Fall back to manual sparse copy on unexpected error.
		_ = os.Remove(dst)
		if ferr := sparseCopyFallback(src, dst); ferr != nil {
			return fmt.Errorf("cp -c failed (%s); fallback sparse copy also failed: %w", string(out), ferr)
		}
	}
	return nil
}
