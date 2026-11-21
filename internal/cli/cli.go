// Package cli handles arguments and presentation only.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/platform"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

type envelope struct {
	SchemaVersion int        `json:"schemaVersion"`
	OK            bool       `json:"ok"`
	Data          any        `json:"data"`
	Error         *app.Error `json:"error"`
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return RunWithProviders(ctx, args, stdout, stderr, platform.ProviderServices())
}

// Services are the device-local seams behind the CLI.
type Services struct {
	Providers provider.Services
	// Trust is the pre-execution decision for credentialed runs. Nil uses the
	// device-local trust records in the data directory.
	Trust app.ExecutionTrust
	// parseOnly checks arguments and flags without running the command.
	parseOnly bool
}

// Validate checks args against the CLI grammar, including flag names, argument
// counts and usage rules, without touching a data directory. Front ends that
// print commands for people to copy validate them with it, so a renamed flag
// fails their tests instead of a user's terminal.
func Validate(args []string) error {
	var stderr strings.Builder
	if RunWithServices(context.Background(), args, io.Discard, &stderr, Services{parseOnly: true}) != 0 {
		return errors.New(strings.TrimSpace(stderr.String()))
	}
	return nil
}

func RunWithProviders(ctx context.Context, args []string, stdout, stderr io.Writer, providers provider.Services) int {
	return RunWithServices(ctx, args, stdout, stderr, Services{Providers: providers})
}

func RunWithServices(ctx context.Context, args []string, stdout, stderr io.Writer, services Services) int {
	format := "text"
	dataDir := ""
	projectSelector := ""
	var result any
	ran := false
	root := &cobra.Command{Use: "ctxferry", Short: "Local-first development context", SilenceUsage: true, SilenceErrors: true}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.PersistentFlags().StringVar(&projectSelector, "project", "", "Explicit Project UUID or slug")
	root.PersistentFlags().StringVar(&format, "format", "text", "Output format: text, json for queries, or jsonl for run and exec")
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "Application data root (or CTXFERRY_DATA_DIR)")
	invoke := func(fn func(app.App) (any, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			ran = true
			if cmd.Flags().Changed("project") && projectSelector == "" {
				return app.Fail("usage", "project must not be empty")
			}
			if format != "text" && format != "json" && !((cmd.Name() == "run" || cmd.Name() == "exec") && format == "jsonl") {
				return app.Fail("usage", "format must be text or json")
			}
			if services.parseOnly {
				return nil
			}
			dir, e := platform.DataDir(dataDir, cmd.Flags().Changed("data-dir"))
			if e != nil {
				if cmd.Name() != "doctor" || cmd.Flags().Changed("data-dir") {
					return app.Fail("usage", e.Error())
				}
				dir = "" // Doctor can still report checks unrelated to the catalog.
			}
			trust := services.Trust
			if trust == nil {
				trust = app.LocalTrust(dir)
			}
			result, e = fn(app.App{DataDir: dir, Providers: services.Providers, Trust: trust})
			return e
		}
	}
	initCmd := &cobra.Command{Use: "init", Short: "Initialize a local workspace without login", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.Init(ctx) })}
	project := &cobra.Command{Use: "project", Short: "Manage local Projects"}
	slug := ""
	repository := ""
	remotes := []string{}
	bind := &cobra.Command{Use: "bind <path>", Short: "Bind a directory to a Project", Args: cobra.ExactArgs(1)}
	bind.Flags().StringVar(&repository, "repository", "", "Existing Repository UUID")
	bind.Flags().StringArrayVar(&remotes, "remote", nil, "Explicitly accept a GitHub remote alias")
	bind.Flags().StringVar(&slug, "slug", "", "Explicit unique Project slug")
	bind.RunE = func(cmd *cobra.Command, argv []string) error {
		if cmd.Flags().Changed("slug") && slug == "" {
			return app.Fail("usage", "slug must not be empty")
		}
		return invoke(func(a app.App) (any, error) {
			return a.BindProjectWith(ctx, argv[0], slug, projectSelector, repository, remotes)
		})(cmd, argv)
	}
	var allProjects bool
	list := &cobra.Command{Use: "list", Short: "List local Projects", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.ListProjectsAll(ctx, allProjects) })}
	list.Flags().BoolVar(&allProjects, "all", false, "Include unsubscribed Projects")
	inspect := &cobra.Command{Use: "inspect [uuid-or-slug]", Short: "Inspect a Project or the current directory", Args: cobra.MaximumNArgs(1)}
	inspect.RunE = func(cmd *cobra.Command, argv []string) error {
		return invoke(func(a app.App) (any, error) {
			selector := projectSelector
			if len(argv) > 0 {
				if projectSelector != "" && projectSelector != argv[0] {
					return nil, app.Fail("usage", "conflicting Project selectors")
				}
				selector = argv[0]
			}
			cwd, e := os.Getwd()
			if e != nil {
				return nil, e
			}
			return a.InspectProject(ctx, selector, cwd)
		})(cmd, argv)
	}
	remote := &cobra.Command{Use: "remote", Short: "Manage explicitly accepted Repository aliases"}
	addRemote := &cobra.Command{Use: "add <repository-uuid> <url>", Args: cobra.ExactArgs(2)}
	addRemote.RunE = func(cmd *cobra.Command, argv []string) error {
		return invoke(func(a app.App) (any, error) { return a.AddRemote(ctx, argv[0], argv[1]) })(cmd, argv)
	}
	remote.AddCommand(addRemote)
	for _, enabled := range []bool{true, false} {
		name := "unsubscribe"
		if enabled {
			name = "subscribe"
		}
		command := &cobra.Command{Use: name + " <project>", Args: cobra.ExactArgs(1)}
		command.RunE = func(cmd *cobra.Command, args []string) error {
			return invoke(func(a app.App) (any, error) { return a.Subscribe(ctx, args[0], enabled) })(cmd, args)
		}
		project.AddCommand(command)
	}
	project.AddCommand(bind, list, inspect, remote)
	config, profile := configurationCommands(ctx, invoke, &projectSelector)
	secrets, providerCommands := secretCommands(ctx, invoke, &projectSelector)
	var taskExit int
	taskRan := false
	onExit := func(code int) { taskExit, taskRan = code, true }
	runTask := runCommand(ctx, invoke, &projectSelector, &format, onExit)
	execTask := execCommand(ctx, invoke, &projectSelector, &format, onExit)
	root.AddCommand(doctorCommand(ctx, invoke, &projectSelector), secrets, providerCommands, runTask, execTask, taskCommand(ctx, invoke, &projectSelector), initCmd, project, config, profile, contextCommand(ctx, invoke, &projectSelector), resourceCommand(ctx, invoke, &projectSelector), artifactCommand(ctx, invoke, &projectSelector), syncCommand(ctx, invoke), joinCommand(ctx, invoke), workspaceCommand(ctx, invoke))
	root.AddCommand(onboardingCommand(ctx, invoke, &projectSelector))

	// Detect format before parsing so usage errors also obey the JSON contract.
	requestedFormat := ""
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if value, ok := strings.CutPrefix(arg, "--format="); ok {
			requestedFormat = value
		}
		if arg == "--format" && i+1 < len(args) {
			requestedFormat = args[i+1]
		}
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		ran = true
		help := cmd.UsageString()
		if cmd.Long != "" {
			help = cmd.Long + "\n\n" + help
		}
		result = map[string]any{"help": help}
	})
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		ran = true
		result = map[string]any{"help": cmd.UsageString()}
		return nil
	}
	project.RunE = root.RunE
	root.AddCommand(skillCommand(ctx, invoke))
	root.SetContext(ctx)
	command, err := root.ExecuteC()
	if taskRan {
		return taskExit
	}
	if err == nil {
		result, err = budgetContext(result, format)
	}
	code := 0
	var failure *app.Error
	if err != nil {
		failure = app.Classify(err)
		if failure.Code == "store-error" && !ran {
			failure = &app.Error{Code: "usage", Message: err.Error()}
		}
		code = 1
		if failure.Code == "usage" {
			code = 2
		}
		executing := command == runTask || command == execTask
		if executing {
			f := *failure
			f.Origin = "control-plane"
			failure = &f
			if requestedFormat == "jsonl" || format == "jsonl" {
				truncated := false
				var executionError *streamFailure
				if errors.As(err, &executionError) {
					truncated = executionError.truncated
				}
				if len(f.Message) > 1024 {
					f.Message = app.CompactText(f.Message)
				}
				event := taskEvent{Type: "result", Origin: f.Origin, Truncated: &truncated, Error: &f}
				if wire, _ := json.Marshal(event); len(wire)+1 > taskEventLimit {
					f.Details = nil
				}
				stream := taskStream{out: stdout}
				stream.emit(event)
				return code
			}
		}
		if executing && requestedFormat != "json" && format != "json" {
			fmt.Fprintf(stderr, "%s (%s): %s\n", failure.Code, failure.Origin, failure.Message)
			if paths, ok := failure.Details.([]string); ok {
				for _, path := range paths {
					fmt.Fprintln(stderr, path)
				}
			}
		} else {
			fmt.Fprintln(stderr, failure.Message)
		}
	}
	if requestedFormat == "json" || format == "json" {
		if err != nil {
			result = nil
		}
		b, e := json.Marshal(envelope{1, err == nil, result, failure})
		if e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
		if _, e = fmt.Fprintln(stdout, string(b)); e != nil {
			return 1
		}
	} else if err == nil {
		if e := render(stdout, result); e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
	}
	return code
}
func render(w io.Writer, result any) error {
	var err error
	switch r := result.(type) {
	case app.SkillPackage:
		_, err = fmt.Fprintf(w, "%s (%s)\nSource: %s\nLicense: %s\n", r.Name, r.Digest, r.SourceURL, r.License)
	case app.AgentEntryPlan:
		return renderAgentEntry(w, r)
	case app.SkillPlan:
		return renderSkillPlan(w, r)
	case app.SkillChangePlan:
		return renderSkillChange(w, r)
	case app.SkillGroup:
		_, err = fmt.Fprintf(w, "%s: %s\n", r.Name, strings.Join(r.Skills, ", "))
	case app.SkillGroupPlan:
		if _, err := fmt.Fprintf(w, "%s (%d Skills)\n", r.Group.Name, len(r.Plans)); err != nil {
			return err
		}
		for _, plan := range r.Plans {
			if err := renderSkillPlan(w, plan); err != nil {
				return err
			}
		}
	case app.DoctorReport:
		return renderDoctor(w, r)
	case app.InitResult:
		_, err = fmt.Fprintf(w, "Workspace %s\nDevice %s (%s/%s)\n", r.Workspace.ID, r.Device.ID, r.Device.OS, r.Device.Arch)
	case app.BindResult:
		_, err = fmt.Fprintf(w, "%s (%s)\nCheckout: %s\n", r.Project.Slug, r.Project.ID, r.Binding.Path)
		if err == nil && r.Binding.RepositoryID != "" {
			_, err = fmt.Fprintf(w, "Repository: %s\nMembership: %s\n", r.Binding.RepositoryID, r.Binding.MembershipID)
		}
	case app.ListResult:
		if len(r.Projects) == 0 {
			_, err = fmt.Fprintln(w, "No Projects bound.")
		}
		for _, p := range r.Projects {
			if _, err = fmt.Fprintf(w, "%s\t%s\n", p.Slug, p.ID); err != nil {
				return err
			}
		}
	case app.InspectResult:
		_, err = fmt.Fprintf(w, "%s (%s)\n", r.Project.Slug, r.Project.ID)
		if err == nil && r.Resolution != nil {
			_, err = fmt.Fprintf(w, "Source: %s\nDirectory: %s\nCurrent checkout: %s\nWorktree Git directory: %s\nCommon directory: %s\nGit: %s\nDevice: %s (%s/%s)\n", r.Resolution.Source, r.Resolution.Directory, r.Resolution.Git.CheckoutRoot, r.Resolution.Git.GitDir, r.Resolution.Git.CommonDir, r.Resolution.Git.Status, r.Resolution.Device.ID, r.Resolution.Device.OS, r.Resolution.Device.Arch)
		}
		if err != nil {
			return err
		}
		for _, b := range r.Bindings {
			if _, err = fmt.Fprintf(w, "Checkout: %s\nRepository: %s\nMembership: %s\n", b.Path, b.RepositoryID, b.MembershipID); err != nil {
				return err
			}
		}
	case app.ConflictDetail:
		b, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return e
		}
		_, err = fmt.Fprintln(w, string(b))
	case platform.SyncService:
		_, err = fmt.Fprintln(w, r.Message)
	case app.SyncResult, app.WorkspaceBackupResult, app.WorkspaceRestoreResult:
		b, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return e
		}
		_, err = fmt.Fprintln(w, string(b))
	case app.OnboardingState:
		return renderOnboarding(w, r)
	case app.EnvScan, app.EnvImportPlan, app.EnvImportReceipt, app.ArtifactManifest, app.ArtifactList, app.ArtifactFetch, app.ArtifactSharePreview, app.NoteScan, app.NoteImportReceipt, app.ContextResult, app.ConfigurationExplanation, app.ConfigurationReceipt, app.ProfileSelection, app.Resource, app.ResourceList, app.ResourceDetail, app.Observation, app.Task, app.TaskList, app.ResourceMapping, app.SecretList, app.SecretStatus, app.SecretBindResult, app.ProviderLoginResult, app.ExecutionReview:
		b, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return e
		}
		_, err = fmt.Fprintln(w, string(b))
	case map[string]any:
		if help, ok := r["help"]; ok {
			_, err = fmt.Fprint(w, help)
		}
	case map[string]string:
		_, err = fmt.Fprintf(w, "Repository: %s\nRemote alias: %s\n", r["repositoryId"], r["remote"])
	}
	return err
}
