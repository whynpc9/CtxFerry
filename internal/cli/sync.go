package cli

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func syncCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error) *cobra.Command {
	group := &cobra.Command{Use: "sync", Short: "Synchronize shared plaintext context", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.Sync(ctx) })}
	var remote, branch, preview string
	enable := &cobra.Command{Use: "enable", Short: "Verify Git access and private GitHub identity (no upload)", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.EnableSync(ctx, remote, branch) })}
	enable.Flags().StringVar(&remote, "remote", "", "Dedicated private GitHub repository URL")
	enable.Flags().StringVar(&branch, "branch", "ctxferry-v1", "Sync branch")
	plan := &cobra.Command{Use: "preview", Short: "List exact plaintext upload files; inspect shared source material before publishing", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.PreviewSync(ctx) })}
	init := &cobra.Command{Use: "init", Short: "Explicitly create the sync branch with an expected-absent lease", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.InitSync(ctx, preview) })}
	init.Flags().StringVar(&preview, "preview", "", "Required current preview ID")
	status := &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.SyncStatus(ctx) })}
	check := &cobra.Command{Use: "check", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.CheckSync(ctx) })}
	group.AddCommand(enable, plan, init, status, check)
	return group
}

func joinCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error) *cobra.Command {
	var branch string
	var projects []string
	var all bool
	cmd := &cobra.Command{Use: "join <git-remote>", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&branch, "branch", "ctxferry-v1", "Existing sync branch")
	cmd.Flags().StringArrayVar(&projects, "subscribe", nil, "Project UUID or slug to subscribe to")
	cmd.Flags().BoolVar(&all, "all-projects", false, "Subscribe to every Project")
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) { return a.Join(ctx, args[0], branch, projects, all) })(c, args)
	}
	return cmd
}
