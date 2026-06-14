package mountspec

// MountSpec is the YAML shape for vm.mounts and agent mounts.
type MountSpec struct {
	Source string `yaml:"source" mapstructure:"source"`
	Target string `yaml:"target" mapstructure:"target"`
	Mode   string `yaml:"mode" mapstructure:"mode"`
}

// ResolvedMount is a fully expanded mount ready for the VM backend.
type ResolvedMount struct {
	HostPath  string
	GuestPath string
	Writable  bool
}

// Context supplies template variables for path rendering.
type Context struct {
	Home      string // host home directory
	GuestHome string // guest VM user home directory
	StateDir  string
}
