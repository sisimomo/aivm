package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/lifecycle"
)

// fakeProvider is a minimal agent.Provider for unit tests.
type fakeProvider struct {
	name     string
	required []string
}

func (f *fakeProvider) Name() string              { return f.name }
func (f *fakeProvider) Description() string       { return "fake-" + f.name }
func (f *fakeProvider) RequiredPlugins() []string { return f.required }
func (f *fakeProvider) Launch(_ context.Context, _ agent.LaunchEnv) (*agent.Response, error) {
	return nil, nil
}

func TestBootstrapEnabledPlugins_EmptyInputs(t *testing.T) {
	t.Parallel()
	got := lifecycle.BootstrapEnabledPlugins(nil, nil, nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestBootstrapEnabledPlugins_ConfiguredOnly_PreservesOrder(t *testing.T) {
	t.Parallel()
	configured := []string{"system", "mise-node", "mise-python"}
	got := lifecycle.BootstrapEnabledPlugins(nil, nil, configured)
	want := []string{"system", "mise-node", "mise-python"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBootstrapEnabledPlugins_ProviderPluginAppended(t *testing.T) {
	t.Parallel()
	providers := []agent.Provider{
		&fakeProvider{name: "claude", required: []string{"claude"}},
	}
	got := lifecycle.BootstrapEnabledPlugins(nil, providers, []string{"system"})
	want := []string{"system", "claude"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBootstrapEnabledPlugins_DeduplicatesConfiguredAndProvider(t *testing.T) {
	t.Parallel()
	providers := []agent.Provider{
		&fakeProvider{name: "claude", required: []string{"claude"}},
	}
	got := lifecycle.BootstrapEnabledPlugins(nil, providers, []string{"system", "claude"})
	want := []string{"system", "claude"}
	if len(got) != len(want) {
		t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBootstrapEnabledPlugins_MultipleProviders_Deduplication(t *testing.T) {
	t.Parallel()
	providers := []agent.Provider{
		&fakeProvider{name: "claude", required: []string{"claude", "shared"}},
		&fakeProvider{name: "copilot", required: []string{"copilot", "shared"}},
	}
	got := lifecycle.BootstrapEnabledPlugins(nil, providers, []string{"system"})
	want := []string{"system", "claude", "shared", "copilot"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBootstrapEnabledPlugins_EmptyNameFiltered(t *testing.T) {
	t.Parallel()
	providers := []agent.Provider{
		&fakeProvider{name: "claude", required: []string{"", "claude"}},
	}
	got := lifecycle.BootstrapEnabledPlugins(nil, providers, []string{"", "system"})
	want := []string{"system", "claude"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBootstrapEnabledPlugins_NoProviders_ConfiguredPassthrough(t *testing.T) {
	t.Parallel()
	got := lifecycle.BootstrapEnabledPlugins(nil, []agent.Provider{}, []string{"a", "b", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBootstrapEnabledPlugins_ProviderWithNoRequired(t *testing.T) {
	t.Parallel()
	providers := []agent.Provider{
		&fakeProvider{name: "noop", required: []string{}},
	}
	got := lifecycle.BootstrapEnabledPlugins(nil, providers, []string{"system"})
	want := []string{"system"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got[0] != "system" {
		t.Errorf("got[0] = %q, want %q", got[0], "system")
	}
}

func TestBootstrapEnabledPlugins_AllDuplicates_OnlyFirstKept(t *testing.T) {
	t.Parallel()
	got := lifecycle.BootstrapEnabledPlugins(nil, nil, []string{"system", "system", "system"})
	if len(got) != 1 || got[0] != "system" {
		t.Errorf("got %v, want [system]", got)
	}
}
