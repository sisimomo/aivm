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
	// claude keeps its real cli_command so aivm agent -- hits the binary directly.
	h := framework.New(t,
		framework.WithPreserveAgentCLI("claude"),
	)

	h.Scenario("aivm agent -- forwards args to the agent CLI in the VM").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Run: aivm agent -- --version", actions.CLI("agent", "--", "--version")).
		Assert("Claude version in CLI output", assertions.OutputContains("Claude")).
		Step("Run: aivm agent without -- (expect error)", cliExpectError("agent", "-p", "noop")).
		Step("Run: aivm stop", actions.CLI("stop")).
		Run()
}
