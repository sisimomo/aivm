package vm

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeVM struct {
	createdSnapshots []string
	deletedSnapshots []string
	createErr        error
	deleteErr        error
}

func (f *fakeVM) Profile() string                                          { return "test" }
func (f *fakeVM) NeedsPortBindingAtBoot() bool                             { return false }
func (f *fakeVM) Status(context.Context) (Status, error)                   { return StatusRunning, nil }
func (f *fakeVM) Start(context.Context, StartOptions) error                { return nil }
func (f *fakeVM) Stop(context.Context) error                               { return nil }
func (f *fakeVM) Destroy(context.Context) error                            { return nil }
func (f *fakeVM) Run(context.Context, string, map[string]string) error     { return nil }
func (f *fakeVM) RunOutput(context.Context, string, map[string]string) (string, error) {
	return "", nil
}
func (f *fakeVM) RunInteractive(context.Context, string, map[string]string) error {
	return nil
}
func (f *fakeVM) SSH(context.Context) error                                 { return nil }
func (f *fakeVM) CopyTo(context.Context, string, string, bool) error        { return nil }
func (f *fakeVM) CopyFrom(context.Context, string, string, bool) error      { return nil }
func (f *fakeVM) WaitReady(context.Context, time.Duration) error            { return nil }
func (f *fakeVM) CreateSnapshot(_ context.Context, name string) error       { f.createdSnapshots = append(f.createdSnapshots, name); return f.createErr }
func (f *fakeVM) RestoreSnapshot(context.Context, string) (bool, error)     { return false, nil }
func (f *fakeVM) DeleteSnapshot(_ context.Context, name string) error       { f.deletedSnapshots = append(f.deletedSnapshots, name); return f.deleteErr }
func (f *fakeVM) ListSnapshots(context.Context) ([]Snapshot, error)         { return nil, nil }
func (f *fakeVM) GetPublishedPort(containerPort int) (int, error)           { return containerPort, nil }

func TestSaveBaseImagePrunesPreviousSnapshotAfterSuccessfulCreate(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	vm := &fakeVM{}
	mgr := NewImageManager(vm, stateDir)

	if err := mgr.writeBaseImage(&BaseImage{
		ID:           "old",
		SnapshotName: "aivm-base-old",
		CreatedAt:    time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write old base image: %v", err)
	}

	img, err := mgr.SaveBaseImage(context.Background())
	if err != nil {
		t.Fatalf("save base image: %v", err)
	}

	if len(vm.createdSnapshots) != 1 {
		t.Fatalf("created snapshots = %v, want 1 snapshot create", vm.createdSnapshots)
	}
	if len(vm.deletedSnapshots) != 1 || vm.deletedSnapshots[0] != "aivm-base-old" {
		t.Fatalf("deleted snapshots = %v, want [aivm-base-old]", vm.deletedSnapshots)
	}
	if img.SnapshotName == "" {
		t.Fatal("saved base image missing snapshot name")
	}

	got := mgr.LoadBaseImage()
	if got == nil {
		t.Fatal("expected base image metadata to be present")
	}
	if got.SnapshotName != img.SnapshotName {
		t.Fatalf("saved snapshot name = %q, want %q", got.SnapshotName, img.SnapshotName)
	}
	if got.ID == "old" {
		t.Fatalf("expected new base image id, still got %q", got.ID)
	}
}

func TestSaveBaseImageDoesNotPruneOldSnapshotWhenCreateFails(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	vm := &fakeVM{createErr: context.DeadlineExceeded}
	mgr := NewImageManager(vm, stateDir)

	if err := mgr.writeBaseImage(&BaseImage{
		ID:           "old",
		SnapshotName: "aivm-base-old",
		CreatedAt:    time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write old base image: %v", err)
	}

	img, err := mgr.SaveBaseImage(context.Background())
	if err != nil {
		t.Fatalf("save base image: %v", err)
	}

	if len(vm.deletedSnapshots) != 0 {
		t.Fatalf("deleted snapshots = %v, want none", vm.deletedSnapshots)
	}
	if img.SnapshotName != "" {
		t.Fatalf("snapshot name = %q, want empty when snapshot create fails", img.SnapshotName)
	}

	got := mgr.LoadBaseImage()
	if got == nil {
		t.Fatal("expected base image metadata to be present")
	}
	if got.SnapshotName != "" {
		t.Fatalf("saved snapshot name = %q, want empty when snapshot create fails", got.SnapshotName)
	}
}

func TestSaveBaseImageContinuesWhenDeleteFails(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	vm := &fakeVM{deleteErr: errors.New("rmi: permission denied")}
	mgr := NewImageManager(vm, stateDir)

	if err := mgr.writeBaseImage(&BaseImage{
		ID:           "old",
		SnapshotName: "aivm-base-old",
		CreatedAt:    time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write old base image: %v", err)
	}

	img, err := mgr.SaveBaseImage(context.Background())
	if err != nil {
		t.Fatalf("SaveBaseImage should succeed even when delete fails: %v", err)
	}
	if img.SnapshotName == "" {
		t.Fatal("expected new snapshot to be recorded")
	}
	// delete was attempted (the Warn path fired)
	if len(vm.deletedSnapshots) != 1 || vm.deletedSnapshots[0] != "aivm-base-old" {
		t.Fatalf("deletedSnapshots = %v, want [aivm-base-old]", vm.deletedSnapshots)
	}
}

func TestSaveBaseImageNoPreviousImage(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	vm := &fakeVM{}
	mgr := NewImageManager(vm, stateDir)

	// No previous base image written — first-ever save.
	img, err := mgr.SaveBaseImage(context.Background())
	if err != nil {
		t.Fatalf("SaveBaseImage: %v", err)
	}
	if len(vm.deletedSnapshots) != 0 {
		t.Fatalf("expected no delete on first save, got %v", vm.deletedSnapshots)
	}
	if img.SnapshotName == "" {
		t.Fatal("expected snapshot to be created")
	}
}

func TestSaveBaseImagePreviousImageHasNoSnapshot(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	vm := &fakeVM{}
	mgr := NewImageManager(vm, stateDir)

	if err := mgr.writeBaseImage(&BaseImage{
		ID:           "old",
		SnapshotName: "", // previous save had no snapshot
		CreatedAt:    time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write old base image: %v", err)
	}

	_, err := mgr.SaveBaseImage(context.Background())
	if err != nil {
		t.Fatalf("SaveBaseImage: %v", err)
	}
	if len(vm.deletedSnapshots) != 0 {
		t.Fatalf("expected no delete when previous image has no snapshot, got %v", vm.deletedSnapshots)
	}
}
