package lifecycle_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

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

func TestPromptBootstrapRefresh_Accept(t *testing.T) {
	t.Parallel()
	c := lifecycle.NewScriptedConfirmer("y")
	var out bytes.Buffer
	if !lifecycle.PromptBootstrapRefresh(&out, c, 31*24*time.Hour, 30*24*time.Hour) {
		t.Fatal("want accept")
	}
}

func TestPromptBootstrapRefresh_Decline(t *testing.T) {
	t.Parallel()
	c := lifecycle.NewScriptedConfirmer("n")
	var out bytes.Buffer
	if lifecycle.PromptBootstrapRefresh(&out, c, 31*24*time.Hour, 30*24*time.Hour) {
		t.Fatal("want decline")
	}
}

func TestPromptCombined_VMExists_Option2(t *testing.T) {
	t.Parallel()
	c := lifecycle.NewScriptedConfirmer("2")
	var out bytes.Buffer
	got := lifecycle.PromptCombined(&out, c, true, 31*24*time.Hour, 30*24*time.Hour, 8*24*time.Hour, 7*24*time.Hour)
	if got != lifecycle.CombinedFastRecreate {
		t.Fatalf("got %v, want %v", got, lifecycle.CombinedFastRecreate)
	}
	if !strings.Contains(out.String(), "[3] Continue without changes") {
		t.Fatalf("expected continue option in prompt output, got %q", out.String())
	}
}

func TestPromptCombined_VMExists_Option3(t *testing.T) {
	t.Parallel()
	c := lifecycle.NewScriptedConfirmer("3")
	got := lifecycle.PromptCombined(&bytes.Buffer{}, c, true, 31*24*time.Hour, 30*24*time.Hour, 8*24*time.Hour, 7*24*time.Hour)
	if got != lifecycle.CombinedContinue {
		t.Fatalf("got %v, want %v", got, lifecycle.CombinedContinue)
	}
}

func TestPromptRuntimeChange_Decline(t *testing.T) {
	t.Parallel()
	c := lifecycle.NewScriptedConfirmer("n")
	if lifecycle.PromptRuntimeChange(&bytes.Buffer{}, c, "lima/vz", "docker") {
		t.Fatal("want decline")
	}
}
