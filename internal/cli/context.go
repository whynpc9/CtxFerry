package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func contextCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, selector *string) *cobra.Command {
	group := &cobra.Command{Use: "context", Short: "Read and explicitly write project context"}
	run := func(cmd *cobra.Command, args []string, fn func(app.App, string) (any, error)) error {
		return invoke(func(a app.App) (any, error) {
			cwd, e := os.Getwd()
			if e != nil {
				return nil, e
			}
			return fn(a, cwd)
		})(cmd, args)
	}
	group.RunE = func(cmd *cobra.Command, _ []string) error {
		return invoke(func(app.App) (any, error) { return map[string]any{"help": cmd.UsageString()}, nil })(cmd, nil)
	}
	var input app.PutContext
	file := ""
	body := ""
	summaryFile := ""
	put := &cobra.Command{Use: "put [document-uuid]", Short: "Create or replace a document; title, purpose and source are agent-visible", Long: putHelp, Args: cobra.MaximumNArgs(1)}
	put.Flags().StringVar(&input.IfRevision, "if-revision", "", "Required current revision for replacement")
	put.Flags().StringVar(&input.Title, "title", "", "Agent-visible title")
	put.Flags().StringVar(&input.Purpose, "purpose", "", "Purpose; project-summary designates the Project summary")
	put.Flags().StringVar(&input.Source, "source", "", "Agent-visible source description (never inferred from file path)")
	put.Flags().StringVar(&input.Summary, "summary", "", "Explicit agent-approved summary")
	put.Flags().StringVar(&summaryFile, "summary-file", "", "UTF-8 summary file (alternative to --summary)")
	put.Flags().StringVar(&input.Storage, "storage", "", "local-only or shared (new documents default to local-only)")
	put.Flags().StringVar(&input.Disclosure, "disclosure", "", "full, summary, reference or local-tool (new documents default to full)")
	put.Flags().StringVar(&input.ValidUntil, "valid-until", "", "Optional freshness expiry, RFC3339")
	put.Flags().StringVar(&file, "body-file", "", "UTF-8 Markdown file; - reads stdin")
	put.Flags().StringVar(&body, "body", "", "Inline UTF-8 Markdown body (alternative to --body-file)")
	put.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			inline := cmd.Flags().Changed("body")
			if inline == (cmd.Flags().Changed("body-file") && file != "") {
				return nil, app.Fail("usage", "pass one of --body or --body-file")
			}
			if len(args) == 0 {
				for _, d := range []struct {
					value          *string
					flag, fallback string
				}{{&input.Purpose, "purpose", "note"}, {&input.Source, "source", "ctxferry CLI"}, {&input.Storage, "storage", "local-only"}, {&input.Disclosure, "disclosure", "full"}} {
					if !cmd.Flags().Changed(d.flag) {
						*d.value = d.fallback
					}
				}
			}
			var reader io.Reader = cmd.InOrStdin()
			if inline {
				reader = strings.NewReader(body)
			} else if file != "-" {
				f, e := os.Open(file)
				if e != nil {
					return nil, app.Fail("invalid-document", "cannot open body file")
				}
				defer f.Close()
				reader = f
			}
			b, e := io.ReadAll(io.LimitReader(reader, app.BodyLimit+1))
			if e != nil {
				return nil, app.Fail("invalid-document", "cannot read body")
			}
			if len(b) > app.BodyLimit {
				return nil, app.Fail("size-limit", "Markdown body exceeds 1048576 bytes")
			}
			if !utf8.Valid(b) {
				return nil, app.Fail("invalid-document", "body must be UTF-8")
			}
			if cmd.Flags().Changed("summary-file") {
				if cmd.Flags().Changed("summary") {
					return nil, app.Fail("usage", "use --summary or --summary-file")
				}
				f, e := os.Open(summaryFile)
				if e != nil {
					return nil, app.Fail("invalid-document", "cannot open summary file")
				}
				defer f.Close()
				summary, e := io.ReadAll(io.LimitReader(f, app.MetadataLimit+1))
				if e != nil {
					return nil, app.Fail("invalid-document", "cannot read summary")
				}
				if len(summary) > app.MetadataLimit {
					return nil, app.Fail("size-limit", "structured context record exceeds 262144 bytes")
				}
				input.Summary = string(summary)
			}
			input.Body = string(b)
			if len(args) > 0 {
				input.ID = args[0]
			}
			return a.PutContext(ctx, *selector, cwd, input)
		})
	}
	cursor := ""
	show := &cobra.Command{Use: "show", Short: "Project summary, execution environment and document, Resource and Task index (8 KiB)", Args: cobra.NoArgs}
	show.Flags().StringVar(&cursor, "cursor", "", "Continue the index after this UUID")
	show.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.QueryContext(ctx, *selector, cwd, "show", "", cursor)
		})
	}
	get := &cobra.Command{Use: "get <document-uuid-or-title>", Short: "Retrieve a document through its disclosure policy", Args: cobra.ExactArgs(1)}
	get.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.QueryContext(ctx, *selector, cwd, "get", args[0], "")
		})
	}
	search := &cobra.Command{Use: "search <keywords>", Short: "Literal keyword search over agent-visible content (up to 100 results)", Args: cobra.ExactArgs(1)}
	search.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.QueryContext(ctx, *selector, cwd, "search", args[0], "")
		})
	}
	for _, cmd := range []*cobra.Command{show, get, search} {
		var wait bool
		cmd.Flags().BoolVar(&wait, "sync", false, "Wait for a successful sync before querying")
		original := cmd.RunE
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if wait {
				if err := invoke(func(a app.App) (any, error) { return a.Sync(ctx) })(cmd, args); err != nil {
					return err
				}
			}
			return original(cmd, args)
		}
	}
	for _, action := range []string{"delete", "restore"} {
		var revision, commit string
		cmd := &cobra.Command{Use: action + " <document-uuid>", Args: cobra.ExactArgs(1)}
		cmd.Flags().StringVar(&revision, "if-revision", "", "Required current revision")
		if action == "restore" {
			cmd.Flags().StringVar(&commit, "from-commit", "", "Required ancestor commit SHA")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return invoke(func(a app.App) (any, error) {
				if action == "restore" && commit == "" {
					return nil, app.Fail("usage", "restore requires --from-commit")
				}
				return a.ChangeDeletion(ctx, args[0], revision, commit)
			})(cmd, args)
		}
		group.AddCommand(cmd)
	}
	var profiles []string
	explain := &cobra.Command{Use: "explain", Short: "Show effective ordinary configuration and each field's origin", Args: cobra.NoArgs}
	explain.Flags().StringArrayVar(&profiles, "profile", nil, "Explicit Profile for this invocation")
	explain.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			name, err := explicitProfile(profiles)
			if err != nil {
				return nil, err
			}
			return a.ExplainConfiguration(ctx, *selector, cwd, name)
		})
	}
	group.AddCommand(put, show, get, search, explain)
	return group
}

const putHelp = `Create or replace a context document.

A new document needs only --title and a body (--body, --body-file or
--body-file - for stdin). The other fields default to --purpose note,
--source "ctxferry CLI", --storage local-only and --disclosure full. Pass
--storage shared to synchronize the document to other devices.

Replacing a document (put <document-uuid> --if-revision <revision>) replaces
every field, so pass the purpose, source, storage and disclosure explicitly.`

// Budget the actual envelope/text representation, never serialized fragments.
func budgetContext(result any, format string) (any, error) {
	r, ok := result.(app.ContextResult)
	if !ok || r.Environment == nil {
		return result, nil
	}
	fitted, err := app.FitContextPage(r, app.ContextShowBudget, func(r app.ContextResult) (int, error) {
		if format == "json" {
			b, err := json.Marshal(envelope{1, true, r, nil})
			return len(b) + 1, err
		}
		var buf bytes.Buffer
		err := render(&buf, r)
		return buf.Len(), err
	})
	if err != nil {
		return nil, err
	}
	return fitted, nil
}
