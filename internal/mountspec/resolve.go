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
	if strings.TrimSpace(spec.Location) == "" {
		return ResolvedMount{}, fmt.Errorf("mount location is required")
	}
	if strings.TrimSpace(spec.MountPoint) == "" {
		return ResolvedMount{}, fmt.Errorf("mount mountPoint is required")
	}
	if strings.TrimSpace(spec.Mode) == "" {
		return ResolvedMount{}, fmt.Errorf("mount mode is required")
	}

	location, err := renderPath(spec.Location, ctx)
	if err != nil {
		return ResolvedMount{}, fmt.Errorf("location: %w", err)
	}
	mountPoint, err := renderPath(spec.MountPoint, ctx)
	if err != nil {
		return ResolvedMount{}, fmt.Errorf("mountPoint: %w", err)
	}

	writable, err := parseMode(spec.Mode)
	if err != nil {
		return ResolvedMount{}, err
	}

	location = expandTilde(location, ctx.Home)
	mountPoint = expandTilde(mountPoint, ctx.Home)

	if !filepath.IsAbs(location) {
		return ResolvedMount{}, fmt.Errorf(
			"location %q must be absolute after expansion", location)
	}
	if !filepath.IsAbs(mountPoint) {
		return ResolvedMount{}, fmt.Errorf(
			"mountPoint %q must be absolute after expansion", mountPoint)
	}

	return ResolvedMount{
		HostPath:  filepath.Clean(location),
		GuestPath: filepath.Clean(mountPoint),
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
		"home":      ctx.Home,
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
