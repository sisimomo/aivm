package harness

import (
	"fmt"
	"strings"
)

type harnessConfig struct {
	backend                     string
	vmType                      string
	baseImageEnable             bool
	recreatePromptAfter         string
	bootstrapRefreshPromptAfter string
	scriptedAnswers             []string
}

func defaultHarnessConfig() harnessConfig {
	return harnessConfig{
		backend:                     "docker",
		vmType:                      "",
		baseImageEnable:             true,
		recreatePromptAfter:         "-1",
		bootstrapRefreshPromptAfter: "30d",
	}
}

func buildHarnessYAML(cfg harnessConfig) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "vm:\n")
	fmt.Fprintf(&sb, "  backend: %q\n", cfg.backend)
	if cfg.backend == "docker" {
		fmt.Fprintf(&sb, "  docker_image: %q\n", "aivm-test:latest")
	}
	if cfg.vmType != "" {
		fmt.Fprintf(&sb, "  type: %q\n", cfg.vmType)
	}
	fmt.Fprintf(&sb, "  base_image_enable: %v\n", cfg.baseImageEnable)
	fmt.Fprintf(&sb, "  recreate_prompt_after: %q\n", cfg.recreatePromptAfter)
	if cfg.bootstrapRefreshPromptAfter != "" {
		fmt.Fprintf(&sb, "  bootstrap_refresh_prompt_after: %q\n", cfg.bootstrapRefreshPromptAfter)
	}
	fmt.Fprintf(&sb, "idle:\n")
	fmt.Fprintf(&sb, "  stop_timeout: %q\n", "5m")
	fmt.Fprintf(&sb, "  delete_timeout: %q\n", "5m")
	fmt.Fprintf(&sb, "agents:\n")
	fmt.Fprintf(&sb, "  default: claude\n")
	fmt.Fprintf(&sb, "  enabled:\n")
	fmt.Fprintf(&sb, "    - claude\n")
	fmt.Fprintf(&sb, "plugins:\n")
	fmt.Fprintf(&sb, "  enabled: []\n")
	return sb.String()
}
