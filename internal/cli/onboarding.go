package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func onboardingCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, selector *string) *cobra.Command {
	group := &cobra.Command{Use: "onboard", Short: "Set up the first Project and import selected files"}
	var repository string
	selection := func() app.OnboardingSelection {
		return app.OnboardingSelection{Project: *selector, Repository: repository}
	}
	start := &cobra.Command{Use: "start <directory>", Short: "Save the directory and preview its Project", Args: cobra.ExactArgs(1)}
	start.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			return a.StartOnboarding(ctx, args[0], selection())
		})(cmd, args)
	}
	status := &cobra.Command{Use: "status", Short: "Resume from saved progress and review the Project preview", Args: cobra.NoArgs,
		RunE: invoke(func(a app.App) (any, error) { return a.OnboardingStatus(ctx, selection()) }),
	}
	var previewID string
	bind := &cobra.Command{Use: "bind", Short: "Confirm the previewed Project and bind the saved directory", Args: cobra.NoArgs,
		RunE: invoke(func(a app.App) (any, error) { return a.ConfirmOnboardingBinding(ctx, selection(), previewID) }),
	}
	bind.Flags().StringVar(&previewID, "preview", "", "Project preview ID from onboard status")
	for _, command := range []*cobra.Command{start, status, bind} {
		command.Flags().StringVar(&repository, "repository", "", "Select a Repository UUID for the Project")
	}
	scan := &cobra.Command{Use: "scan <note>", Short: "Report credential-like findings for one selected note", Args: cobra.ExactArgs(1)}
	scan.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			return a.ScanOnboardingNote(args[0])
		})(cmd, args)
	}
	var note app.NoteImport
	importNote := &cobra.Command{Use: "import <note>", Short: "Import one explicitly classified note", Args: cobra.ExactArgs(1)}
	importNote.Flags().StringVar(&note.Title, "title", "", "Required agent-visible title")
	importNote.Flags().StringVar(&note.Summary, "summary", "", "Explicit agent-visible summary")
	importNote.Flags().StringVar(&note.Policy.Storage, "storage", "", "Required: local-only or shared")
	importNote.Flags().StringVar(&note.Policy.AgentDisclosure, "disclosure", "", "Required: full, summary, reference or local-tool")
	importNote.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			return a.ImportOnboardingNote(ctx, args[0], note)
		})(cmd, args)
	}
	finish := &cobra.Command{Use: "finish", Short: "Run doctor and complete onboarding", Args: cobra.NoArgs,
		RunE: invoke(func(a app.App) (any, error) { return a.FinishOnboarding(ctx) }),
	}
	scanEnv := &cobra.Command{Use: "scan-env <file>", Short: "List field names and matching Task requirements for one selected env file", Args: cobra.ExactArgs(1)}
	scanEnv.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			return a.ScanOnboardingEnv(ctx, *selector, args[0])
		})(cmd, args)
	}
	var env app.EnvImport
	importEnv := &cobra.Command{Use: "import-env <file>", Short: "Preview or confirm one env field's provider import and Profile mapping", Args: cobra.ExactArgs(1)}
	flags := importEnv.Flags()
	flags.StringVar(&env.Profile, "profile", "", "Existing Project Profile name")
	flags.StringVar(&env.Task, "task", "", "Existing Task name or UUID")
	flags.StringVar(&env.Requirement, "requirement", "", "Task credential requirement name")
	flags.StringVar(&env.Field, "field", "", "Source variable name")
	flags.StringVar(&env.ResourceID, "resource", "", "Shared Resource UUID for the requirement's slot")
	flags.StringVar(&env.Alias, "alias", "", "New SecretBinding alias")
	flags.StringVar(&env.ProviderProfile.Provider, "provider", "", "Secret provider name")
	flags.StringVar(&env.ProviderProfile.Name, "provider-profile", "", "Device provider profile name")
	flags.StringVar(&env.ProviderProfile.Method, "method", "", "Provider authentication method")
	flags.StringVar(&env.Locator.ProjectID, "backend-project", "", "Exact backend project ID")
	flags.StringVar(&env.Locator.Environment, "environment", "", "Exact backend environment")
	flags.StringVar(&env.Locator.Path, "path", "", "Exact backend folder path (use / for root)")
	flags.StringVar(&env.Locator.Key, "key", "", "Exact backend entry key")
	flags.BoolVar(&env.Recover, "recover", false, "Verify an existing entry against the source before mapping it")
	flags.StringVar(&env.Confirm, "confirm", "", "Preview ID from the same command without --confirm")
	importEnv.RunE = func(cmd *cobra.Command, args []string) error {
		if flags.Changed("confirm") && env.Confirm == "" {
			return app.Fail("usage", "pass a nonempty --confirm preview ID")
		}
		return invoke(func(a app.App) (any, error) {
			return a.ImportOnboardingEnv(ctx, *selector, args[0], env)
		})(cmd, args)
	}
	group.AddCommand(start, status, bind, scan, importNote, scanEnv, importEnv, finish)
	return group
}

func renderOnboarding(w io.Writer, state app.OnboardingState) error {
	switch state.Step {
	case "not-started":
		_, err := fmt.Fprintln(w, "Choose a directory with ctxferry onboard start <directory>")
		return err
	case "preview":
		preview := state.Preview
		if _, err := fmt.Fprintf(w, "Directory: %s\nGit: %s\n", state.Directory, preview.Git.Status); err != nil {
			return err
		}
		for _, candidate := range preview.Candidates {
			if _, err := fmt.Fprintf(w, "Candidate: %s (%s), Repository %s [%s]\n", candidate.Project.Slug, candidate.Project.ID, candidate.RepositoryID, candidate.Evidence); err != nil {
				return err
			}
		}
		if preview.ID == "" {
			hint := "Select a Project with ctxferry onboard status --project <uuid-or-slug>"
			if preview.Source == "repository-ambiguous" {
				hint = "Select the Repository with ctxferry onboard status --project <uuid-or-slug> --repository <uuid>"
			}
			_, err := fmt.Fprintln(w, hint)
			return err
		}
		command := "ctxferry onboard bind --preview " + preview.ID
		project := "New Project: " + preview.NewSlug
		if preview.Project != nil {
			project = fmt.Sprintf("Project: %s (%s)", preview.Project.Slug, preview.Project.ID)
			if preview.Source == "explicit" {
				command += " --project " + preview.Project.ID
			}
		}
		if preview.RepositoryID != "" {
			project += "\nRepository: " + preview.RepositoryID
			command += " --repository " + preview.RepositoryID
		}
		_, err := fmt.Fprintln(w, project+"\nConfirm with "+command)
		return err
	case "notes":
		_, err := fmt.Fprintf(w, "Bound %s to Project %s\nImported notes: %d\nUse ctxferry onboard scan or import to select notes, then ctxferry onboard finish.\n",
			state.Directory, state.ProjectID, len(state.DocumentIDs))
		return err
	case "complete":
		if _, err := fmt.Fprintln(w, "Onboarding complete for", state.Directory); err != nil {
			return err
		}
		if state.Doctor != nil {
			return renderDoctor(w, *state.Doctor)
		}
	}
	return nil
}
