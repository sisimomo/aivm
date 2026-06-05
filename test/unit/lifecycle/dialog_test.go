package lifecycle_test

import (
	"bytes"
	"testing"

	"github.com/sisimomo/aivm/internal/lifecycle"
)

func TestPromptYesNoNonInteractiveDefault(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	c := &lifecycle.SilentConfirmer{}

	if got := lifecycle.PromptYesNo(&out, c, "prompt? [y/N] ", false); got {
		t.Fatal("default No: want false")
	}
	if got := lifecycle.PromptYesNo(&out, c, "prompt? [y/N] ", true); !got {
		t.Fatal("default Yes: want true")
	}
	if out.Len() != 0 {
		t.Fatalf("non-interactive should not write prompt, got %q", out.String())
	}
}

func TestPromptConfigChangedNonInteractive(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if lifecycle.PromptConfigChanged(&out, &lifecycle.SilentConfirmer{}) {
		t.Fatal("non-interactive config change: want default No")
	}
}
