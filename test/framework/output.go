package framework

import (
	"bytes"
	"sync"
)

// OutputBuffer captures stdout and stderr from the LifecycleService logger
// for assertion in e2e tests.
// All methods are safe for concurrent use.
type OutputBuffer struct {
	mu     sync.Mutex
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// Write implements io.Writer for stdout capture.
func (b *OutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.Write(p)
}

// WriteErr implements io.Writer for stderr capture.
func (b *OutputBuffer) WriteErr(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stderr.Write(p)
}

// Stdout returns all captured stdout as a string.
func (b *OutputBuffer) Stdout() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.String()
}

// Stderr returns all captured stderr as a string.
func (b *OutputBuffer) Stderr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stderr.String()
}

// Reset clears both stdout and stderr buffers. Call between CLI invocations
// when you want to assert on only the most recent command's output.
func (b *OutputBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stdout.Reset()
	b.stderr.Reset()
}

// stderrWriter is a thin io.Writer that routes to OutputBuffer.WriteErr.
type stderrWriter struct{ buf *OutputBuffer }

func (w *stderrWriter) Write(p []byte) (int, error) { return w.buf.WriteErr(p) }
