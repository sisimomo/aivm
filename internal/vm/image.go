package vm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	aivmlog "aivm/internal/log"
)

type ImageManager struct {
	vm       VM
	stateDir string
}

func NewImageManager(v VM, stateDir string) *ImageManager {
	return &ImageManager{vm: v, stateDir: stateDir}
}

func SnapshotName(enabled []string, pluginCfg map[string]map[string]any) string {
	data, _ := json.Marshal(map[string]any{"enabled": enabled, "config": pluginCfg})
	h := sha256.Sum256(data)
	return fmt.Sprintf("aivm-%x", h[:4])
}

func (m *ImageManager) TrySave(ctx context.Context, name string) {
	aivmlog.Info("saving bootstrap snapshot '%s'...", name)
	if err := m.vm.CreateSnapshot(ctx, name); err != nil {
		aivmlog.Warn("snapshot create failed (non-fatal): %v", err)
		return
	}
	aivmlog.Success("snapshot '%s' saved — future VMs can skip bootstrap", name)
}

func (m *ImageManager) TryRestore(ctx context.Context, name string) bool {
	found, err := m.vm.RestoreSnapshot(ctx, name)
	if err != nil {
		aivmlog.Debug("snapshot restore error: %v", err)
		return false
	}
	if found {
		aivmlog.Success("restored from snapshot '%s' — bootstrap skipped", name)
	}
	return found
}

func (m *ImageManager) RecordCreation() {
	path := filepath.Join(m.stateDir, "vm-created-at")
	os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}

func (m *ImageManager) AgeDays() int {
	path := filepath.Join(m.stateDir, "vm-created-at")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	epoch, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0
	}
	return int(time.Since(time.Unix(epoch, 0)).Hours() / 24)
}
