package lifecycle

import (
	"fmt"
	"os"
	"sync"
)

// Confirmer abstracts interactive terminal I/O so that production code can use
// real stdin/terminal detection and tests can inject scripted answers.
type Confirmer interface {
	// IsInteractive reports whether a human is on the other end of the terminal.
	IsInteractive() bool
	// ReadAnswer reads a single whitespace-delimited token from input.
	ReadAnswer() string
}

// TTYConfirmer reads from the real os.Stdin and checks whether it is a TTY.
type TTYConfirmer struct{}

// NewTTYConfirmer returns a Confirmer backed by os.Stdin.
func NewTTYConfirmer() *TTYConfirmer { return &TTYConfirmer{} }

func (c *TTYConfirmer) IsInteractive() bool {
	if os.Getenv("AIVM_FORCE_INTERACTIVE") == "1" {
		return true
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func (c *TTYConfirmer) ReadAnswer() string {
	var s string
	_, _ = fmt.Fscanln(os.Stdin, &s)
	return s
}

// SilentConfirmer is always non-interactive and returns "" for every answer.
// Use in daemon / CI contexts where stdin is not a terminal.
type SilentConfirmer struct{}

func (c *SilentConfirmer) IsInteractive() bool { return false }
func (c *SilentConfirmer) ReadAnswer() string  { return "" }

// ScriptedConfirmer replays a pre-loaded list of answers, one per ReadAnswer call.
// Use in tests to exercise interactive code paths.
type ScriptedConfirmer struct {
	mu      sync.Mutex
	answers []string
	i       int
}

// NewScriptedConfirmer returns a Confirmer that answers with the provided strings in order.
func NewScriptedConfirmer(answers ...string) *ScriptedConfirmer {
	return &ScriptedConfirmer{answers: answers}
}

func (c *ScriptedConfirmer) IsInteractive() bool { return true }

func (c *ScriptedConfirmer) ReadAnswer() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.i >= len(c.answers) {
		return ""
	}
	ans := c.answers[c.i]
	c.i++
	return ans
}
