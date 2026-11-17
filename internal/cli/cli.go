// Package cli handles arguments and presentation only.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/platform"
)

type envelope struct {
	SchemaVersion int        `json:"schemaVersion"`
	OK            bool       `json:"ok"`
	Data          any        `json:"data"`
	Error         *app.Error `json:"error"`
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	format := "text"
	dataDir := ""
	var result any
	ran := false
	root := &cobra.Command{Use: "ctxferry", Short: "Local-first development context", SilenceUsage: true, SilenceErrors: true}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.PersistentFlags().StringVar(&format, "format", "text", "Output format: text or json")
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "Application data root (or CTXFERRY_DATA_DIR)")
	invoke := func(fn func(app.App) (any, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			ran = true
			if format != "text" && format != "json" {
				return app.Fail("usage", "format must be text or json")
			}
			dir, e := platform.DataDir(dataDir, cmd.Flags().Changed("data-dir"))
			if e != nil {
				return app.Fail("usage", e.Error())
			}
			result, e = fn(app.App{DataDir: dir})
			return e
		}
	}
	initCmd := &cobra.Command{Use: "init", Short: "Initialize a local workspace without login", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.Init(ctx) })}
	project := &cobra.Command{Use: "project", Short: "Manage local Projects"}
	slug := ""
	bind := &cobra.Command{Use: "bind <path>", Short: "Bind a directory to a Project", Args: cobra.ExactArgs(1)}
	bind.Flags().StringVar(&slug, "slug", "", "Explicit unique Project slug")
	bind.RunE = func(cmd *cobra.Command, argv []string) error {
		if cmd.Flags().Changed("slug") && slug == "" {
			return app.Fail("usage", "slug must not be empty")
		}
		return invoke(func(a app.App) (any, error) { return a.BindProject(ctx, argv[0], slug) })(cmd, argv)
	}
	list := &cobra.Command{Use: "list", Short: "List local Projects", Args: cobra.NoArgs, RunE: invoke(func(a app.App) (any, error) { return a.ListProjects(ctx) })}
	inspect := &cobra.Command{Use: "inspect [uuid-or-slug]", Short: "Inspect a Project or the current directory", Args: cobra.MaximumNArgs(1)}
	inspect.RunE = func(cmd *cobra.Command, argv []string) error {
		return invoke(func(a app.App) (any, error) {
			selector := ""
			if len(argv) > 0 {
				selector = argv[0]
			}
			cwd, e := os.Getwd()
			if e != nil {
				return nil, e
			}
			return a.InspectProject(ctx, selector, cwd)
		})(cmd, argv)
	}
	project.AddCommand(bind, list, inspect)
	root.AddCommand(initCmd, project)
	// Detect format before parsing so usage errors also obey the JSON contract.
	requestedJSON := false
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--format=json" {
			requestedJSON = true
		}
		if arg == "--format=text" {
			requestedJSON = false
		}
		if arg == "--format" && i+1 < len(args) {
			requestedJSON = args[i+1] == "json"
		}
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) { ran = true; result = map[string]any{"help": cmd.UsageString()} })
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		ran = true
		result = map[string]any{"help": cmd.UsageString()}
		return nil
	}
	project.RunE = root.RunE
	err := root.ExecuteContext(ctx)
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
		fmt.Fprintln(stderr, failure.Message)
	}
	if requestedJSON || format == "json" {
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
	case app.InitResult:
		_, err = fmt.Fprintf(w, "Workspace %s\nDevice %s (%s/%s)\n", r.Workspace.ID, r.Device.ID, r.Device.OS, r.Device.Arch)
	case app.BindResult:
		_, err = fmt.Fprintf(w, "%s (%s)\nCheckout: %s\n", r.Project.Slug, r.Project.ID, r.Binding.Path)
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
		if err != nil {
			return err
		}
		for _, b := range r.Bindings {
			if _, err = fmt.Fprintf(w, "Checkout: %s\n", b.Path); err != nil {
				return err
			}
		}
	case map[string]any:
		_, err = fmt.Fprint(w, r["help"])
	}
	return err
}
