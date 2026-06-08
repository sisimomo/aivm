//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_AWSCli verifies that the awscli plugin installs the AWS CLI v2
// correctly. The install script is arch-aware (x86_64 vs aarch64), so this test
// exercises the download + unzip + install path for the host architecture.
func TestPlugin_AWSCli(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("awscli", nil) // installs system first (dependency)
	h.AssertCommand("aws --version", "aws-cli")
}
