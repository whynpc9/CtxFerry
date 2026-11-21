package app

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type TaskExecution struct {
	Task    Task
	Dir     string
	Profile string
	// The private snapshot is the only input to authorization and launch, so
	// changes to the exported fields cannot redirect delivered credentials.
	launch      *taskLaunch
	checkout    checkoutPin
	definition  ExecutionDefinition
	credentials []plannedCredential
}

// checkoutPin is the Git checkout selected for a Task: its root and common
// directory. A pinned checkout is revalidated from the catalog alone.
type checkoutPin struct {
	root, commonDir string
}

type taskLaunch struct {
	program string
	argv    []string
	dir     string
	// custom launches a caller-chosen program: an absolute path, a path
	// relative to the working directory, or a name looked up on PATH.
	custom bool
}

// Definition returns a copy of the credentialed execution definition.
func (p TaskExecution) Definition() ExecutionDefinition {
	d := p.definition
	d.Argv = slices.Clone(d.Argv)
	d.Credentials = slices.Clone(d.Credentials)
	if d.CustomCommand != nil {
		d.CustomCommand = &ExecutionCommand{Program: d.CustomCommand.Program, Argv: slices.Clone(d.CustomCommand.Argv)}
	}
	return d
}

// PrepareTask resolves the Task's registered command for run.
func (a App) PrepareTask(ctx context.Context, project, cwd, name, checkout, profile string) (TaskExecution, error) {
	return a.prepareTask(ctx, project, cwd, name, checkout, profile, nil, nil)
}

// PrepareCustomTask resolves a caller-chosen command for exec. It resolves the
// same Task, checkout, Profile and credential requirements as run, and is
// refused unless the Task explicitly allows custom commands.
func (a App) PrepareCustomTask(ctx context.Context, project, cwd, name, checkout, profile string, command ExecutionCommand) (TaskExecution, error) {
	if command.Program == "" || strings.ContainsRune(command.Program, 0) || len(command.Argv) > 4096 {
		return TaskExecution{}, Fail("usage", "exec requires a program after --")
	}
	if !filepath.IsAbs(command.Program) && (filepath.VolumeName(command.Program) != "" ||
		os.IsPathSeparator(command.Program[0]) || command.Program[0] == '/') {
		// Windows drive-relative (C:tool) and drive-rooted (\tool) paths depend
		// on per-process drive state; refuse them before any credential work.
		return TaskExecution{}, Fail("invalid-command", "exec program must be absolute, relative to the Task's working directory, or a name on PATH")
	}
	for _, arg := range command.Argv {
		if strings.ContainsRune(arg, 0) {
			return TaskExecution{}, Fail("usage", "exec arguments must not contain NUL")
		}
	}
	command.Argv = slices.Clone(command.Argv)
	if command.Argv == nil {
		command.Argv = []string{}
	}
	return a.prepareTask(ctx, project, cwd, name, checkout, profile, &command, nil)
}

// prepareTask resolves a Task from one catalog snapshot. With a pinned
// checkout it runs no Git command: the checkout must still be bound to the
// Task's Repository on this device, and the membership root is recomputed.
func (a App) prepareTask(ctx context.Context, project, cwd, name, checkout, profile string, custom *ExecutionCommand, pin *checkoutPin) (TaskExecution, error) {
	var out TaskExecution
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	r, err := resolveProjectLocation(ctx, db, project, cwd, false)
	if err != nil {
		return out, err
	}
	if err := requireSubscription(ctx, db, r.Project.ID); err != nil {
		return out, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	task, err := loadTask(ctx, tx, r.Project.ID, name)
	if err != nil {
		return out, err
	}
	if custom != nil && !task.AllowCustomCommand {
		return out, &Error{Code: "custom-command-not-allowed", Message: "Task " + task.Name +
			" does not allow custom commands; use ctxferry run " + task.Name, Details: map[string]string{"taskId": task.ID, "task": task.Name}}
	}
	if len(task.Platforms) != 0 && !slices.Contains(task.Platforms, runtime.GOOS) &&
		!slices.Contains(task.Platforms, runtime.GOOS+"/"+runtime.GOARCH) {
		return out, Fail("platform-unsupported", "Task does not apply to the current platform")
	}
	repository, subdir, err := taskMembership(ctx, tx, r.Project.ID, task.MembershipID)
	if err != nil {
		return out, err
	}
	for _, id := range []string{r.Project.ID, task.ID, task.MembershipID, repository} {
		if err := requireNoConflict(ctx, tx, id); err != nil {
			return out, err
		}
	}
	var selectedCheckout checkoutPin
	var root string
	if pin != nil {
		selectedCheckout, root, err = pinnedCheckout(ctx, tx, r.Device.ID, repository, subdir, *pin)
	} else {
		selectedCheckout, root, err = taskCheckout(ctx, tx, r.Device.ID, repository, subdir, checkout)
	}
	if err != nil {
		return out, err
	}
	out.checkout = selectedCheckout
	rel, err := filepath.Localize(task.WorkingDirectory)
	if err != nil {
		return out, Fail("invalid-working-directory", "Task working directory is invalid on this platform")
	}
	dir, err := canonical(filepath.Join(root, rel))
	if err != nil || !contains(root, dir) {
		return out, Fail("invalid-working-directory", "Task working directory must exist inside its membership root")
	}
	loc := configurationLocation{project: *r.Project, device: r.Device.ID, checkout: &CurrentCheckout{Path: root}}
	if err := tx.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&loc.workspace); err != nil {
		return out, err
	}
	selected, err := chooseProfile(ctx, tx, loc, profile)
	if err != nil {
		return out, err
	}
	// Credential mappings report a conflicted Profile or Resource as binding-conflict.
	mappingConflict := requireNoConflict
	if len(task.CredentialRequirements) != 0 {
		mappingConflict = func(ctx context.Context, q rowQuerier, id string) error { return requireNoBindingConflict(ctx, q, id) }
	}
	var cfg configuration
	if selected != nil {
		if err := mappingConflict(ctx, tx, selected.ID); err != nil {
			return out, err
		}
		value, exists, err := readConfiguration(tx.QueryRowContext(ctx, "SELECT payload FROM configurations WHERE id=?", selected.ID), false)
		if err != nil {
			return out, err
		}
		if !exists {
			return out, Fail("profile-not-found", "Profile was not found in this Project")
		}
		cfg = value
		out.Profile = selected.Name
	}
	if len(task.ResourceSlots) != 0 && selected == nil {
		return out, Fail("profile-required", "select a Profile or use --profile to resolve the Task's Resource slots")
	}
	for _, slot := range task.ResourceSlots {
		id, err := mappedResourceID(cfg, task, slot.Name)
		if err != nil {
			return out, err
		}
		if err := mappingConflict(ctx, tx, id); err != nil {
			return out, err
		}
		resource, err := loadMappedResource(ctx, tx, id)
		if err != nil {
			return out, err
		}
		if err := mappingConflict(ctx, tx, resource.Scope.ID); err != nil {
			return out, err
		}
		if resource.Policy.Storage != "shared" || !compatibleResource(task.TaskSpec, slot.Name, resource.ResourceSpec) {
			return out, Fail("incompatible", "mapped Resource does not match the Task slot")
		}
		if len(resource.Platforms) > 0 && !slices.Contains(resource.Platforms, runtime.GOOS+"/"+runtime.GOARCH) {
			return out, Fail("platform-unsupported", "mapped Resource does not apply to the current platform")
		}
	}
	if len(task.CredentialRequirements) != 0 {
		out.credentials, err = planCredentials(ctx, tx, task, selected, cfg)
		if err != nil {
			return out, err
		}
		out.definition = ExecutionDefinition{TaskID: task.ID, TaskRevision: task.Revision, MembershipID: task.MembershipID,
			RepositoryID: repository, CheckoutRoot: selectedCheckout.root, Program: task.Program, Argv: slices.Clone(task.Argv), AllowCustomCommand: task.AllowCustomCommand,
			MembershipRoot: root, WorkingDirectory: dir, ProfileID: selected.ID, Profile: selected.Name,
			Credentials: make([]ExecutionCredential, 0, len(out.credentials))}
		for _, c := range out.credentials {
			out.definition.Credentials = append(out.definition.Credentials, c.ExecutionCredential)
		}
	}
	out.Task, out.Dir = task, dir
	// The trust decision and the launch read the same snapshot, so the command
	// that was authorized is the command that runs.
	out.launch = &taskLaunch{program: task.Program, argv: slices.Clone(task.Argv), dir: dir}
	if custom != nil {
		out.launch = &taskLaunch{program: custom.Program, argv: slices.Clone(custom.Argv), dir: dir, custom: true}
		if len(out.credentials) != 0 {
			out.definition.CustomCommand = &ExecutionCommand{Program: custom.Program, Argv: slices.Clone(custom.Argv)}
		}
	}
	return out, nil
}

// Checkout bindings identify Git common directories. A locally registered clone
// contributes every usable worktree, even if the caller is in another repository.
func taskCheckout(ctx context.Context, q taskReader, device, repository, subdir, explicit string) (checkoutPin, string, error) {
	commonDirs, err := boundCommonDirs(ctx, q, device, repository)
	if err != nil {
		return checkoutPin{}, "", err
	}
	rel, err := filepath.Localize(subdir)
	if err != nil {
		return checkoutPin{}, "", Fail("invalid-reference", "membership directory is invalid on this platform")
	}
	if explicit != "" {
		explicit, err = canonical(explicit)
		if err != nil {
			return checkoutPin{}, "", Fail("checkout-not-found", "--checkout must name a usable Git checkout root")
		}
	}
	type candidate struct {
		pin        checkoutPin
		memberRoot string
	}
	candidates := map[string]candidate{}
	for _, common := range commonDirs {
		gitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, err := discoveryGit(gitCtx, common, "worktree", "list", "--porcelain", "-z").Output()
		cancel()
		if err != nil {
			continue
		}
		for _, field := range strings.Split(string(raw), "\x00") {
			path, ok := strings.CutPrefix(field, "worktree ")
			if !ok {
				continue
			}
			root, err := canonical(path)
			if err != nil || explicit != "" && root != explicit {
				continue
			}
			g := discoverGit(ctx, root)
			if g.Status != "available" || g.CheckoutRoot != root || g.CommonDir != common {
				continue
			}
			memberRoot, err := canonical(filepath.Join(root, rel))
			if err == nil && contains(root, memberRoot) {
				candidates[root] = candidate{checkoutPin{root, common}, memberRoot}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return checkoutPin{}, "", err
	}
	switch len(candidates) {
	case 0:
		return checkoutPin{}, "", Fail("checkout-not-found", "bind a local checkout of the Task's Repository membership or choose a usable root with --checkout")
	case 1:
		for _, c := range candidates {
			return c.pin, c.memberRoot, nil
		}
	}
	paths := make([]string, 0, len(candidates))
	for path := range candidates {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return checkoutPin{}, "", &Error{Code: "ambiguous-checkout", Message: "multiple local checkouts match the Task; choose a Git checkout root with --checkout", Details: paths}
}

// pinnedCheckout revalidates a checkout chosen by an earlier taskCheckout
// against the current catalog without running Git.
func pinnedCheckout(ctx context.Context, q taskReader, device, repository, subdir string, pin checkoutPin) (checkoutPin, string, error) {
	commonDirs, err := boundCommonDirs(ctx, q, device, repository)
	if err != nil {
		return checkoutPin{}, "", err
	}
	rel, err := filepath.Localize(subdir)
	if err != nil {
		return checkoutPin{}, "", Fail("invalid-reference", "membership directory is invalid on this platform")
	}
	if !slices.Contains(commonDirs, pin.commonDir) {
		return checkoutPin{}, "", Fail("checkout-not-found", "the selected checkout is no longer bound to the Task's Repository")
	}
	memberRoot, err := canonical(filepath.Join(pin.root, rel))
	if err != nil || !contains(pin.root, memberRoot) {
		return checkoutPin{}, "", Fail("checkout-not-found", "the Task's membership directory is not usable in the selected checkout")
	}
	return pin, memberRoot, nil
}

func boundCommonDirs(ctx context.Context, q taskReader, device, repository string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT b.git_common_dir FROM checkout_bindings b
		JOIN memberships m ON m.id=b.membership_id
		WHERE b.device_id=? AND m.repository_id=? AND coalesce(b.git_common_dir,'')<>''
		AND `+live("m.id", "m.project_id")+` ORDER BY 1`, device, repository)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var commonDirs []string
	for rows.Next() {
		var common string
		if err := rows.Scan(&common); err != nil {
			return nil, err
		}
		commonDirs = append(commonDirs, common)
	}
	return commonDirs, rows.Err()
}

// ExecuteTask runs a prepared Task and forwards byte streams. A Task with
// credential requirements runs only after the device trust decision, a
// reachability check and device-local provider resolution. Its child receives
// the neutral base environment and the Task's delivered variables only, and
// delivered values are replaced in its output.
func (a App) ExecuteTask(ctx context.Context, plan TaskExecution, stdin io.Reader, stdout, stderr io.Writer, started func()) (int, error) {
	if plan.launch == nil {
		return 0, Fail("credentials-required", "prepare the Task before execution")
	}
	launch := *plan.launch
	env := taskEnvironment()
	if len(plan.credentials) != 0 {
		delivered, values, err := a.credentialEnvironment(ctx, plan)
		if ctx.Err() != nil {
			for _, value := range values {
				clear(value)
			}
			return 0, Fail("interrupted", "Task execution was interrupted")
		}
		if err != nil {
			return 0, err
		}
		defer func() {
			for _, value := range values {
				clear(value)
			}
		}()
		env = childEnvironment(delivered)
		out := newRedactor(stdout, values)
		defer out.wipe()
		diagnostics := out
		// One destination keeps one serialized stream, as os/exec does.
		if !sameWriter(stdout, stderr) {
			diagnostics = newRedactor(stderr, values)
			defer diagnostics.wipe()
		}
		code, err := runChild(ctx, launch, env, stdin, out, diagnostics, started)
		if err != nil && ctx.Err() != nil {
			err = Fail("interrupted", "Task execution was interrupted")
		}
		// Held partial matches are released only after the child has exited.
		flushOut, flushDiagnostics := out.Flush(), diagnostics.Flush()
		if err == nil && (flushOut != nil || flushDiagnostics != nil) {
			err = Fail("execution-failed", "could not write Task output")
		}
		return code, err
	}
	return runChild(ctx, launch, env, stdin, stdout, stderr, started)
}

func sameWriter(a, b io.Writer) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

func runChild(ctx context.Context, launch taskLaunch, env []string, stdin io.Reader, stdout, stderr io.Writer, started func()) (int, error) {
	program, err := launchProgram(launch)
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, program, launch.argv...)
	if cmd.Err == nil && !filepath.IsAbs(cmd.Path) {
		return 0, Fail("launch-failed", "Task program must resolve to an absolute executable path")
	}
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = launch.dir, stdin, stdout, stderr
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return 0, Fail("launch-failed", err.Error())
	}
	started()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return 0, Fail("interrupted", "Task execution was interrupted")
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code >= 0 {
			return code, nil
		}
		return 1, Fail("child-terminated", "Task process terminated without an exit code")
	}
	if err != nil {
		return 0, Fail("execution-failed", err.Error())
	}
	return 0, nil
}

// launchProgram resolves a registered program like #13 did: a slash-separated
// path is relative to the working directory, otherwise a name on PATH. A
// custom exec program may also be absolute or use the platform separator.
func launchProgram(launch taskLaunch) (string, error) {
	if launch.custom {
		// The caller's path is passed to the OS unchanged: lexical cleaning
		// would resolve ".." before symlinks and could select another file.
		switch {
		case filepath.IsAbs(launch.program):
			return launch.program, nil
		case strings.ContainsAny(launch.program, "/"+string(filepath.Separator)):
			sep := string(filepath.Separator)
			return strings.TrimSuffix(launch.dir, sep) + sep + launch.program, nil
		}
		return launch.program, nil
	}
	program, err := filepath.Localize(strings.TrimPrefix(launch.program, "./"))
	if err != nil {
		return "", Fail("launch-failed", "Task program path is invalid on this platform")
	}
	if strings.Contains(launch.program, "/") {
		program = filepath.Join(launch.dir, program)
	}
	return program, nil
}

var baseEnvironmentNames = []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "HOME", "USERPROFILE", "TMP", "TMPDIR", "TEMP",
	"LANG", "LC_ALL", "TERM", "COLORTERM"}

// environmentKey folds variable names only where the platform does.
func environmentKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

// taskEnvironment is the credential-free base, unchanged from #13: names are
// folded on every platform.
func taskEnvironment() []string {
	return baseEnvironment(strings.ToUpper)
}

// baseEnvironment selects the neutral variables passed to a Task child.
func baseEnvironment(key func(string) string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(baseEnvironmentNames, key(name)) {
			env = append(env, entry)
		}
	}
	return env
}
