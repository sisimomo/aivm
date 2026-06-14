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
	if ctx.GuestHome == "" {
		return ResolvedMount{}, fmt.Errorf("guest home is required for mount resolution")
	}

	source, err := renderPath(spec.Source, ctx)
	if err != nil {
		return ResolvedMount{}, fmt.Errorf("source: %w", err)
	}
	target, err := renderPath(spec.Target, ctx)
	if err != nil {
		return ResolvedMount{}, fmt.Errorf("target: %w", err)
	}

	writable, err := parseMode(spec.Mode)
	if err != nil {
		return ResolvedMount{}, err
	}

	source = expandTilde(source, ctx.Home)
	target = expandTilde(target, ctx.GuestHome)

	if !filepath.IsAbs(source) {
		return ResolvedMount{}, fmt.Errorf(
			"source %q must be absolute after expansion", source)
	}
	if !filepath.IsAbs(target) {
		return ResolvedMount{}, fmt.Errorf(
			"target %q must be absolute after expansion", target)
	}

	return ResolvedMount{
		HostPath:  filepath.Clean(source),
		GuestPath: filepath.Clean(target),
		Writable:  writable,
	}, nil
}

func renderPath(src string, ctx Context) (string, error) {
	t, err := template.New("").Funcs(plugin.TemplateFuncMap()).Parse(src)
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

func expandTilde(path, home string) string {
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
