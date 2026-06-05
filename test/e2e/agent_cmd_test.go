package e2e

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestCLIAgentPassthrough verifies that `aivm agent -- <args>` runs the agent
// CLI in the VM without launch_args and requires the '--' separator.
func TestCLIAgentPassthrough(t *testing.T) {
	t.Parallel()
	// opencode keeps its real cli_command so aivm agent -- hits the binary directly.
	h := framework.New(t,
		framework.WithExtraAgents("opencode"),
		framework.WithPreserveAgentCLI("opencode"),
	)

	h.Scenario("aivm agent -- forwards args to the agent CLI in the VM").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Run: aivm --agent opencode agent -- --version", actions.CLI("--agent", "opencode", "agent", "--", "--version")).
		Assert("OpenCode version in CLI output", assertions.OutputContains("opencode")).
		Step("Run: aivm agent without -- (expect error)", cliExpectError("agent", "-p", "noop")).
		Step("Run: aivm stop", actions.CLI("stop")).
		Run()
}
