//go:build bootstrap

package bootstraptest

import "testing"

func TestPlugin_Docker(t *testing.T) {
	t.Parallel()
	h := newPrivilegedBootstrapHarness(t)
	h.Install("docker", nil)
	h.AssertCommand("sudo docker version", "Version:")
}
