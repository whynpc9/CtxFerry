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
	for _, action := range []struct {
		name, help string
	}{
		{"update", "Preview or confirm an explicit Skill pin change"},
		{"rollback", "Preview or confirm restoration of the previous installed Skill"},
	} {
		input := app.SkillChangeInput{Rollback: action.name == "rollback"}
		command := &cobra.Command{Use: action.name + " <name>", Short: action.help, Args: cobra.ExactArgs(1)}
		command.Flags().StringVar(&input.Target, "target", "", "User-level target: codex or claude-code")
		command.Flags().StringVar(&input.Confirm, "confirm", "", "Confirmation from the exact preview of this change")
		if !input.Rollback {
			command.Flags().StringVar(&input.Ref, "ref", "", "Full Git commit hash or refs/tags/<release> (required for Git)")
			command.Flags().StringVar(&input.License, "license", "", "License identifier (defaults to the new Skill's license or NOASSERTION)")
		}
		command.RunE = func(cmd *cobra.Command, args []string) error {
			return invoke(func(a app.App) (any, error) {
				input.Name = args[0]
				return a.ChangeSkill(ctx, input)
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
	return renderSkillProblems(w, p.Issues)
}

func renderSkillChange(w io.Writer, p app.SkillChangePlan) error {
	if _, err := fmt.Fprintf(w, "%s %s → %s (%s, %s)\n", p.Action, p.Before.Name, p.Path, p.Mode, p.Status); err != nil {
		return err
	}
	for _, pin := range []struct {
		label string
		pkg   *app.SkillPackage
		link  string
	}{{"Before", &p.Before, p.BeforeLink}, {"After", p.After, p.AfterLink}} {
		if pin.pkg == nil {
			continue
		}
		pkg := pin.pkg
		if _, err := fmt.Fprintf(w, "  %s: %s [%s] sha256:%s (license %s, compatibility %q)\n",
			pin.label, pkg.SourceURL, pkg.Subdir, pkg.Digest, pkg.License, pkg.Compatibility); err != nil {
			return err
		}
		if pin.link != "" {
			if _, err := fmt.Fprintf(w, "    symlink → %s\n", pin.link); err != nil {
				return err
			}
		}
		if pkg.Commit != "" {
			if _, err := fmt.Fprintf(w, "    commit %s  release %q\n", pkg.Commit, pkg.Release); err != nil {
				return err
			}
		}
	}
	for _, change := range p.Changes {
		if change.Before != nil {
			if err := renderSkillChangeFile(w, "-", change.Before); err != nil {
				return err
			}
		}
		if change.After != nil {
			if err := renderSkillChangeFile(w, "+", change.After); err != nil {
				return err
			}
		}
	}
	if err := renderSkillProblems(w, p.Issues); err != nil {
		return err
	}
	if p.Confirmation != "" {
		_, err := fmt.Fprintf(w, "  Confirm with --confirm %s\n", p.Confirmation)
		return err
	}
	return nil
}

func renderSkillChangeFile(w io.Writer, prefix string, file *app.SkillFile) error {
	_, err := fmt.Fprintf(w, "  %s %s sha256:%s (executable=%t)\n", prefix, file.Path, file.Digest, file.Executable)
	return err
}

func renderSkillProblems(w io.Writer, issues []app.SkillProblem) error {
	for _, problem := range issues {
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
