package vm

import (
	"fmt"
	"strings"
)

// LimaMountYAML returns one Lima mounts[] entry as YAML lines.
// limactl --mount only supports host paths (same guest path); remapped mounts
// must be written into the instance template with mountPoint.
func LimaMountYAML(m Mount) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "- location: %q\n", m.HostPath)
	if m.GuestPath != "" && m.GuestPath != m.HostPath {
		fmt.Fprintf(&sb, "  mountPoint: %q\n", m.GuestPath)
	}
	fmt.Fprintf(&sb, "  writable: %t\n", m.Writable)
	return sb.String()
}

// LimaMountsYAML returns a full mounts: section for a Lima instance template.
func LimaMountsYAML(mounts []Mount) string {
	if len(mounts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("mounts:\n")
	for _, m := range mounts {
		sb.WriteString(LimaMountYAML(m))
	}
	return sb.String()
}

func DockerVolumeFlag(m Mount) string {
	mode := "ro"
	if m.Writable {
		mode = "rw"
	}
	return fmt.Sprintf("%s:%s:%s", m.HostPath, m.GuestPath, mode)
}
