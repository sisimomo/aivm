package vm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const transitionStateFile = "vm-transition.json"

// TransitionState tracks an in-progress migration from a legacy VM to a newly-built VM.
// It is written when the user chooses to keep their active sessions on the old VM while
// launching future sessions on the freshly-bootstrapped one.
type TransitionState struct {
	LegacyProfile string    `json:"legacy_profile"`
	NewProfile    string    `json:"new_profile"`
	StartedAt     time.Time `json:"started_at"`
}

func LoadTransitionState(stateDir string) *TransitionState {
	data, err := os.ReadFile(filepath.Join(stateDir, transitionStateFile))
	if err != nil {
		return nil
	}
	var ts TransitionState
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil
	}
	return &ts
}

func SaveTransitionState(stateDir string, ts *TransitionState) error {
	data, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, transitionStateFile), data, 0644)
}

func ClearTransitionState(stateDir string) {
	os.Remove(filepath.Join(stateDir, transitionStateFile))
}
