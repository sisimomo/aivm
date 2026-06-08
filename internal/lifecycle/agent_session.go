package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/vm"
)

type agentSession struct {
	prov    agent.Provider
	provDef agent.Def
	vmDir   string
	hostCWD string
	ctx     context.Context
	cleanup func()
}

func (svc *LifecycleService) prepareAgentSession(ctx context.Context, agentOverride string) (*agentSession, error) {
	cfg := svc.Config

	prov := svc.Provider
	provDef := svc.AgentDefs[svc.Provider.Name()]
	if agentOverride != "" {
		found := false
		for _, p := range svc.EnabledProviders {
			if p.Name() == agentOverride {
				prov = p
				provDef = svc.AgentDefs[agentOverride]
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(svc.EnabledProviders))
			for _, p := range svc.EnabledProviders {
				names = append(names, p.Name())
			}
			sort.Strings(names)
			return nil, fmt.Errorf("agent %q is not enabled — enabled agents: %v", agentOverride, names)
		}
	}

	if provDef.CLICommand == "" {
		return nil, fmt.Errorf("agent %q: cli_command is not configured", prov.Name())
	}

	getCWD := svc.GetWorkDir
	if getCWD == nil {
		getCWD = os.Getwd
	}
	hostCWD, err := getCWD()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}
	realCWD, err := filepath.EvalSymlinks(hostCWD)
	if err != nil {
		realCWD = filepath.Clean(hostCWD)
	}
	if err := AssertUnderMount(realCWD, cfg); err != nil {
		return nil, err
	}

	if err := svc.checkVMAge(ctx); err != nil {
		return nil, err
	}

	status, err := svc.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return nil, fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	sess, err := svc.Sessions.Create(hostCWD)
	if err != nil {
		svc.logger().Warn(fmt.Sprintf("could not create session lock: %v", err))
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)

	cleanup := func() {
		stop()
		if sess != nil {
			sess.Remove()
		}
	}

	return &agentSession{
		prov:    prov,
		provDef: provDef,
		vmDir:   realCWD,
		hostCWD: hostCWD,
		ctx:     runCtx,
		cleanup: cleanup,
	}, nil
}

func AssertUnderMount(realCWD string, cfg *config.Config) error {
	realCWD = filepath.Clean(realCWD)
	for _, m := range cfg.VM.ParsedMounts {
		realMount, err := filepath.EvalSymlinks(m.HostPath)
		if err != nil {
			realMount = filepath.Clean(m.HostPath)
		}
		if realMount == "" {
			continue
		}
		if PathUnderMount(realCWD, realMount) {
			return nil
		}
	}
	return fmt.Errorf("current directory '%s' is not under any configured VM mount\naivm only works inside a mounted directory", realCWD)
}

func PathUnderMount(path, mount string) bool {
	sep := string(os.PathSeparator)
	if mount == sep {
		return true
	}
	if path == mount {
		return true
	}
	return strings.HasPrefix(path, mount+sep)
}

func agentExitError(resp *agent.Response, err error) error {
	if err != nil {
		return err
	}
	if resp != nil && resp.ExitCode != 0 {
		return fmt.Errorf("agent exited with code %d", resp.ExitCode)
	}
	return nil
}
