package vm_test

import (
	"context"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

type stubBaseStore struct {
	hasBase bool
}

func (s *stubBaseStore) Profile() string { return "stub" }

func (s *stubBaseStore) NeedsPortBindingAtBoot() bool { return false }

func (s *stubBaseStore) Status(_ context.Context) (vm.Status, error) {
	return vm.StatusStopped, nil
}

func (s *stubBaseStore) Start(_ context.Context, _ vm.StartOptions) error { return nil }

func (s *stubBaseStore) Stop(_ context.Context) error { return nil }

func (s *stubBaseStore) Destroy(_ context.Context) error { return nil }

func (s *stubBaseStore) Run(_ context.Context, _ string, _ map[string]string) error { return nil }

func (s *stubBaseStore) RunOutput(_ context.Context, _ string, _ map[string]string) (string, error) {
	return "", nil
}

func (s *stubBaseStore) RunInteractive(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (s *stubBaseStore) RunStream(_ context.Context, _ string, _ map[string]string) (int, error) {
	return 0, nil
}

func (s *stubBaseStore) SSH(_ context.Context, _ map[string]string) error { return nil }

func (s *stubBaseStore) CopyTo(_ context.Context, _, _ string, _ bool) error { return nil }

func (s *stubBaseStore) CopyFrom(_ context.Context, _, _ string, _ bool) error { return nil }

func (s *stubBaseStore) WaitReady(_ context.Context, _ time.Duration) error { return nil }

func (s *stubBaseStore) GetPublishedPort(_ int) (int, error) { return 0, nil }

func (s *stubBaseStore) SaveBaseImage(_ context.Context, _ vm.StartOptions) error { return nil }

func (s *stubBaseStore) RestoreFromBaseImage(_ context.Context, _ vm.StartOptions) error {
	return nil
}

func (s *stubBaseStore) DeleteBaseImage(_ context.Context) error { return nil }

func (s *stubBaseStore) HasBaseImage(_ context.Context) bool { return s.hasBase }

func TestAsBaseImageStore_InterfaceAssertion(t *testing.T) {
	t.Parallel()

	v := &stubBaseStore{hasBase: true}
	store, ok := vm.AsBaseImageStore(v)
	if !ok {
		t.Fatal("expected AsBaseImageStore to succeed for stub implementing BaseImageStore")
	}
	if !store.HasBaseImage(context.Background()) {
		t.Fatal("expected HasBaseImage to return true")
	}
}
