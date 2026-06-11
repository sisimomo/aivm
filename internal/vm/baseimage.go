package vm

import "time"

func LimaShadowProfile(live string) string { return live + "-base" }

func DockerBaseImageTag(profile string) string { return "aivm-" + profile + "-base" }

const BaseImageOpTimeout = 5 * time.Minute
