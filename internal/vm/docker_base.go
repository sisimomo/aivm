package vm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

func (d *DockerVM) baseImageTag() string { return DockerBaseImageTag(d.profile) }

// SaveBaseImage commits the live container to a tagged base image.
func (d *DockerVM) SaveBaseImage(ctx context.Context, _ StartOptions) error {
	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()
	return dockerCmd(ctx, "commit", d.containerName, d.baseImageTag())
}

// RestoreFromBaseImage removes the live container and recreates it from the
// committed base image with the given start options.
func (d *DockerVM) RestoreFromBaseImage(ctx context.Context, opts StartOptions) error {
	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()

	_ = dockerCmd(ctx, "stop", d.containerName)
	_ = dockerCmd(ctx, "rm", "-f", d.containerName)
	return d.startFromImage(ctx, d.baseImageTag(), opts)
}

// DeleteBaseImage removes the committed base image tag if present.
func (d *DockerVM) DeleteBaseImage(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()

	err := dockerCmd(ctx, "rmi", "-f", d.baseImageTag())
	if err != nil && isDockerImageNotFound(err) {
		return nil
	}
	return err
}

func (d *DockerVM) FinalizeAfterBootstrap(
	ctx context.Context, runtimeOpts StartOptions,
) error {
	if d.baseImageEnable {
		if err := d.SaveBaseImage(ctx, runtimeOpts); err != nil {
			slog.Warn(fmt.Sprintf("save base image failed: %v", err))
			return d.promoteWithEphemeralCommit(ctx, runtimeOpts)
		}
		return d.RestoreFromBaseImage(ctx, runtimeOpts)
	}
	return d.promoteWithEphemeralCommit(ctx, runtimeOpts)
}

// promoteWithEphemeralCommit commits the live container to a temporary image,
// recreates the container with runtime start options, then removes the image.
func (d *DockerVM) promoteWithEphemeralCommit(ctx context.Context, opts StartOptions) error {
	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()

	imageID, err := dockerOutput(ctx, "commit", d.containerName)
	if err != nil {
		return fmt.Errorf("promote commit: %w", err)
	}
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return fmt.Errorf("promote commit: empty image id")
	}
	defer func(img string) {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), BaseImageOpTimeout)
		defer cleanupCancel()
		_ = dockerCmd(cleanupCtx, "rmi", "-f", img)
	}(imageID)

	_ = dockerCmd(ctx, "stop", d.containerName)
	_ = dockerCmd(ctx, "rm", "-f", d.containerName)
	if err := d.startFromImage(ctx, imageID, opts); err != nil {
		return fmt.Errorf("promote start from image: %w", err)
	}
	return nil
}

// HasBaseImage reports whether the committed base image tag exists locally.
func (d *DockerVM) HasBaseImage(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, BaseImageOpTimeout)
	defer cancel()
	return dockerCmd(ctx, "image", "inspect", d.baseImageTag()) == nil
}

func isDockerImageNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "No such image") || strings.Contains(msg, "image not known")
}
