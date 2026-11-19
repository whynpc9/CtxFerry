package cli

import (
	"context"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func artifactCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, selector *string) *cobra.Command {
	group := &cobra.Command{Use: "artifact", Short: "Register, share and retrieve run artifacts"}
	run := func(cmd *cobra.Command, args []string, fn func(app.App, string) (any, error)) error {
		return invoke(func(a app.App) (any, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return fn(a, cwd)
		})(cmd, args)
	}
	group.RunE = func(cmd *cobra.Command, _ []string) error {
		return invoke(func(app.App) (any, error) { return map[string]any{"help": cmd.UsageString()}, nil })(cmd, nil)
	}

	var spec app.ArtifactRegistration
	var p app.RunProvenance
	add := &cobra.Command{Use: "add <file>", Short: "Register one file for a new or existing run", Args: cobra.ExactArgs(1)}
	flags := add.Flags()
	flags.StringVar(&spec.RunID, "run", "", "Existing run UUID returned by artifact add")
	flags.StringVar(&spec.Type, "type", "", "Payload media type, e.g. application/json")
	flags.StringVar(&spec.Sensitivity, "sensitivity", "", "Sensitivity class: public, internal or sensitive")
	flags.StringVar(&p.SourceRevision, "source-revision", "", "Source revision for a new run")
	flags.BoolVar(&p.Dirty, "dirty", false, "Whether the source was dirty (supply --dirty=true or --dirty=false)")
	flags.StringVar(&p.DiffID, "diff-id", "", "Identifier of an existing diff for dirty source")
	flags.StringVar(&p.Profile, "profile", "", "Profile used for the run, or none")
	flags.StringArrayVar(&p.ToolVersions, "tool", nil, "Relevant tool and version (repeatable)")
	flags.StringArrayVar(&p.Inputs, "input", nil, "Input identity such as a dataset or model revision (repeatable)")
	flags.StringArrayVar(&p.Command, "command", nil, "One command element in argv order (repeatable)")
	flags.IntVar(&p.ExitCode, "exit-code", 0, "Run exit code")
	flags.StringVar(&p.Summary, "summary", "", "Run summary")
	flags.StringArrayVar(&p.Completed, "completed", nil, "Completed scope (repeatable)")
	flags.StringArrayVar(&p.Uncompleted, "uncompleted", nil, "Uncompleted scope (repeatable)")
	add.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			if flags.Changed("run") {
				if spec.RunID == "" {
					return nil, app.Fail("usage", "run must not be empty")
				}
				for _, name := range []string{"source-revision", "dirty", "diff-id", "profile", "tool", "input", "command", "exit-code", "summary", "completed", "uncompleted"} {
					if flags.Changed(name) {
						return nil, app.Fail("usage", "cannot change the provenance of an existing run")
					}
				}
			} else {
				if !flags.Changed("dirty") || !flags.Changed("exit-code") {
					return nil, app.Fail("usage", "a new run requires explicit --dirty and --exit-code values")
				}
				spec.Provenance = &p
			}
			spec.CacheLimit = app.DefaultArtifactCacheLimit
			if raw, ok := os.LookupEnv("CTXFERRY_ARTIFACT_CACHE_BYTES"); ok {
				limit, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || limit <= 0 {
					return nil, app.Fail("usage", "CTXFERRY_ARTIFACT_CACHE_BYTES must be a positive byte count")
				}
				spec.CacheLimit = limit
			}
			return a.AddArtifact(ctx, *selector, cwd, args[0], spec)
		})
	}

	var runID string
	list := &cobra.Command{Use: "list", Short: "List artifacts for the Project and optionally a run", Args: cobra.NoArgs}
	list.Flags().StringVar(&runID, "run", "", "Filter by run UUID")
	list.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			if cmd.Flags().Changed("run") && runID == "" {
				return nil, app.Fail("usage", "run must not be empty")
			}
			return a.ListArtifacts(ctx, *selector, cwd, runID)
		})
	}

	inspect := &cobra.Command{Use: "inspect <artifact-uuid>", Short: "Show the artifact manifest", Args: cobra.ExactArgs(1)}
	inspect.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.InspectArtifact(ctx, *selector, cwd, args[0])
		})
	}

	var output string
	fetch := &cobra.Command{Use: "fetch <artifact-uuid>", Short: "Verify and copy the payload to a new file", Args: cobra.ExactArgs(1)}
	fetch.Flags().StringVarP(&output, "output", "o", "", "Destination file (required)")
	fetch.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.FetchArtifact(ctx, *selector, cwd, args[0], output)
		})
	}
	var mode, approval string
	share := &cobra.Command{Use: "share <artifact-uuid>", Short: "Preview or allow-list one registered artifact for sync", Args: cobra.ExactArgs(1)}
	share.Flags().StringVar(&mode, "mode", "automatic", "Publication mode: automatic, explicit or metadata")
	share.Flags().StringVar(&approval, "preview", "", "Approve the inspected artifact share preview ID")
	share.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			if cmd.Flags().Changed("preview") && approval == "" {
				return nil, app.Fail("usage", "preview ID must not be empty")
			}
			return a.ShareArtifact(ctx, *selector, cwd, args[0], mode, approval)
		})
	}
	group.AddCommand(add, list, inspect, fetch, share)
	return group
}
