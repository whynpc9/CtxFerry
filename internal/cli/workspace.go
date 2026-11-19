package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func workspaceCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error) *cobra.Command {
	group := &cobra.Command{Use: "workspace", Short: "Back up and recover a workspace"}
	var destination string
	var includeLocal bool
	backup := &cobra.Command{Use: "backup", Short: "Save a plaintext catalog snapshot and confirmed Git history", Args: cobra.NoArgs,
		RunE: invoke(func(a app.App) (any, error) { return a.BackupWorkspace(ctx, destination, includeLocal) })}
	backup.Flags().StringVar(&destination, "to", "", "New backup directory")
	backup.Flags().BoolVar(&includeLocal, "include-local-only", false, "Include local-only content and device settings")
	var source, expected string
	var newEpoch bool
	restore := &cobra.Command{Use: "restore", Short: "Publish an older workspace snapshot under a new epoch", Args: cobra.NoArgs,
		RunE: invoke(func(a app.App) (any, error) { return a.RestoreWorkspace(ctx, source, expected, newEpoch) })}
	restore.Flags().StringVar(&source, "from", "", "Workspace backup directory")
	restore.Flags().StringVar(&expected, "if-head", "", "Required current published Git commit")
	restore.Flags().BoolVar(&newEpoch, "new-epoch", false, "Explicitly establish a new workspace epoch")
	var localSource string
	recover := &cobra.Command{Use: "recover-local", Short: "Recover the saved device into a new application data directory", Args: cobra.NoArgs}
	recover.Flags().StringVar(&localSource, "from", "", "Workspace backup directory")
	recover.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			if !cmd.Flags().Changed("data-dir") {
				return nil, app.Fail("usage", "local recovery requires an explicit new --data-dir")
			}
			return a.RecoverLocalWorkspace(ctx, localSource)
		})(cmd, args)
	}
	group.AddCommand(backup, restore, recover)
	return group
}
