package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	aivmlog "aivm/internal/log"
)

const baseImageFile = "base-image.json"
const vmImageRefFile = "vm-image-ref"

// BaseImage represents a versioned, immutable snapshot of a fully-bootstrapped VM.
// It is the only valid source for creating new runtime VMs.
type BaseImage struct {
	ID           string    `json:"id"`
	SnapshotName string    `json:"snapshot_name"`
	CreatedAt    time.Time `json:"created_at"`
}

type ImageManager struct {
	vm       VM
	stateDir string
}

func NewImageManager(v VM, stateDir string) *ImageManager {
	return &ImageManager{vm: v, stateDir: stateDir}
}

// SaveBaseImage records the current VM state as the new base image.
// Metadata is always persisted; a VM snapshot is attempted as a best-effort
// optimisation (skipped silently when the VM backend does not support it).
// All future VM creations will reference this image.
func (m *ImageManager) SaveBaseImage(ctx context.Context) (*BaseImage, error) {
	id := strconv.FormatInt(time.Now().Unix(), 10)
	snapshotName := "aivm-base-" + id

	img := &BaseImage{
		ID:        id,
		CreatedAt: time.Now().UTC(),
	}

	// Write metadata first so the base image is tracked even without a snapshot.
	if err := m.writeBaseImage(img); err != nil {
		return nil, fmt.Errorf("recording base image metadata: %w", err)
	}

	// Best-effort: create a VM snapshot for fast restore on future VM creation.
	if err := m.vm.CreateSnapshot(ctx, snapshotName); err != nil {
		aivmlog.Debug("VM snapshot unavailable (non-fatal): %v", err)
	} else {
		img.SnapshotName = snapshotName
		_ = m.writeBaseImage(img) // update with snapshot name
		aivmlog.Success("base image saved: %s (id=%s)", snapshotName, id)
	}

	aivmlog.Info("base image recorded: id=%s", id)
	return img, nil
}

// LoadBaseImage returns the current base image metadata, or nil if none has been saved.
func (m *ImageManager) LoadBaseImage() *BaseImage {
	data, err := os.ReadFile(filepath.Join(m.stateDir, baseImageFile))
	if err != nil {
		return nil
	}
	var img BaseImage
	if err := json.Unmarshal(data, &img); err != nil {
		return nil
	}
	return &img
}

// TryRestoreBaseImage restores the VM to the current base image snapshot, skipping bootstrap.
// Returns true on success. Returns false — triggering normal bootstrap — when no snapshot
// was stored (e.g. the VM backend does not support snapshots) or the snapshot is unavailable.
func (m *ImageManager) TryRestoreBaseImage(ctx context.Context) bool {
	img := m.LoadBaseImage()
	if img == nil || img.SnapshotName == "" {
		aivmlog.Debug("no restorable base image snapshot — will run bootstrap")
		return false
	}

	found, err := m.vm.RestoreSnapshot(ctx, img.SnapshotName)
	if err != nil {
		aivmlog.Debug("base image restore error: %v", err)
		return false
	}
	if !found {
		aivmlog.Debug("base image snapshot '%s' not found — will run bootstrap", img.SnapshotName)
		return false
	}

	aivmlog.Success("restored from base image '%s' (id=%s) — bootstrap skipped", img.SnapshotName, img.ID)
	m.RecordVMImageRef(img.ID)
	return true
}

// SaveBaseImageMetadataOnly records a new base image version without creating a VM snapshot.
// Used during soft-transition rebuilds where bootstrap runs on a temporary VM.
// The VM snapshot will be created on the next fresh VM start.
func (m *ImageManager) SaveBaseImageMetadataOnly() (*BaseImage, error) {
	id := strconv.FormatInt(time.Now().Unix(), 10)
	img := &BaseImage{
		ID:        id,
		CreatedAt: time.Now().UTC(),
		// SnapshotName intentionally empty; TryRestoreBaseImage will fall through to bootstrap.
	}
	if err := m.writeBaseImage(img); err != nil {
		return nil, fmt.Errorf("recording base image metadata: %w", err)
	}
	aivmlog.Info("base image version recorded: id=%s (snapshot will be created on next start)", id)
	return img, nil
}


func (m *ImageManager) RecordVMImageRef(imageID string) {
	os.WriteFile(filepath.Join(m.stateDir, vmImageRefFile), []byte(imageID), 0644)
}

// GetVMImageRef returns the base image ID this VM was created from, or "" if unknown.
func (m *ImageManager) GetVMImageRef() string {
	data, err := os.ReadFile(filepath.Join(m.stateDir, vmImageRefFile))
	if err != nil {
		return ""
	}
	return string(data)
}

// IsVMLegacy returns true when the running VM was created from an older base image
// than the current one, making it a legacy instance.
func (m *ImageManager) IsVMLegacy() bool {
	img := m.LoadBaseImage()
	if img == nil {
		return false
	}
	ref := m.GetVMImageRef()
	return ref != "" && ref != img.ID
}

// RecordCreation writes the VM creation timestamp used for age-based rotation.
func (m *ImageManager) RecordCreation() {
	path := filepath.Join(m.stateDir, "vm-created-at")
	os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}

// AgeDays returns how many days ago this VM was created.
func (m *ImageManager) AgeDays() int {
	data, err := os.ReadFile(filepath.Join(m.stateDir, "vm-created-at"))
	if err != nil {
		return 0
	}
	epoch, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0
	}
	return int(time.Since(time.Unix(epoch, 0)).Hours() / 24)
}

// BaseImageAgeDays returns how many days ago the current base image was created.
func (m *ImageManager) BaseImageAgeDays() int {
	img := m.LoadBaseImage()
	if img == nil {
		return 0
	}
	return int(time.Since(img.CreatedAt).Hours() / 24)
}

func (m *ImageManager) writeBaseImage(img *BaseImage) error {
	data, err := json.MarshalIndent(img, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.stateDir, baseImageFile), data, 0644)
}
