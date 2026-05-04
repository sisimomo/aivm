package agent

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// LoadDefs returns the built-in agent definitions from the embedded defaults.yaml.
// Each definition describes how to install, check, and configure an agent in the VM.
func LoadDefs() (map[string]Def, error) {
	var defs map[string]Def
	if err := yaml.Unmarshal(defaultsYAML, &defs); err != nil {
		return nil, err
	}
	return defs, nil
}
