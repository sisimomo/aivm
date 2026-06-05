package log

import "os"

// ResolveLevel returns the effective log level using precedence: flag → env → config → info.
func ResolveLevel(flag string, flagSet bool, cfgLevel string) (Level, error) {
	if flagSet {
		return ParseLevel(flag)
	}
	if v := os.Getenv("AIVM_LOG_LEVEL"); v != "" {
		return ParseLevel(v)
	}
	if cfgLevel != "" {
		return ParseLevel(cfgLevel)
	}
	return LevelInfo, nil
}
