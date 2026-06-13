package vm

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/run"
)

// SaveBaseImage stops the live instance, clones it to a shadow profile, and
// restarts the live instance.
func (l *LimaVM) SaveBaseImage(ctx context.Context, _ StartOptions) error {
	release, err := l.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()

	shadow := LimaShadowProfile(l.profile)

	status, err := l.Status(ctx)
	if err != nil {
		return err
	}
	if status == StatusRunning {
		if err := l.stopWithoutLock(ctx); err != nil {
			return err
		}
	}

	if err := l.deleteShadowIfExists(ctx, shadow); err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("Saving base image: cloning %q → %q", l.profile, shadow))
	cmd := exec.CommandContext(ctx, "limactl", "-y", "clone", l.profile, shadow, "--start=false")
	if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
		return fmt.Errorf("clone base image: %w", err)
	}

	slog.Info(fmt.Sprintf("Restarting live VM %q", l.profile))
	startCmd := exec.CommandContext(ctx, "limactl", "start", l.profile)
	if err := aivmlog.RunCmd(startCmd, "lima"); err != nil {
		return fmt.Errorf("restart live VM: %w", err)
	}
	return nil
}

// RestoreFromBaseImage deletes the live instance and clones the shadow profile
// back with the current StartOptions.
func (l *LimaVM) RestoreFromBaseImage(ctx context.Context, opts StartOptions) error {
	release, err := l.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()

	shadow := LimaShadowProfile(l.profile)
	if !l.HasBaseImage(ctx) {
		return fmt.Errorf("shadow instance %q not found", shadow)
	}

	if err := l.deleteLiveIfExists(ctx); err != nil {
		return err
	}

	memGiB := opts.MemoryBytes >> 30
	diskGiB := opts.DiskBytes >> 30
	args := LimaFastRestoreArgs(shadow, l.profile, opts.CPUs, memGiB, diskGiB, opts.VMType)
	cloneArgs := append([]string{"-y"}, args...)
	slog.Info(fmt.Sprintf("Restoring from base image: cloning %q → %q", shadow, l.profile))
	cmd := exec.CommandContext(ctx, "limactl", cloneArgs...)
	if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
		return fmt.Errorf("restore from base image: %w", err)
	}
	slog.Info(fmt.Sprintf("Restored live VM %q from base image", l.profile))

	return l.WaitReady(ctx, 2*time.Minute)
}

// DeleteBaseImage removes the shadow profile if it exists.
func (l *LimaVM) DeleteBaseImage(ctx context.Context) error {
	release, err := l.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()

	return l.deleteShadowIfExists(ctx, LimaShadowProfile(l.profile))
}

// HasBaseImage reports whether the shadow profile exists.
func (l *LimaVM) HasBaseImage(ctx context.Context) bool {
	shadow := LimaShadowProfile(l.profile)
	lines, err := run.OutputLines(ctx, "limactl", "list")
	if err != nil {
		return false
	}
	return ParseLimaListStatus(lines, shadow) != StatusNotFound
}

// LimaFastRestoreArgs builds limactl clone arguments for restoring from a shadow
// profile. Returned args are suitable for: limactl -y <args...>
//
// Mounts are intentionally omitted: the shadow instance already carries the
// mount set from the live→shadow save clone. Passing --mount again makes
// limactl reject overlapping paths (see limactl clone docs).
func LimaFastRestoreArgs(shadow, live string, cpus int, memGiB, diskGiB int64, vmType string) []string {
	args := []string{
		"clone", shadow, live,
		"--start",
		"--cpus", strconv.Itoa(cpus),
		"--memory", strconv.Itoa(int(memGiB)),
		"--disk", strconv.Itoa(int(diskGiB)),
	}
	args = append(args, limaCloneVMTypeFlags(vmType)...)
	return args
}

func limaCloneVMTypeFlags(vmType string) []string {
	effective := vmType
	if effective == "" {
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			effective = "vz"
		} else {
			effective = "qemu"
		}
	}
	if effective == "vz" && runtime.GOOS == "darwin" {
		return []string{"--vm-type", "vz", "--rosetta"}
	}
	if effective == "vz" {
		return []string{"--vm-type", "vz"}
	}
	return []string{"--vm-type", "qemu"}
}

func (l *LimaVM) stopWithoutLock(ctx context.Context) error {
	status, err := l.Status(ctx)
	if err != nil || status != StatusRunning {
		return nil
	}

	slog.Debug("Stopping Docker containers inside VM...")
	_ = l.Run(ctx, stopContainersScript, nil)

	slog.Info(fmt.Sprintf("Stopping Lima VM %q for base image save", l.profile))
	cmd := exec.CommandContext(ctx, "limactl", "stop", l.profile)
	if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
		slog.Warn("graceful stop failed, forcing...")
		if forceErr := run.Quiet(ctx, "limactl", "stop", l.profile, "--force"); forceErr != nil {
			return fmt.Errorf("stop VM %q: graceful stop failed: %v; force stop failed: %w", l.profile, err, forceErr)
		}
	}
	return nil
}

func (l *LimaVM) deleteLiveIfExists(ctx context.Context) error {
	status, err := l.Status(ctx)
	if err != nil {
		return err
	}
	if status == StatusNotFound {
		return nil
	}
	if status == StatusRunning {
		_ = l.Run(ctx, stopContainersScript, nil)
		_ = run.Quiet(ctx, "limactl", "stop", l.profile, "--force")
	}
	slog.Info(fmt.Sprintf("Deleting live VM %q before restore", l.profile))
	cmd := exec.CommandContext(ctx, "limactl", "delete", l.profile, "--force")
	if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
		return fmt.Errorf("delete live VM: %w", err)
	}
	return nil
}

func (l *LimaVM) deleteShadowIfExists(ctx context.Context, shadow string) error {
	lines, err := run.OutputLines(ctx, "limactl", "list")
	if err != nil {
		return nil
	}
	status := ParseLimaListStatus(lines, shadow)
	if status == StatusNotFound {
		return nil
	}
	if status == StatusRunning {
		_ = run.Quiet(ctx, "limactl", "stop", shadow, "--force")
	}
	slog.Info(fmt.Sprintf("Deleting shadow VM %q", shadow))
	if err := run.Quiet(ctx, "limactl", "delete", shadow, "--force"); err != nil {
		return fmt.Errorf("delete shadow VM: %w", err)
	}
	return nil
}
