package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
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
	summaryFile := ""
	put := &cobra.Command{Use: "put [document-uuid]", Short: "Create or replace a document; title, purpose and source are agent-visible", Args: cobra.MaximumNArgs(1)}
	put.Flags().StringVar(&input.IfRevision, "if-revision", "", "Required current revision for replacement")
	put.Flags().StringVar(&input.Title, "title", "", "Agent-visible title")
	put.Flags().StringVar(&input.Purpose, "purpose", "", "Purpose; project-summary designates the Project summary")
	put.Flags().StringVar(&input.Source, "source", "", "Agent-visible source description (never inferred from file path)")
	put.Flags().StringVar(&input.Summary, "summary", "", "Explicit agent-approved summary")
	put.Flags().StringVar(&summaryFile, "summary-file", "", "UTF-8 summary file (alternative to --summary)")
	put.Flags().StringVar(&input.Storage, "storage", "", "Required: local-only or shared")
	put.Flags().StringVar(&input.Disclosure, "disclosure", "", "Required: full, summary, reference or local-tool")
	put.Flags().StringVar(&input.ValidUntil, "valid-until", "", "Optional freshness expiry, RFC3339")
	put.Flags().StringVar(&file, "body-file", "", "Required UTF-8 Markdown file; - reads stdin")
	put.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			if !cmd.Flags().Changed("body-file") || file == "" {
				return nil, app.Fail("usage", "--body-file is required")
			}
			var reader io.Reader = cmd.InOrStdin()
			if file != "-" {
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
	show := &cobra.Command{Use: "show", Short: "Project summary, execution environment and document index (8 KiB)", Args: cobra.NoArgs}
	show.Flags().StringVar(&cursor, "cursor", "", "Continue the document index after this UUID")
	show.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.QueryContext(ctx, *selector, cwd, "show", "", cursor)
		})
	}
	get := &cobra.Command{Use: "get <document-uuid>", Short: "Retrieve a document through its disclosure policy", Args: cobra.ExactArgs(1)}
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
	group.AddCommand(put, show, get, search)
	return group
}

// Budget the actual envelope/text representation, never serialized fragments.
func budgetContext(result any, format string) (any, error) {
	compact := false
	r, ok := result.(app.ContextResult)
	if !ok || r.Environment == nil {
		return result, nil
	}
	for {
		var b []byte
		var e error
		if format == "json" {
			b, e = json.Marshal(envelope{1, true, r, nil})
			b = append(b, '\n')
		} else {
			var buf bytes.Buffer
			e = render(&buf, r)
			b = buf.Bytes()
		}
		if e != nil {
			return nil, e
		}
		if len(b) <= 8192 {
			return r, nil
		}
		if len(r.Documents) <= 1 {
			if compact {
				return nil, app.Fail("size-limit", "required context metadata exceeds 8192-byte show budget")
			}
			// Keep a retrievable entry on every nonempty page. Escaping can
			// expand a byte of metadata sixfold, including the repeated summary.
			compact = true
			if r.Summary != nil {
				s := compactText(*r.Summary)
				r.Summary = &s
			}
			if len(r.Documents) == 1 {
				d := &r.Documents[0]
				d.Title = compactText(d.Title)
				d.Purpose = compactText(d.Purpose)
				d.Source = compactText(d.Source)
				if d.Summary != nil {
					s := compactText(*d.Summary)
					d.Summary = &s
				}
			}
			continue
		}
		r.Documents = r.Documents[:len(r.Documents)-1]
		r.Truncated = true
		r.NextCursor = r.Documents[len(r.Documents)-1].ID
	}
}

func compactText(s string) string {
	if len(s) <= 64 {
		return s
	}
	s = s[:64]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}
