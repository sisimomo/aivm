package vm

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed lima.yaml
var limaTemplate []byte

// LimaTemplatePath writes the embedded template plus mount entries to a temp file
// for limactl create. Caller may remove the file after limactl create completes.
func LimaTemplatePath(mounts []Mount, bridges []SocketBridge) (string, error) {
	f, err := os.CreateTemp("", "aivm-lima-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create lima template temp file: %w", err)
	}
	if _, err := f.Write(limaTemplate); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write lima template: %w", err)
	}
	if yaml := LimaMountsYAML(mounts); yaml != "" {
		if _, err := f.WriteString("\n" + yaml); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", fmt.Errorf("write lima mounts: %w", err)
		}
	}
	if yaml := LimaPortForwardsYAML(bridges); yaml != "" {
		if _, err := f.WriteString("\n" + yaml); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", fmt.Errorf("write lima portForwards: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
