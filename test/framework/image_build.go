package framework

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sisimomo/aivm/internal/plugin"
)

var (
	buildImageOnce sync.Once
	buildImageErr  error
)

// bootstrapDockerfileTemplate is the structural skeleton of the test base image.
// All shell scripting is injected at build time from plugin definitions:
//   - system-setup.sh  — sourced from the "system" plugin in defaults.yaml
//   - mise-setup.sh    — sourced from the "mise" plugin in defaults.yaml
//
// The only exception is the mise-node step: "mise-node" is a dynamic plugin
// synthesised by Go code in mise_plugin.go and has no YAML entry. Its setup
// for the default config (version=latest, no extras) is the single line
// `mise use --global node@latest`, derived directly from misePlugin.Setup().
//
// Minimal non-YAML prerequisites (sudo, python3) are kept here because:
//   - sudo   — the system plugin's own setup script calls "sudo apt-get"
//   - python3 — expected by some bootstrap scripts but absent from the system plugin
const bootstrapDockerfileTemplate = `FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Minimal prerequisites not covered by any plugin in defaults.yaml.
# sudo   — required by the system plugin setup script (sudo apt-get).
# python3 — required by some bootstrap install scripts.
RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
      sudo \
      python3 && \
    rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash user && \
    echo "user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/user && \
    chmod 0440 /etc/sudoers.d/user

USER user
ENV HOME=/home/user
WORKDIR /home/user

# "system" plugin setup — sourced from internal/plugin/defaults.yaml.
# Rebuilt automatically whenever the setup script changes.
COPY system-setup.sh /tmp/system-setup.sh
RUN bash /tmp/system-setup.sh

# "mise" plugin setup — sourced from internal/plugin/defaults.yaml.
# Rebuilt automatically whenever the setup script changes.
COPY mise-setup.sh /tmp/mise-setup.sh
RUN bash /tmp/mise-setup.sh

# Make mise available for subsequent RUN steps.
ENV PATH="/home/user/.local/bin:${PATH}"

# "mise-node" plugin pre-installation.
# mise-node is a dynamic plugin (see internal/plugin/mise_plugin.go), not a
# YAML entry. For default config (version=latest, no extras) its Setup() emits
# exactly this one line. Pre-installed to avoid slow downloads on every test
# run; claude and t3code both declare a mise-node dependency.
RUN mise use --global node@latest && mise reshim

CMD ["sleep", "infinity"]
`

// BuildTestImage generates a Dockerfile from the plugin definitions in
// defaults.yaml and builds the test base image (aivm-test-base:latest).
// The image is only rebuilt when the generated content changes, detected via
// a SHA-256 hash stored as a Docker label.
//
// Safe to call concurrently: the build runs at most once per process.
func BuildTestImage() error {
	buildImageOnce.Do(func() {
		buildImageErr = doBuildTestImage()
	})
	return buildImageErr
}

func doBuildTestImage() error {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		return fmt.Errorf("load plugin defaults: %w", err)
	}

	systemSetup, err := pluginSetupScript(defs, "system")
	if err != nil {
		return err
	}
	miseSetup, err := pluginSetupScript(defs, "mise")
	if err != nil {
		return err
	}

	// Hash everything that determines the image content so stale images are detected.
	hash := imageContentHash(bootstrapDockerfileTemplate, systemSetup, miseSetup)

	// Skip rebuild if the existing image already matches.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	existingHash, err := dockerImageLabel(ctx, TestImageName, "aivm-dockerfile-hash")
	cancel()
	if err == nil && existingHash == hash {
		return nil
	}

	dir, err := os.MkdirTemp("", "aivm-test-image-*")
	if err != nil {
		return fmt.Errorf("create build context dir: %w", err)
	}
	defer os.RemoveAll(dir)

	files := map[string]string{
		"Dockerfile":      bootstrapDockerfileTemplate,
		"system-setup.sh": systemSetup,
		"mise-setup.sh":   miseSetup,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer buildCancel()

	cmd := exec.CommandContext(buildCtx, "docker", "build",
		"--label", "aivm-dockerfile-hash="+hash,
		"-t", TestImageName,
		dir,
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		if buildCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker build: timeout after 10m\n%s", buf.String())
		}
		return fmt.Errorf("docker build: %w\n%s", err, buf.String())
	}

	return nil
}

// pluginSetupScript returns the trimmed setup script for the named plugin.
func pluginSetupScript(defs map[string]plugin.PluginDef, name string) (string, error) {
	def, ok := defs[name]
	if !ok {
		return "", fmt.Errorf("plugin %q not found in defaults.yaml", name)
	}
	if def.Setup == "" {
		return "", fmt.Errorf("plugin %q has no setup script", name)
	}
	return strings.TrimSpace(def.Setup), nil
}

// imageContentHash returns a hex SHA-256 over all parts that determine the
// image content. Changing any part triggers a rebuild.
func imageContentHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0}) // null separator between parts
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// dockerImageLabel inspects the named Docker image and returns the value of
// the given label. Returns empty string and error if the image does not exist,
// the label is absent, or the operation times out.
func dockerImageLabel(ctx context.Context, image, label string) (string, error) {
	out, err := exec.CommandContext(ctx,
		"docker", "inspect",
		"--format", fmt.Sprintf(`{{index .Config.Labels %q}}`, label),
		image,
	).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("docker inspect: timeout")
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
