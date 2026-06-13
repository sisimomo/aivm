package lifecycle

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sisimomo/aivm/internal/config"
)

func GuestPathForHost(hostPath string, cfg *config.Config) (string, error) {
	hostPath = filepath.Clean(hostPath)
	type match struct {
		hostLen int
		mount   config.Mount
	}
	var matches []match
	for _, m := range cfg.VM.ParsedMounts {
		realLoc := m.HostPath
		if resolved, err := filepath.EvalSymlinks(m.HostPath); err == nil {
			realLoc = filepath.Clean(resolved)
		}
		if PathUnderMount(hostPath, realLoc) {
			matches = append(matches, match{
				hostLen: len(realLoc), mount: m,
			})
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf(
			"internal error: host path %q is not under any vm.mounts location",
			hostPath)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].hostLen > matches[j].hostLen
	})
	best := matches[0].mount
	realLoc := best.HostPath
	if resolved, err := filepath.EvalSymlinks(best.HostPath); err == nil {
		realLoc = filepath.Clean(resolved)
	}
	rel := strings.TrimPrefix(hostPath, realLoc)
	rel = strings.TrimPrefix(rel, string(filepath.Separator))
	if rel == "" {
		return best.GuestPath, nil
	}
	return filepath.Join(best.GuestPath, rel), nil
}
