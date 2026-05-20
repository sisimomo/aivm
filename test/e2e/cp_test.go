package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestCpVMToHost verifies that a single file can be copied from the VM to the
// host using the vm: prefix on the source path.
func TestCpVMToHost(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	dst := filepath.Join(h.StateDir, "cptest.txt")

	h.Scenario("aivm cp: copy file from VM to host").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create file in VM", actions.RunInVM("echo 'hello from VM' > /tmp/cptest.txt")).
		Step("Copy file VM→host", actions.CLI("cp", "vm:/tmp/cptest.txt", dst)).
		Assert("File exists on host", assertions.HostFileExists(dst)).
		Assert("File contains expected content", assertions.HostFileContains(dst, "hello from VM")).
		Run()
}

// TestCpHostToVM verifies that a single file can be copied from the host to the
// VM using the vm: prefix on the destination path.
func TestCpHostToVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	src := filepath.Join(h.StateDir, "hostfile.txt")

	h.Scenario("aivm cp: copy file from host to VM").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create host file", actions.CreateHostFile(src, "hello from host")).
		Step("Copy file host→VM", actions.CLI("cp", src, "vm:/tmp/from-host.txt")).
		Assert("File exists in VM", assertions.VMFileExists("/tmp/from-host.txt")).
		Assert("File contains expected content in VM",
			assertions.VMRunOutput("cat /tmp/from-host.txt", "hello from host")).
		Run()
}

// TestCpDirVMToHost verifies that a directory can be recursively copied from
// the VM to the host with the -r flag. When the host destination does not
// exist, docker cp creates it and populates it with the directory contents.
func TestCpDirVMToHost(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	// Destination does not exist: docker cp creates it and places contents
	// of the VM source directory directly inside it.
	dst := filepath.Join(h.StateDir, "cpdir-out")

	h.Scenario("aivm cp -r: copy directory from VM to host").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create directory with files in VM",
			actions.RunInVM("mkdir -p /tmp/cpdir && echo 'vm dir content' > /tmp/cpdir/data.txt")).
		Step("Copy directory VM→host recursively", actions.CLI("cp", "-r", "vm:/tmp/cpdir", dst)).
		Assert("File inside copied directory exists on host",
			assertions.HostFileExists(filepath.Join(dst, "data.txt"))).
		Assert("File contains expected content",
			assertions.HostFileContains(filepath.Join(dst, "data.txt"), "vm dir content")).
		Run()
}

// TestCpDirHostToVM verifies that a directory can be recursively copied from
// the host to the VM with the -r flag. When the VM destination does not exist,
// docker cp creates it and populates it with the directory contents.
func TestCpDirHostToVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	srcFile := filepath.Join(h.StateDir, "cpdir-src", "data.txt")

	h.Scenario("aivm cp -r: copy directory from host to VM").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create host directory with file", actions.CreateHostFile(srcFile, "host dir content")).
		Step("Copy directory host→VM recursively",
			actions.CLI("cp", "-r", filepath.Join(h.StateDir, "cpdir-src"), "vm:/tmp/cpdir-from-host")).
		Assert("File inside copied directory exists in VM",
			assertions.VMFileExists("/tmp/cpdir-from-host/data.txt")).
		Assert("File contains expected content in VM",
			assertions.VMRunOutput("cat /tmp/cpdir-from-host/data.txt", "host dir content")).
		Run()
}

// TestCpVMNotRunning verifies that aivm cp returns an error with a helpful
// hint when the VM is not running.
func TestCpVMNotRunning(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	dst := filepath.Join(h.StateDir, "should-not-exist.txt")

	h.Scenario("aivm cp: error when VM is not running").
		Step("Attempt cp without starting VM",
			cliExpectError("cp", "vm:/tmp/file.txt", dst)).
		Assert("Error mentions 'start'", assertions.StderrContains("start")).
		Assert("Destination file was not created", assertions.HostFileAbsent(dst)).
		Run()
}

// TestCpDestExistsNoForce verifies that aivm cp fails with a clear error when
// the destination already exists and the -f flag is not provided.
func TestCpDestExistsNoForce(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	dst := filepath.Join(h.StateDir, "existing.txt")

	h.Scenario("aivm cp: error when destination exists without --force").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create file in VM", actions.RunInVM("echo 'new content' > /tmp/cptest.txt")).
		Step("Create destination file on host", actions.CreateHostFile(dst, "preexisting content")).
		Step("Attempt cp without --force", cliExpectError("cp", "vm:/tmp/cptest.txt", dst)).
		Assert("Error mentions '--force'", assertions.StderrContains("force")).
		Assert("Destination file was not overwritten",
			assertions.HostFileContains(dst, "preexisting content")).
		Run()
}

// TestCpForceOverwrite verifies that aivm cp with -f successfully overwrites
// an existing destination file.
func TestCpForceOverwrite(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	dst := filepath.Join(h.StateDir, "target.txt")

	h.Scenario("aivm cp --force: overwrite existing destination").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create file in VM with new content",
			actions.RunInVM("echo 'overwritten' > /tmp/cptest.txt")).
		Step("Create destination file on host", actions.CreateHostFile(dst, "old content")).
		Step("Copy with --force", actions.CLI("cp", "-f", "vm:/tmp/cptest.txt", dst)).
		Assert("Destination was overwritten",
			assertions.HostFileContains(dst, "overwritten")).
		Run()
}

// TestCpBothVMPaths verifies that aivm cp returns an error when both source
// and destination use the vm: prefix.
func TestCpBothVMPaths(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm cp: error when both src and dst are vm: paths").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Attempt cp with both vm: paths",
			cliExpectError("cp", "vm:/tmp/src.txt", "vm:/tmp/dst.txt")).
		Assert("Error explains both cannot be VM paths",
			assertions.StderrContains("both")).
		Run()
}

// TestCpNeitherVMPath verifies that aivm cp returns an error when neither
// source nor destination uses the vm: prefix.
func TestCpNeitherVMPath(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	src := filepath.Join(h.StateDir, "src.txt")
	dst := filepath.Join(h.StateDir, "dst.txt")

	h.Scenario("aivm cp: error when neither path uses vm: prefix").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Attempt cp with no vm: paths", cliExpectError("cp", src, dst)).
		Assert("Error mentions vm: prefix",
			assertions.StderrContains("vm:")).
		Run()
}

// cliExpectError returns a StepFunc that runs an aivm CLI command and expects
// it to return a non-zero exit code. The step itself succeeds only when the
// command fails; it fails if the command unexpectedly succeeds. Output is still
// captured to h.Output so subsequent Assert steps can inspect stderr.
func cliExpectError(args ...string) framework.StepFunc {
	return func(ctx context.Context, h *framework.Harness) error {
		h.Output.Reset()
		err := h.RunCLI(ctx, args...)
		if err == nil {
			return fmt.Errorf("expected CLI command %v to fail, but it succeeded", args)
		}
		return nil
	}
}
