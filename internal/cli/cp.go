package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func CpCmd(getApp func() (*App, error)) *cobra.Command {
	var recursive bool
	var force bool

	cmd := &cobra.Command{
		Use:   "cp <src> <dest>",
		Short: "Copy files between host and VM",
		Long: `Copy files or directories between the host and the VM.

Use the vm: prefix to refer to paths inside the VM.

Examples:
  aivm cp vm:/home/user/file.txt ./local/          Copy file from VM to host
  aivm cp ./local/file.txt vm:/home/user/          Copy file from host to VM
  aivm cp -r vm:/home/user/dir/ ./local/dir/       Copy directory from VM to host
  aivm cp -rf ./local/dir/ vm:/home/user/dir/      Copy directory to VM, overwrite`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoCp(cmd.Context(), app, args[0], args[1], recursive, force)
		},
	}

	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Copy directories recursively")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite destination if it already exists")

	return cmd
}

func DoCp(ctx context.Context, app *App, src, dst string, recursive, force bool) error {
	return app.Lifecycle.Copy(ctx, src, dst, recursive, force)
}
