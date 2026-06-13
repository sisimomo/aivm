package vm

import "fmt"

func LimaMountFlag(m Mount) string {
	flag := fmt.Sprintf("type=bind,source=%s,target=%s", m.HostPath, m.GuestPath)
	if !m.Writable {
		flag += ",readonly"
	}
	return flag
}

func DockerVolumeFlag(m Mount) string {
	mode := "ro"
	if m.Writable {
		mode = "rw"
	}
	return fmt.Sprintf("%s:%s:%s", m.HostPath, m.GuestPath, mode)
}
