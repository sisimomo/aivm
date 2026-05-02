package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Session struct {
	PID        int
	StartEpoch int64
	Dir        string
	LockFile   string
}

type Store struct {
	dir string
}

func NewStore(stateDir string) *Store {
	return &Store{dir: filepath.Join(stateDir, "sessions")}
}

func (s *Store) Create(workDir string) (*Session, error) {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return nil, err
	}
	pid := os.Getpid()
	start := time.Now().Unix()
	lockFile := filepath.Join(s.dir, fmt.Sprintf("%d.lock", pid))

	content := fmt.Sprintf("%d %d\n%s\n", pid, start, workDir)
	if err := os.WriteFile(lockFile, []byte(content), 0644); err != nil {
		return nil, err
	}
	return &Session{PID: pid, StartEpoch: start, Dir: workDir, LockFile: lockFile}, nil
}

func (s *Session) Remove() {
	os.Remove(s.LockFile)
}

func (s *Store) CountActive() (int, error) {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}

	active := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".lock" {
			continue
		}
		lockPath := filepath.Join(s.dir, entry.Name())
		sess, err := readLock(lockPath)
		if err != nil {
			os.Remove(lockPath)
			continue
		}
		if isAlive(sess.PID) {
			active++
		} else {
			os.Remove(lockPath)
		}
	}
	return active, nil
}

func (s *Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sessions []*Session
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".lock" {
			continue
		}
		lockPath := filepath.Join(s.dir, entry.Name())
		sess, err := readLock(lockPath)
		if err != nil {
			continue
		}
		if isAlive(sess.PID) {
			sessions = append(sessions, sess)
		}
	}
	return sessions, nil
}

func readLock(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(string(data))
	if len(lines) < 1 {
		return nil, fmt.Errorf("empty lock file")
	}
	var pid int
	var epoch int64
	n, err := fmt.Sscanf(lines[0], "%d %d", &pid, &epoch)
	if err != nil || n < 1 || pid <= 0 {
		return nil, fmt.Errorf("invalid lock format")
	}
	dir := ""
	if len(lines) > 1 {
		dir = lines[1]
	}
	return &Session{PID: pid, StartEpoch: epoch, Dir: dir, LockFile: path}, nil
}

func isAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			line := s[start:i]
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func (s *Store) LastActiveFile() string {
	return filepath.Join(filepath.Dir(s.dir), "last-session-end")
}

func (s *Store) WriteLastActive() {
	os.WriteFile(s.LastActiveFile(), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}

func (s *Store) ReadLastActive() time.Time {
	data, err := os.ReadFile(s.LastActiveFile())
	if err != nil {
		return time.Now()
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.Unix(epoch, 0)
}

func (s *Store) vmStoppedAtFile() string {
	return filepath.Join(filepath.Dir(s.dir), "vm-stopped-at")
}

func (s *Store) WriteVMStoppedAt() {
	os.WriteFile(s.vmStoppedAtFile(), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}

func (s *Store) ReadVMStoppedAt() time.Time {
	data, err := os.ReadFile(s.vmStoppedAtFile())
	if err != nil {
		return time.Time{}
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(epoch, 0)
}

func (s *Store) ClearVMStoppedAt() {
	os.Remove(s.vmStoppedAtFile())
}

// KillAll sends SIGTERM to every active session process and returns the PIDs that were signalled.
func (s *Store) KillAll() []int {
	sessions, _ := s.List()
	var killed []int
	for _, sess := range sessions {
		proc, err := os.FindProcess(sess.PID)
		if err == nil {
			if proc.Signal(syscall.SIGTERM) == nil {
				killed = append(killed, sess.PID)
			}
		}
	}
	return killed
}

// CountLegacy returns the number of active sessions that started before the given time.
// Used during VM transitions to determine when the legacy VM can be safely removed.
func (s *Store) CountLegacy(startedBefore time.Time) (int, error) {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".lock" {
			continue
		}
		lockPath := filepath.Join(s.dir, entry.Name())
		sess, err := readLock(lockPath)
		if err != nil {
			os.Remove(lockPath)
			continue
		}
		if isAlive(sess.PID) && time.Unix(sess.StartEpoch, 0).Before(startedBefore) {
			count++
		}
	}
	return count, nil
}
