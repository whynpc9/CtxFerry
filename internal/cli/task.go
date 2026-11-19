package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func taskCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project *string) *cobra.Command {
	group := &cobra.Command{Use: "task", Short: "Register and discover Project Tasks"}
	var spec app.TaskSpec
	var resources, credentials []string
	var storage string
	add := &cobra.Command{Use: "add <name>", Short: "Register a Task and its execution definition", Args: cobra.ExactArgs(1)}
	add.Flags().StringVar(&spec.Purpose, "purpose", "", "Task purpose")
	add.Flags().StringArrayVar(&spec.Inputs, "input", []string{}, "Describe a Task input (repeatable)")
	add.Flags().StringArrayVar(&resources, "resource-slot", nil, "JSON object with name, kind and stage (repeatable)")
	add.Flags().StringArrayVar(&credentials, "credential-requirement", nil, "JSON object with name, resourceSlot, purpose, type and env (repeatable)")
	add.Flags().StringVar(&spec.MembershipID, "membership", "", "Repository membership UUID")
	add.Flags().StringVar(&spec.WorkingDirectory, "working-directory", ".", "Directory relative to the membership root")
	add.Flags().StringVar(&spec.Program, "program", "", "Executable name on PATH or a slash-separated path relative to the working directory")
	add.Flags().StringArrayVar(&spec.Argv, "arg", []string{}, "One literal argument (repeatable, use --arg=VALUE for leading dashes)")
	add.Flags().StringArrayVar(&spec.Platforms, "platform", []string{}, "Supported OS or OS/architecture (repeatable)")
	add.Flags().StringVar(&storage, "storage", "", "Required storage policy (local-only or shared)")
	add.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			spec.Name = args[0]
			spec.ResourceSlots, err = slotFlags[app.TaskResourceSlot](resources, "--resource-slot")
			if err != nil {
				return nil, err
			}
			spec.CredentialRequirements, err = slotFlags[app.CredentialRequirement](credentials, "--credential-requirement")
			if err != nil {
				return nil, err
			}
			return a.AddTask(ctx, *project, cwd, storage, spec)
		})(cmd, args)
	}
	list := &cobra.Command{Use: "list", Short: "List Tasks and their requirements", Args: cobra.NoArgs}
	list.RunE = invoke(func(a app.App) (any, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return a.ListTasks(ctx, *project, cwd)
	})
	inspect := &cobra.Command{Use: "inspect <name-or-uuid>", Short: "Inspect a Task and its requirements", Args: cobra.ExactArgs(1)}
	inspect.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return a.InspectTask(ctx, *project, cwd, args[0])
		})(cmd, args)
	}
	group.AddCommand(add, list, inspect)
	return group
}

func slotFlags[T any](values []string, flag string) ([]T, error) {
	out := make([]T, 0, len(values))
	for _, value := range values {
		var slot T
		dec := json.NewDecoder(bytes.NewBufferString(value))
		dec.DisallowUnknownFields()
		if dec.Decode(&slot) != nil || dec.Decode(new(any)) != io.EOF {
			return nil, app.Fail("usage", flag+" requires one JSON object with the documented fields")
		}
		out = append(out, slot)
	}
	return out, nil
}

const runUsage = "use ctxferry run <task> [--checkout <root>] [--profile <name>] [--format text|jsonl]"

func runCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project, format *string, onExit func(int)) *cobra.Command {
	var profiles, checkouts []string
	cmd := &cobra.Command{Use: "run <task>", Short: "Run a registered credential-free Task with text or JSONL output",
		Args: func(_ *cobra.Command, args []string) error {
			if *format != "text" && *format != "jsonl" || len(args) != 1 {
				return app.Fail("usage", runUsage)
			}
			return nil
		}}
	cmd.Flags().StringArrayVar(&profiles, "profile", nil, "Explicit Profile for this invocation")
	cmd.Flags().StringArrayVar(&checkouts, "checkout", nil, "Explicit Git checkout root for the Task's membership")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			profile, err := explicitProfile(profiles)
			if err != nil {
				return nil, err
			}
			checkout := ""
			if len(checkouts) > 0 {
				if len(checkouts) != 1 || checkouts[0] == "" {
					return nil, app.Fail("usage", "specify one nonempty --checkout root")
				}
				checkout = checkouts[0]
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			plan, err := a.PrepareTask(ctx, *project, cwd, args[0], checkout, profile)
			if err != nil {
				return nil, err
			}
			code, err := executeTask(ctx, cmd, *format, plan)
			if err == nil {
				onExit(code)
			}
			return nil, err
		})(cmd, args)
	}
	return cmd
}

func executeTask(ctx context.Context, cmd *cobra.Command, format string, plan app.TaskExecution) (int, error) {
	out, diagnostics := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if format == "text" {
		return app.ExecuteTask(ctx, plan, cmd.InOrStdin(), out, diagnostics, func() {})
	}
	stream := taskStream{out: out}
	stream.mu.Lock()
	started := false
	code, err := app.ExecuteTask(ctx, plan, nil, taskOutput{&stream, "stdout"}, taskOutput{&stream, "stderr"}, func() {
		started = true
		stream.emit(taskEvent{Type: "start", TaskID: plan.Task.ID, Task: plan.Task.Name, Profile: plan.Profile})
		stream.mu.Unlock()
	})
	if !started {
		stream.mu.Unlock()
	}
	if err != nil {
		// Include truncation from any output that preceded the control-plane failure.
		return 0, &streamFailure{err, stream.truncated}
	}
	result := taskEvent{Type: "result", Origin: "child", ExitCode: &code, Truncated: &stream.truncated}
	stream.emit(result)
	if stream.err != nil {
		return code, app.Fail("output-failed", "could not write Task events")
	}
	return code, nil
}

type streamFailure struct {
	err       error
	truncated bool
}

func (e *streamFailure) Error() string { return e.err.Error() }
func (e *streamFailure) Unwrap() error { return e.err }

const (
	taskEventLimit  = 8 * 1024
	taskStreamLimit = 64 * 1024
	taskOutputChunk = 1024 // Even six-byte JSON escapes fit inside one event.
)

type taskEvent struct {
	Type      string     `json:"type"`
	TaskID    string     `json:"taskId,omitempty"`
	Task      string     `json:"task,omitempty"`
	Profile   string     `json:"profile,omitempty"`
	Data      string     `json:"data,omitempty"`
	Encoding  string     `json:"encoding,omitempty"`
	Origin    string     `json:"origin,omitempty"`
	ExitCode  *int       `json:"exitCode,omitempty"`
	Truncated *bool      `json:"truncated,omitempty"`
	Error     *app.Error `json:"error,omitempty"`
}

type taskStream struct {
	mu        sync.Mutex
	out       io.Writer
	bytes     int
	truncated bool
	err       error
}

func (s *taskStream) emit(event taskEvent) {
	if s.err != nil {
		return
	}
	wire, err := json.Marshal(event)
	if err != nil {
		s.err = err
		return
	}
	wire = append(wire, '\n')
	if len(wire) > taskEventLimit {
		s.err = fmt.Errorf("Task event exceeds %d bytes", taskEventLimit)
		return
	}
	if event.Type == "stdout" || event.Type == "stderr" {
		if len(wire) > taskStreamLimit-taskEventLimit-s.bytes {
			s.truncated = true
			return
		}
	}
	s.bytes += len(wire)
	n, err := s.out.Write(wire)
	if err == nil && n != len(wire) {
		err = io.ErrShortWrite
	}
	s.err = err
}

type taskOutput struct {
	stream *taskStream
	kind   string
}

func (w taskOutput) Write(p []byte) (int, error) {
	s := w.stream
	s.mu.Lock()
	defer s.mu.Unlock()
	for rest := p; len(rest) != 0 && !s.truncated && s.err == nil; {
		n := min(len(rest), taskOutputChunk)
		if n < len(rest) {
			// Keep UTF-8 runes intact when splitting a valid chunk.
			for i := n; i > n-utf8.UTFMax; i-- {
				if utf8.RuneStart(rest[i]) {
					n = i
					break
				}
			}
		}
		chunk := rest[:n]
		event := taskEvent{Type: w.kind, Data: string(chunk)}
		if !utf8.Valid(chunk) {
			event.Encoding = "base64"
			event.Data = base64.StdEncoding.EncodeToString(chunk)
		}
		s.emit(event)
		rest = rest[n:]
	}
	return len(p), nil
}
