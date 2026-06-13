package lifecycle_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/session"
)

func seedFakeSession(t *testing.T, store *session.Store, workDir string) func() {
	t.Helper()
	cmd := exec.Command("sleep", "3600")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cleanup := func() { _ = cmd.Process.Kill() }

	dir := filepath.Join(filepath.Dir(store.LastActiveFile()), "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		cleanup()
		t.Fatal(err)
	}
	lockFile := filepath.Join(dir, fmt.Sprintf("%d.lock", cmd.Process.Pid))
	content := fmt.Sprintf("%d %d\n%s\n", cmd.Process.Pid, time.Now().Unix(), workDir)
	if err := os.WriteFile(lockFile, []byte(content), 0644); err != nil {
		cleanup()
		t.Fatal(err)
	}
	return cleanup
}

func TestTerminateActiveSessions_RemovesLocks(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	sessions := session.NewStore(stateDir)
	cleanup := seedFakeSession(t, sessions, "/tmp/work")
	defer cleanup()
	if n, err := sessions.CountActive(); err != nil || n != 1 {
		t.Fatalf("expected 1 active session, got %d err=%v", n, err)
	}

	svc := &lifecycle.LifecycleService{Sessions: sessions}
	lifecycle.TerminateActiveSessionsForTest(svc)

	if n, err := sessions.CountActive(); err != nil || n != 0 {
		t.Fatalf("expected 0 active sessions after terminate, got %d err=%v", n, err)
	}
}
