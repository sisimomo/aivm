package lifecycle

type BaseImageCheck struct {
	ConfigHash     string
	Backend        string
	VMType         string
	ArtifactExists bool
}

func BaseImageValid(state *BootstrapState, check BaseImageCheck) bool {
	if state == nil || !check.ArtifactExists || state.NeedsMigration() {
		return false
	}
	return state.ConfigHash == check.ConfigHash &&
		state.Backend == check.Backend &&
		state.VMType == check.VMType
}
