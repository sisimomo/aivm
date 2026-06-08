package vm

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed lima.yaml
var limaTemplate []byte

// LimaTemplatePath writes the embedded template to a temp file for limactl.
// Caller may remove the file after limactl create completes.
func LimaTemplatePath() (string, error) {
	f, err := os.CreateTemp("", "aivm-lima-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create lima template temp file: %w", err)
	}
	if _, err := f.Write(limaTemplate); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write lima template: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
