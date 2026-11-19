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
	add.Flags().BoolVar(&spec.AllowCustomCommand, "allow-custom-command", false, "Allow exec to run caller-chosen commands with this Task's credentials")
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
	group.AddCommand(add, list, inspect, taskUpdateCommand(ctx, invoke, project), trustCommand(ctx, invoke, project))
	return group
}

func taskUpdateCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project *string) *cobra.Command {
	var change app.TaskChange
	var purpose, membership, workingDirectory, program string
	var inputs, resources, credentials, argv, platforms []string
	var allowCustom bool
	cmd := &cobra.Command{Use: "update <name-or-uuid>", Short: "Replace fields of a Task at its current revision", Long: `Replace the given fields of a Task. Fields not given keep their values.

A change to the program, arguments, membership, working directory, credential
requirements or custom-command permission is an execution change: every device,
including this one, must enable the new definition with task trust preview and
task trust enable before running it with credentials. A change to the purpose
or inputs alone keeps the enabled state.`, Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&change.IfRevision, "if-revision", "", "Required current Task revision")
	cmd.Flags().StringVar(&purpose, "purpose", "", "Task purpose")
	cmd.Flags().StringArrayVar(&inputs, "input", nil, "Describe a Task input (repeatable, replaces all inputs)")
	cmd.Flags().StringArrayVar(&resources, "resource-slot", nil, "JSON Resource slot (repeatable, replaces all slots)")
	cmd.Flags().StringArrayVar(&credentials, "credential-requirement", nil, "JSON credential requirement (repeatable, replaces all requirements)")
	cmd.Flags().StringVar(&membership, "membership", "", "Repository membership UUID")
	cmd.Flags().StringVar(&workingDirectory, "working-directory", "", "Directory relative to the membership root")
	cmd.Flags().StringVar(&program, "program", "", "Executable name or slash-separated path relative to the working directory")
	cmd.Flags().StringArrayVar(&argv, "arg", nil, "One literal argument (repeatable, replaces all arguments)")
	cmd.Flags().Bool("no-args", false, "Replace the arguments with an empty array")
	cmd.Flags().StringArrayVar(&platforms, "platform", nil, "Supported OS or OS/architecture (repeatable, replaces all)")
	cmd.Flags().BoolVar(&allowCustom, "allow-custom-command", false, "Allow or forbid exec with caller-chosen commands")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			flags := cmd.Flags()
			if flags.Changed("purpose") {
				change.Purpose = &purpose
			}
			if flags.Changed("input") {
				change.Inputs = &inputs
			}
			if flags.Changed("membership") {
				change.MembershipID = &membership
			}
			if flags.Changed("working-directory") {
				change.WorkingDirectory = &workingDirectory
			}
			if flags.Changed("program") {
				change.Program = &program
			}
			noArgs, _ := flags.GetBool("no-args")
			if noArgs && flags.Changed("arg") {
				return nil, app.Fail("usage", "--arg and --no-args cannot be combined")
			}
			if noArgs {
				argv = []string{}
			}
			if noArgs || flags.Changed("arg") {
				change.Argv = &argv
			}
			if flags.Changed("platform") {
				change.Platforms = &platforms
			}
			if flags.Changed("allow-custom-command") {
				change.AllowCustomCommand = &allowCustom
			}
			if flags.Changed("resource-slot") {
				slots, err := slotFlags[app.TaskResourceSlot](resources, "--resource-slot")
				if err != nil {
					return nil, err
				}
				change.ResourceSlots = &slots
			}
			if flags.Changed("credential-requirement") {
				reqs, err := slotFlags[app.CredentialRequirement](credentials, "--credential-requirement")
				if err != nil {
					return nil, err
				}
				change.CredentialRequirements = &reqs
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return a.UpdateTask(ctx, *project, cwd, args[0], change)
		})(cmd, args)
	}
	return cmd
}

const trustHelp = `Review and enable a Task's credentialed execution definition on this device.

The definition covers the program and arguments, whether exec may run custom
commands, the Repository membership, the resolved local checkout and working
directory, and the selected Profile's Resource and SecretBinding mapping with
each binding's provider profile, locator and Resource purpose. A synchronized
change to any of these blocks credentialed run and exec here with
execution-definition-changed until it is previewed and enabled again. Editing
descriptive text does not. Trust records stay on this device and are never
synchronized.`

func trustCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project *string) *cobra.Command {
	group := &cobra.Command{Use: "trust", Short: "Review and enable credentialed execution on this device", Long: trustHelp}
	for _, action := range []string{"preview", "enable"} {
		var profiles, checkouts []string
		var preview string
		cmd := &cobra.Command{Use: action + " <task>", Args: cobra.ExactArgs(1), Long: trustHelp}
		cmd.Short = "Show the execution definition and what changed since it was enabled"
		if action == "enable" {
			cmd.Short = "Enable the previewed execution definition on this device"
			cmd.Flags().StringVar(&preview, "preview", "", "Preview token from task trust preview")
		}
		cmd.Flags().StringArrayVar(&profiles, "profile", nil, "Explicit Profile, as passed to run or exec")
		cmd.Flags().StringArrayVar(&checkouts, "checkout", nil, "Explicit Git checkout root, as passed to run or exec")
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return invoke(func(a app.App) (any, error) {
				profile, checkout, err := executionFlags(profiles, checkouts)
				if err != nil {
					return nil, err
				}
				cwd, err := os.Getwd()
				if err != nil {
					return nil, err
				}
				if action == "enable" {
					return a.EnableExecution(ctx, *project, cwd, args[0], checkout, profile, preview)
				}
				return a.PreviewExecution(ctx, *project, cwd, args[0], checkout, profile)
			})(cmd, args)
		}
		group.AddCommand(cmd)
	}
	return group
}

func executionFlags(profiles, checkouts []string) (string, string, error) {
	profile, err := explicitProfile(profiles)
	if err != nil {
		return "", "", err
	}
	checkout := ""
	if len(checkouts) > 0 {
		if len(checkouts) != 1 || checkouts[0] == "" {
			return "", "", app.Fail("usage", "specify one nonempty --checkout root")
		}
		checkout = checkouts[0]
	}
	return profile, checkout, nil
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

const runHelp = `Run a registered Task from its Repository membership in a local checkout.

A Task with credential requirements resolves only its own references, through
the selected Profile's explicit mappings and this device's provider login. The
child receives a minimal base environment and the Task's delivered variables.
Git and provider authentication material is not passed to it. Delivered values
are replaced with [REDACTED] in the output CtxFerry forwards.

Credentialed execution requires this device to have enabled the Task's current
execution definition (see ctxferry task trust). Once enabled, runs do not ask
again until a synchronized change alters what executes or which credential it
receives.

The target program can still read, print, encode or exfiltrate anything it is
given. A Task is not OS isolation: run credentialed Tasks only with programs
you trust.`

const runUsage = "use ctxferry run <task> [--checkout <root>] [--profile <name>] [--format text|jsonl]"

func runCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project, format *string, onExit func(int)) *cobra.Command {
	var profiles, checkouts []string
	cmd := &cobra.Command{Use: "run <task>", Short: "Run a registered Task with text or JSONL output", Long: runHelp,
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
			profile, checkout, err := executionFlags(profiles, checkouts)
			if err != nil {
				return nil, err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			plan, err := a.PrepareTask(ctx, *project, cwd, args[0], checkout, profile)
			if err != nil {
				return nil, err
			}
			code, err := executeTask(ctx, a, cmd, *format, plan)
			if err == nil {
				onExit(code)
			}
			return nil, err
		})(cmd, args)
	}
	return cmd
}

const execHelp = `Run a caller-chosen command with a Task's credentials.

exec is available only for a Task registered with --allow-custom-command. It
resolves the same checkout, working directory, Profile and credential
requirements as run, requires the same device trust decision, and follows the
same output, redaction and exit-code contract. The program is an absolute path,
a path relative to the Task's working directory, or a name on PATH; arguments
are passed as an array, never through a shell.

The command can read, print, encode or exfiltrate every delivered value. A Task
is not OS isolation: enable custom commands only for Tasks whose credentials
you would hand to any program you run.`

const execUsage = "use ctxferry exec --task <task> [--checkout <root>] [--profile <name>] [--format text|jsonl] -- <program> [args...]"

func execCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project, format *string, onExit func(int)) *cobra.Command {
	var profiles, checkouts, tasks []string
	cmd := &cobra.Command{Use: "exec --task <task> -- <program> [args...]", Short: "Run a custom command with a Task's credentials where the Task allows it", Long: execHelp,
		Args: func(cmd *cobra.Command, args []string) error {
			if *format != "text" && *format != "jsonl" || cmd.ArgsLenAtDash() != 0 || len(args) == 0 || len(tasks) != 1 || tasks[0] == "" {
				return app.Fail("usage", execUsage)
			}
			return nil
		}}
	cmd.Flags().StringArrayVar(&tasks, "task", nil, "Task whose credentials and working directory the command uses")
	cmd.Flags().StringArrayVar(&profiles, "profile", nil, "Explicit Profile for this invocation")
	cmd.Flags().StringArrayVar(&checkouts, "checkout", nil, "Explicit Git checkout root for the Task's membership")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			profile, checkout, err := executionFlags(profiles, checkouts)
			if err != nil {
				return nil, err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			plan, err := a.PrepareCustomTask(ctx, *project, cwd, tasks[0], checkout, profile, app.ExecutionCommand{Program: args[0], Argv: args[1:]})
			if err != nil {
				return nil, err
			}
			code, err := executeTask(ctx, a, cmd, *format, plan)
			if err == nil {
				onExit(code)
			}
			return nil, err
		})(cmd, args)
	}
	return cmd
}

func executeTask(ctx context.Context, a app.App, cmd *cobra.Command, format string, plan app.TaskExecution) (int, error) {
	out, diagnostics := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if format == "text" {
		return a.ExecuteTask(ctx, plan, cmd.InOrStdin(), out, diagnostics, func() {})
	}
	stream := taskStream{out: out}
	stream.mu.Lock()
	started := false
	code, err := a.ExecuteTask(ctx, plan, nil, taskOutput{&stream, "stdout"}, taskOutput{&stream, "stderr"}, func() {
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
