package config

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DisabledDuration is the sentinel value meaning "this prompt is disabled".
// Stored in RecreatePromptAfterDuration when the raw string is "-1".
const DisabledDuration = time.Duration(-1)

// ParsePromptDuration parses a human-readable duration string used for staleness
// prompt thresholds. Accepted formats:
//   - "-1"           → DisabledDuration (prompt disabled)
//   - "7d", "30d"    → days
//   - "12h", "48h"   → hours
//
// Any other format or unknown unit is a hard error.
func ParsePromptDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "-1" {
		return DisabledDuration, nil
	}
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration %q: use a unit string like \"7d\" or \"12h\", or \"-1\" to disable", s)
	}

	unit := string(s[len(s)-1])
	numStr := s[:len(s)-1]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil || val < 0 {
		return 0, fmt.Errorf("invalid duration %q: use a unit string like \"7d\" or \"12h\", or \"-1\" to disable", s)
	}

	switch unit {
	case "d":
		return time.Duration(val * float64(24*time.Hour)), nil
	case "h":
		return time.Duration(val * float64(time.Hour)), nil
	default:
		return 0, fmt.Errorf("invalid duration %q: unknown unit %q — supported units: \"d\" (days), \"h\" (hours)", s, unit)
	}
}

// ParseResourceBytes parses a human-readable resource size string into bytes.
// Accepted formats: "8GB", "512MB", "1TB" (case-insensitive suffix).
// No integers allowed — units are mandatory.
func ParseResourceBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if len(s) < 3 {
		return 0, fmt.Errorf("invalid resource %q: use a unit string like \"8GB\", \"512MB\", or \"1TB\"", s)
	}

	unit := strings.ToUpper(s[len(s)-2:])
	numStr := s[:len(s)-2]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil || val <= 0 {
		return 0, fmt.Errorf("invalid resource %q: use a unit string like \"8GB\", \"512MB\", or \"1TB\"", s)
	}

	var multiplier float64
	switch unit {
	case "MB":
		multiplier = 1 << 20
	case "GB":
		multiplier = 1 << 30
	case "TB":
		multiplier = 1 << 40
	default:
		return 0, fmt.Errorf("invalid resource %q: unknown unit %q — supported units: MB, GB, TB", s, unit)
	}

	return int64(val * multiplier), nil
}

// ValidateEnvVarName returns an error if name is not a valid POSIX environment
// variable name. Valid names start with a letter or underscore, followed by
// letters, digits, or underscores.
func ValidateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("env var name must not be empty")
	}
	for i, ch := range name {
		switch {
		case ch == '_':
			// always valid
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z':
			// always valid
		case ch >= '0' && ch <= '9':
			if i == 0 {
				return fmt.Errorf("invalid env var name %q: must not start with a digit", name)
			}
		default:
			return fmt.Errorf("invalid env var name %q: character %q is not allowed (use letters, digits, or underscores)", name, string(ch))
		}
	}
	return nil
}

// ValidateAgentsDefine rejects unknown keys under agents.define.<name> in the
// config file. Field names are derived from AgentDefine (agent.Def) struct tags.
func ValidateAgentsDefine(cfgPath string) error {
	if cfgPath == "" {
		return nil
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil
	}
	var raw struct {
		Agents struct {
			Define map[string]map[string]any `yaml:"define"`
		} `yaml:"agents"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("agents.define: %w", err)
	}
	allowed := agentDefineYAMLKeys()
	var problems []string
	for agentName, fields := range raw.Agents.Define {
		for key := range fields {
			if _, ok := allowed[key]; !ok {
				problems = append(problems, fmt.Sprintf("%s: unknown field %q", agentName, key))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	keys := make([]string, 0, len(allowed))
	for k := range allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Errorf(
		"agents.define: %s\nallowed fields: %s",
		strings.Join(problems, "; "),
		strings.Join(keys, ", "),
	)
}

func agentDefineYAMLKeys() map[string]struct{} {
	var d AgentDefine
	t := reflect.TypeOf(d)
	allowed := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	return allowed
}

// ParseMount parses a mount specification of the form "<host_path>:<mode>"
// or "<host_path>" (defaults to rw). The host path is expanded (~ → home).
// Valid modes: "ro" (read-only), "rw" (read-write). Any other mode is an error.
func ParseMount(spec, home string) (Mount, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Mount{}, fmt.Errorf("empty mount specification")
	}

	parts := strings.SplitN(spec, ":", 2)
	rawPath := strings.TrimSpace(parts[0])
	if rawPath == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: missing host path", spec)
	}

	hostPath := expandPath(rawPath, home)
	writable := true // default is rw

	if len(parts) == 2 {
		mode := strings.ToLower(strings.TrimSpace(parts[1]))
		switch mode {
		case "rw":
			writable = true
		case "ro":
			writable = false
		default:
			return Mount{}, fmt.Errorf("invalid mount %q: unknown mode %q — use \"ro\" or \"rw\"", spec, mode)
		}
	}

	return Mount{HostPath: hostPath, Writable: writable}, nil
}
