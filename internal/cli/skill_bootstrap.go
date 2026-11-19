package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func skillBootstrapCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error) *cobra.Command {
	group := &cobra.Command{Use: "bootstrap", Short: "Manage the local CtxFerry agent entry"}
	for _, action := range []string{"install", "uninstall"} {
		var target, preview string
		cmd := &cobra.Command{
			Use:   action,
			Short: "Preview changes to the CtxFerry Skill and global instructions",
			Args:  cobra.NoArgs,
			RunE: invoke(func(a app.App) (any, error) {
				return a.BootstrapAgentEntry(ctx, target, action, preview)
			}),
		}
		cmd.Flags().StringVar(&target, "target", "", "User-level target: codex or claude-code")
		cmd.Flags().StringVar(&preview, "preview", "", "Apply the token from the current preview")
		group.AddCommand(cmd)
	}
	return group
}

func renderAgentEntry(w io.Writer, plan app.AgentEntryPlan) error {
	if _, err := fmt.Fprintf(w, "%s bootstrap %s (%s)\n", plan.Target, plan.Action, plan.Status); err != nil {
		return err
	}
	for _, write := range plan.Writes {
		if _, err := fmt.Fprintf(w, "  %s %s\n", write.Action, write.Path); err != nil {
			return err
		}
		if plan.Preview != "" && write.Content != "" {
			for line := range strings.SplitSeq(strings.TrimSuffix(write.Content, "\n"), "\n") {
				if _, err := fmt.Fprintf(w, "    %s\n", line); err != nil {
					return err
				}
			}
		}
	}
	if plan.Preview != "" {
		_, err := fmt.Fprintf(w, "\nTo apply: ctxferry skill bootstrap %s --target %s --preview %s\n", plan.Action, plan.Target, plan.Preview)
		return err
	}
	return nil
}
