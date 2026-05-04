package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type LifecycleLock struct {
	stateDir string
}

func NewLifecycleLock(stateDir string) *LifecycleLock {
	return &LifecycleLock{stateDir: stateDir}
}

func (l *LifecycleLock) lockDir() string {
	return filepath.Join(l.stateDir, "lifecycle.lock.d")
}

func (l *LifecycleLock) Acquire(timeout time.Duration) (func(), error) {
	lockDir := l.lockDir()
	deadline := time.Now().Add(timeout)

	for {
		err := os.Mkdir(lockDir, 0700)
		if err == nil {
			pidFile := filepath.Join(lockDir, "pid")
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			_ = os.RemoveAll(lockDir)
			return nil, fmt.Errorf("lifecycle lock: write pid file: %w", err)
		}
			release := func() { os.RemoveAll(lockDir) }
			return release, nil
		}

		pidFile := filepath.Join(lockDir, "pid")
		if data, readErr := os.ReadFile(pidFile); readErr == nil {
			pid, _ := strconv.Atoi(string(data))
			if pid > 0 {
				proc, err := os.FindProcess(pid)
				if err != nil || proc.Signal(syscall.Signal(0)) != nil {
					os.RemoveAll(lockDir)
					continue
				}
			}
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("could not acquire lifecycle lock within %s", timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
