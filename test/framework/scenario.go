// Package framework provides the integration testing harness for AIVM.
package framework

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// StepFunc is a function that executes a single step in a scenario.
// It receives the test context and harness, and returns an error on failure.
type StepFunc func(ctx context.Context, h *Harness) error

// ConditionFunc is a function polled by Wait until it returns true or the
// timeout expires.
type ConditionFunc func(ctx context.Context, h *Harness) (bool, error)

// AssertFunc is a synchronous assertion checked immediately when the step runs.
type AssertFunc func(ctx context.Context, h *Harness) error

// step is an internal representation of one step in a Scenario.
type step struct {
	name    string
	kind    stepKind
	fn      StepFunc
	cond    ConditionFunc
	assert  AssertFunc
	timeout time.Duration
}

type stepKind int

const (
	kindAction stepKind = iota
	kindWait
	kindAssert
)

// Scenario is a sequence of named steps that together test one lifecycle path.
// Build it with the fluent API: .Step().Wait().Assert().Run().
type Scenario struct {
	name  string
	h     *Harness
	steps []step
}

func newScenario(name string, h *Harness) *Scenario {
	return &Scenario{name: name, h: h}
}

// Step adds an action step that runs fn synchronously.
// The step fails the test if fn returns a non-nil error.
func (s *Scenario) Step(name string, fn StepFunc) *Scenario {
	s.steps = append(s.steps, step{name: name, kind: kindAction, fn: fn})
	return s
}

// Wait adds a polling step that calls cond every 500ms until it returns
// true or timeout elapses.  The step fails the test on timeout.
func (s *Scenario) Wait(name string, cond ConditionFunc, timeout time.Duration) *Scenario {
	s.steps = append(s.steps, step{name: name, kind: kindWait, cond: cond, timeout: timeout})
	return s
}

// Assert adds a synchronous assertion step. The step fails the test if assert
// returns a non-nil error.
func (s *Scenario) Assert(name string, assert AssertFunc) *Scenario {
	s.steps = append(s.steps, step{name: name, kind: kindAssert, assert: assert})
	return s
}

// Run executes all steps in order. It calls t.Helper() so failures are
// attributed to the call site.
func (s *Scenario) Run() {
	s.h.t.Helper()
	t := s.h.t

	progress("[scenario] %s\n", s.name)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i, st := range s.steps {
		progress("  [%d/%d] %s\n", i+1, len(s.steps), st.name)

		var err error
		switch st.kind {
		case kindAction:
			err = st.fn(ctx, s.h)
		case kindWait:
			err = runWait(ctx, t, st, s.h)
		case kindAssert:
			err = st.assert(ctx, s.h)
		}

		if err != nil {
			t.Logf("--- output at failure ---\n%s%s", s.h.Output.Stdout(), s.h.Output.Stderr())
			t.Fatalf("scenario %q — step %q failed: %v", s.name, st.name, err)
		}
	}

	progress("  [ok] %s\n", s.name)
}

// runWait polls cond every 500ms until it returns true or timeout elapses.
func runWait(ctx context.Context, t *testing.T, st step, h *Harness) error {
	t.Helper()
	deadline := time.Now().Add(st.timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		ok, err := st.cond(ctx, h)
		if err != nil {
			return fmt.Errorf("condition error: %w", err)
		}
		if ok {
			return nil
		}
		remaining := time.Until(deadline).Round(time.Second)
		if remaining <= 0 {
			return fmt.Errorf("timed out after %s", st.timeout)
		}
		progress("    … waiting (%s remaining)\n", remaining)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// progress writes a message directly to stderr so it appears immediately
// during a long-running test (t.Logf is buffered until test completion).
func progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
