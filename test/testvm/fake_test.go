package testvm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/testvm"
)

func TestFakeVM_StartStopDestroy(t *testing.T) {
	t.Parallel()
	f := testvm.New()
	ctx := context.Background()

	if got, _ := f.Status(ctx); got != vm.StatusNotFound {
		t.Fatalf("initial status: got %v want NotFound", got)
	}
	if err := f.Start(ctx, vm.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Status(ctx); got != vm.StatusRunning {
		t.Fatalf("after start: got %v", got)
	}
	if err := f.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Status(ctx); got != vm.StatusStopped {
		t.Fatalf("after stop: got %v", got)
	}
	if err := f.Destroy(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Status(ctx); got != vm.StatusNotFound {
		t.Fatalf("after destroy: got %v", got)
	}
}

func TestFakeVM_BaseImageLifecycle(t *testing.T) {
	t.Parallel()
	f := testvm.New()
	ctx := context.Background()

	if f.HasBaseImage(ctx) {
		t.Fatal("expected no base image initially")
	}
	if err := f.SaveBaseImage(ctx, vm.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if !f.HasBaseImage(ctx) {
		t.Fatal("expected base image after save")
	}
	if err := f.RestoreFromBaseImage(ctx, vm.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if !f.HasCall("RestoreFromBaseImage") {
		t.Fatal("expected RestoreFromBaseImage in call log")
	}
	if err := f.DeleteBaseImage(ctx); err != nil {
		t.Fatal(err)
	}
	if f.HasBaseImage(ctx) {
		t.Fatal("expected base image deleted")
	}
}

func TestFakeVM_DestroyPreservesBaseImage(t *testing.T) {
	t.Parallel()
	f := testvm.New()
	ctx := context.Background()
	_ = f.SaveBaseImage(ctx, vm.StartOptions{})
	_ = f.Start(ctx, vm.StartOptions{})
	_ = f.Destroy(ctx)
	if !f.BaseImageExists() {
		t.Fatal("Destroy must not delete base image artifact")
	}
}

func TestFakeVM_FaultInjection(t *testing.T) {
	t.Parallel()
	f := testvm.New()
	f.SetBaseImageExists(true)
	f.SetFaults(testvm.Faults{RestoreFromBaseImageErr: errors.New("restore failed")})
	ctx := context.Background()
	if err := f.RestoreFromBaseImage(ctx, vm.StartOptions{}); err == nil {
		t.Fatal("expected restore error")
	}
}
