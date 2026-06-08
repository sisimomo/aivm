//go:build bootstrap

package bootstraptest

import "testing"

func TestPlugin_Docker(t *testing.T) {
	t.Skip("docker plugin requires systemd (Lima VM); see manual checklist in spec")
}
