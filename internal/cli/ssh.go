package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
	"aivm/internal/vm"
)

func SSHCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh",
		Short: "Open an interactive shell in the VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoSSH(cmd.Context(), app)
		},
	}
}

func DoSSH(ctx context.Context, app *App) error {
	status, err := app.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	workDir, _ := os.Getwd()
	sess, err := app.Sessions.Create(workDir)
	if err != nil {
		aivmlog.Warn("could not create session lock: %v", err)
	} else {
		defer sess.Remove()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		if sess != nil {
			sess.Remove()
		}
		os.Exit(0)
	}()

	return app.VM.SSH(ctx)
}
