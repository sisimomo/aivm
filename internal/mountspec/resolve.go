package mountspec

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sisimomo/aivm/internal/plugin"
)

func Resolve(spec MountSpec, ctx Context) (ResolvedMount, error) {
	if strings.TrimSpace(spec.Source) == "" {
		return ResolvedMount{}, fmt.Errorf("mount source is required")
	}
	if strings.TrimSpace(spec.Target) == "" {
		return ResolvedMount{}, fmt.Errorf("mount target is required")
	}
	if strings.TrimSpace(spec.Mode) == "" {
		return ResolvedMount{}, fmt.Errorf("mount mode is required")
	}
	if ctx.VMHome == "" {
		return ResolvedMount{}, fmt.Errorf("vm home is required for mount resolution")
	}

	source, err := ResolveHostPath(spec.Source, ctx)
	if err != nil {
		return ResolvedMount{}, fmt.Errorf("source: %w", err)
	}
	target, err := ResolveGuestPath(spec.Target, ctx)
	if err != nil {
		return ResolvedMount{}, fmt.Errorf("target: %w", err)
	}

	writable, err := parseMode(spec.Mode)
	if err != nil {
		return ResolvedMount{}, err
	}

	return ResolvedMount{
		HostPath:  source,
		GuestPath: target,
		Writable:  writable,
	}, nil
}

// ResolveHostPath renders templates, expands ~ to the host home directory, and
// requires an absolute path.
func ResolveHostPath(raw string, ctx Context) (string, error) {
	return resolvePath(raw, ctx, ctx.Home)
}

// ResolveGuestPath renders templates, expands ~ to the VM home directory, and
// requires an absolute path.
func ResolveGuestPath(raw string, ctx Context) (string, error) {
	return resolvePath(raw, ctx, ctx.VMHome)
}

func resolvePath(raw string, ctx Context, tildeHome string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	rendered, err := RenderPath(raw, ctx)
	if err != nil {
		return "", err
	}
	expanded := ExpandTilde(rendered, tildeHome)
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("path %q must be absolute after expansion (got %q)", raw, expanded)
	}
	return filepath.Clean(expanded), nil
}

// RenderPath expands {{ .host_home }} and {{ .state_dir }} templates in raw.
func RenderPath(src string, ctx Context) (string, error) {
	t, err := template.New("").
		Option("missingkey=error").
		Funcs(plugin.TemplateFuncMap()).
		Parse(src)
	if err != nil {
		return "", err
	}
	data := map[string]string{
		"state_dir": ctx.StateDir,
		"host_home": ctx.Home,
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// ExpandTilde replaces a leading ~ with home.
func ExpandTilde(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func parseMode(mode string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "rw":
		return true, nil
	case "ro":
		return false, nil
	default:
		return false, fmt.Errorf("unknown mode %q — use \"rw\" or \"ro\"", mode)
	}
}
