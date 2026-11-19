package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func skillCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error) *cobra.Command {
	group := &cobra.Command{Use: "skill", Short: "Import and install pinned user-level Skills"}
	var input app.SkillImport
	add := &cobra.Command{Use: "add", Short: "Snapshot one explicit local directory or pinned Git tree", Args: cobra.NoArgs}
	add.Flags().StringVar(&input.Local, "local", "", "Local directory containing SKILL.md")
	add.Flags().StringVar(&input.Git, "git", "", "Git URL or absolute repository path")
	add.Flags().StringVar(&input.Subdir, "subdir", "", "Git path containing SKILL.md (default: repository root)")
	add.Flags().StringVar(&input.Ref, "ref", "", "Full Git commit hash or refs/tags/<release>")
	add.Flags().StringVar(&input.License, "license", "", "License identifier (defaults to SKILL.md license or NOASSERTION)")
	add.Flags().StringArrayVar(&input.Platforms, "platform", nil, "Supported OS or OS/architecture (repeatable)")
	add.Flags().StringArrayVar(&input.Dependencies, "depends-on", nil, "Executable required on PATH (repeatable)")
	add.RunE = invoke(func(a app.App) (any, error) { return a.AddSkill(ctx, input) })
	group.AddCommand(add)
	group.AddCommand(skillBootstrapCommand(ctx, invoke))

	for _, action := range []struct {
		name, help string
	}{
		{"plan", "Preview files, collisions and requirements without writing"},
		{"apply", "Install or resume installation of a pinned Skill"},
	} {
		var target string
		var symlink bool
		command := &cobra.Command{Use: action.name + " <name>", Short: action.help, Args: cobra.ExactArgs(1)}
		command.Flags().StringVar(&target, "target", "", "User-level target: codex or claude-code")
		command.Flags().BoolVar(&symlink, "symlink", false, "Project a verified link to the local Skill package")
		command.RunE = func(cmd *cobra.Command, args []string) error {
			return invoke(func(a app.App) (any, error) {
				if action.name == "plan" {
					return a.PlanSkill(args[0], target, symlink)
				}
				return a.ApplySkill(ctx, args[0], target, symlink)
			})(cmd, args)
		}
		group.AddCommand(command)
	}
	return group
}

func renderSkillPlan(w io.Writer, p app.SkillPlan) error {
	if _, err := fmt.Fprintf(w, "%s → %s (%s, %s)\n", p.Package.Name, p.Path, p.Mode, p.Status); err != nil {
		return err
	}
	for _, file := range p.Writes {
		if file.Kind == "symlink" {
			if _, err := fmt.Fprintf(w, "  symlink %s → %s\n", file.Path, p.LinkSource); err != nil {
				return err
			}
			continue
		}
		mode := ""
		if file.Executable {
			mode = " (executable)"
		}
		if _, err := fmt.Fprintf(w, "  file %s%s\n", file.Path, mode); err != nil {
			return err
		}
	}
	for _, problem := range p.Issues {
		if _, err := fmt.Fprintf(w, "  %s: %s\n", problem.Code, problem.Message); err != nil {
			return err
		}
		for _, path := range problem.Paths {
			if _, err := fmt.Fprintf(w, "    %s\n", path); err != nil {
				return err
			}
		}
	}
	return nil
}
