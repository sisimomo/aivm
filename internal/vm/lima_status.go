package vm

import "strings"

// ParseLimaListStatus maps limactl list output to Status for the given profile.
func ParseLimaListStatus(lines []string, profile string) Status {
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != profile {
			continue
		}
		switch fields[1] {
		case "Running":
			return StatusRunning
		case "Stopped":
			return StatusStopped
		}
	}
	return StatusNotFound
}
