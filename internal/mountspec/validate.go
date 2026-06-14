package mountspec

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func ValidateDuplicateTargets(mounts []ResolvedMount) error {
	seen := make(map[string]string, len(mounts))
	for _, m := range mounts {
		if prev, ok := seen[m.GuestPath]; ok {
			return fmt.Errorf(
				"duplicate target %q (from %q and %q)",
				m.GuestPath, prev, m.HostPath)
		}
		seen[m.GuestPath] = m.HostPath
	}
	return nil
}

func ValidateOverlappingSources(mounts []ResolvedMount) error {
	paths := make([]string, len(mounts))
	for i, m := range mounts {
		paths[i] = m.HostPath
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) < len(paths[j]) })
	sep := string(os.PathSeparator)
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[i] == paths[j] {
				return fmt.Errorf("duplicate vm.mounts source %q", paths[i])
			}
			if strings.HasPrefix(paths[j], paths[i]+sep) {
				return fmt.Errorf("overlapping vm.mounts sources %q and %q", paths[i], paths[j])
			}
		}
	}
	return nil
}
