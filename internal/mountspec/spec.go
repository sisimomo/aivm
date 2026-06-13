package mountspec

// MountSpec is the YAML shape for vm.mounts and agent mounts.
type MountSpec struct {
	Location   string `yaml:"location" mapstructure:"location"`
	MountPoint string `yaml:"mountPoint" mapstructure:"mountPoint"`
	Mode       string `yaml:"mode" mapstructure:"mode"`
}

// ResolvedMount is a fully expanded mount ready for the VM backend.
type ResolvedMount struct {
	HostPath  string
	GuestPath string
	Writable  bool
}

// Context supplies template variables for path rendering.
type Context struct {
	Home     string
	StateDir string
}
