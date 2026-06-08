package lifecycle

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
)

// t3codeURLFile is the name of the state file that holds the T3 Code pairing URL.
const t3codeURLFile = "t3code-url"

// t3CodeState is the parsed contents of the t3code-url state file.
type t3CodeState struct {
	// DisplayURL is the full pairing URL to show to the user (may include a token fragment).
	DisplayURL string
	// HostPort is the TCP port on localhost that T3 Code is forwarded to.
	HostPort int
}

// readT3CodeState reads the t3code-url state file and parses both the display
// URL and the host port from it in a single pass. fallbackPort is used when the
// file is absent or cannot be parsed.
func readT3CodeState(stateDir string, fallbackPort int) t3CodeState {
	raw, err := os.ReadFile(filepath.Join(stateDir, t3codeURLFile))
	if err != nil {
		return t3CodeState{
			DisplayURL: fmt.Sprintf("http://localhost:%d", fallbackPort),
			HostPort:   fallbackPort,
		}
	}
	displayURL := strings.TrimSpace(string(raw))

	// Parse the host port from the URL using stdlib — no regex needed.
	// url.Parse handles the fragment (#token=…) correctly.
	parsed, parseErr := url.Parse(displayURL)
	if parseErr == nil {
		host, portStr, splitErr := net.SplitHostPort(parsed.Host)
		_ = host
		if splitErr == nil {
			if p, atoiErr := strconv.Atoi(portStr); atoiErr == nil {
				return t3CodeState{DisplayURL: displayURL, HostPort: p}
			}
		}
	}

	// URL present but port could not be parsed — return the display URL with the fallback port.
	return t3CodeState{DisplayURL: displayURL, HostPort: fallbackPort}
}

// t3CodeIsAlive probes T3 Code from the host machine by making an HTTP GET to
// http://localhost:{hostPort}/. It returns true when the server sends any HTTP
// response within 3 seconds, confirming that both t3 serve and the
// port-forwarding path are working. A connection error or timeout means the
// service is not reachable.
func t3CodeIsAlive(hostPort int) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/", hostPort))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// BootstrapEnabledPlugins returns the deduplicated ordered list of plugins to install.
func BootstrapEnabledPlugins(reg *plugin.Registry, providers []agent.Provider, configured []string) []string {
	total := len(configured)
	for _, p := range providers {
		total += len(p.RequiredPlugins())
	}
	enabled := make([]string, 0, total)
	seen := make(map[string]bool, total)

	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		enabled = append(enabled, name)
	}

	for _, name := range configured {
		add(name)
	}
	for _, prov := range providers {
		for _, name := range prov.RequiredPlugins() {
			add(name)
		}
	}
	return enabled
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func vmCreatedRecently(stateDir string) bool {
	data, err := os.ReadFile(filepath.Join(stateDir, vm.VMCreatedAtFile))
	if err != nil {
		return false
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(epoch, 0)) < 10*time.Minute
}

// ensureAgentPersistDirs creates the host-side directories that are mounted
// into the VM for persistence.
func ensureAgentPersistDirs(cfg *config.Config, agentDefs map[string]agent.Def) {
	seen := make(map[string]bool)
	for _, def := range agentDefs {
		for _, rel := range def.Persist {
			if seen[rel] {
				continue
			}
			seen[rel] = true
			_ = os.MkdirAll(filepath.Join(cfg.StateDir, rel), 0755)
		}
	}
	if cfg.T3Code.Enable {
		_ = os.MkdirAll(filepath.Join(cfg.StateDir, ".t3"), 0755)
	}
}

// buildStartOptions constructs consistent vm.StartOptions from config.
// All VM-creating operations use this to eliminate duplication.
func buildStartOptions(v vm.VM, cfg *config.Config, agentDefs map[string]agent.Def) vm.StartOptions {
	seenPersist := make(map[string]bool)
	mounts := make([]vm.Mount, 0, len(cfg.VM.ParsedMounts))
	for _, m := range cfg.VM.ParsedMounts {
		mounts = append(mounts, vm.Mount{HostPath: m.HostPath, Writable: m.Writable})
	}
	agentNames := make([]string, 0, len(agentDefs))
	for k := range agentDefs {
		agentNames = append(agentNames, k)
	}
	sort.Strings(agentNames)
	for _, name := range agentNames {
		def := agentDefs[name]
		for _, rel := range def.Persist {
			if seenPersist[rel] {
				continue
			}
			seenPersist[rel] = true
			mounts = append(mounts, vm.Mount{HostPath: filepath.Join(cfg.StateDir, rel), Writable: true})
		}
	}
	if cfg.T3Code.Enable {
		mounts = append(mounts, vm.Mount{HostPath: filepath.Join(cfg.StateDir, ".t3"), Writable: true})
	}

	// Backends that need port bindings at boot (e.g. Docker) declare ports via
	// StartOptions; others (e.g. Lima) use an SSH tunnel after the VM is up.
	var portMappings []vm.PortMapping
	if v.NeedsPortBindingAtBoot() && cfg.T3Code.Enable {
		if cfg.T3Code.Port == 0 {
			// Port 0 in config means "auto-assign host port"; map to default T3 Code container port 3773
			portMappings = []vm.PortMapping{{HostPort: 0, ContainerPort: 3773}}
		} else {
			portMappings = []vm.PortMapping{{HostPort: cfg.T3Code.Port, ContainerPort: cfg.T3Code.Port}}
		}
	}

	return vm.StartOptions{
		CPUs:         cfg.VM.CPUs,
		MemoryBytes:  cfg.VM.MemoryBytes,
		DiskBytes:    cfg.VM.DiskBytes,
		VMType:       cfg.VM.Type,
		Mounts:       mounts,
		PortMappings: portMappings,
	}
}

// applyVMEnv writes vm.env as shell exports to /etc/profile.d/aivm-user-env.sh
// inside the VM, making the variables available in every login shell session.
// If env is empty, the file is written with no exports (clearing any prior values).
func applyVMEnv(ctx context.Context, v vm.VM, env map[string]string) error {
	var sb strings.Builder
	sb.WriteString("# Managed by aivm — do not edit manually\n")

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString("export ")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(vm.ShellEscape(env[k]))
		sb.WriteString("\n")
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(sb.String()))
	script := fmt.Sprintf(
		`echo %s | base64 -d | sudo tee /etc/profile.d/aivm-user-env.sh > /dev/null
sudo chmod 0644 /etc/profile.d/aivm-user-env.sh`,
		vm.ShellEscape(encoded),
	)
	return v.Run(ctx, script, nil)
}

// readHostGitIdentity reads user.name and user.email from the host's global
// git config (--global flag; values set only at system or XDG scope without a
// corresponding global entry may not be visible). Returns empty strings when
// the values are not set — callers must treat empty as "not available".
// Unexpected errors (e.g. git not found in PATH) are logged at trace level.
func readHostGitIdentity() (name, email string) {
	gitOutput := func(args ...string) string {
		out, err := exec.Command("git", args...).Output()
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				// Non-exit error (e.g. git not in PATH); surface it at trace level.
				slog.Log(context.Background(), aivmlog.SlogTrace, fmt.Sprintf("git config query failed: %v", err))
			}
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	name = gitOutput("config", "--global", "user.name")
	email = gitOutput("config", "--global", "user.email")
	return
}

// applyGitIdentity writes the given git user.name and user.email into the VM's
// global git config. If either value is empty the function is a no-op.
func applyGitIdentity(ctx context.Context, v vm.VM, name, email string) error {
	if name == "" || email == "" {
		slog.Log(context.Background(), aivmlog.SlogTrace, "host git identity not set — skipping git config sync")
		return nil
	}
	script := fmt.Sprintf(
		"git config --global user.name %s && git config --global user.email %s",
		vm.ShellEscape(name),
		vm.ShellEscape(email),
	)
	return v.Run(ctx, script, nil)
}
