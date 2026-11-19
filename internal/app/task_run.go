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
}

func (a App) PrepareTask(ctx context.Context, project, cwd, name, checkout, profile string) (TaskExecution, error) {
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
	if len(task.CredentialRequirements) != 0 {
		return out, Fail("credentials-required", "this Task declares credential requirements and cannot run in credential-free mode")
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
	root, err := taskCheckout(ctx, tx, r.Device.ID, repository, subdir, checkout)
	if err != nil {
		return out, err
	}
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
	var cfg configuration
	if selected != nil {
		if err := requireNoConflict(ctx, tx, selected.ID); err != nil {
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
		i := slices.IndexFunc(cfg.ResourceMappings, func(m ResourceMapping) bool { return m.TaskID == task.ID && m.Slot == slot.Name })
		if i < 0 {
			return out, Fail("binding-missing", "Profile has no Resource mapping for Task "+task.Name+" slot "+slot.Name)
		}
		id := cfg.ResourceMappings[i].ResourceID
		if err := requireNoConflict(ctx, tx, id); err != nil {
			return out, err
		}
		resource, err := loadResource(ctx, tx, r.Project.ID, id)
		if err != nil {
			return out, err
		}
		if resource.Policy.Storage != "shared" || !compatibleResource(task.TaskSpec, slot.Name, resource.ResourceSpec) {
			return out, Fail("incompatible", "mapped Resource does not match the Task slot")
		}
		if len(resource.Platforms) > 0 && !slices.Contains(resource.Platforms, runtime.GOOS+"/"+runtime.GOARCH) {
			return out, Fail("platform-unsupported", "mapped Resource does not apply to the current platform")
		}
	}
	out.Task, out.Dir = task, dir
	return out, nil
}

// Checkout bindings identify Git common directories. A locally registered clone
// contributes every usable worktree, even if the caller is in another repository.
func taskCheckout(ctx context.Context, q taskReader, device, repository, subdir, explicit string) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT b.git_common_dir FROM checkout_bindings b
		JOIN memberships m ON m.id=b.membership_id
		WHERE b.device_id=? AND m.repository_id=? AND coalesce(b.git_common_dir,'')<>''
		AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.id IN (m.id,m.project_id)) ORDER BY 1`, device, repository)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var commonDirs []string
	for rows.Next() {
		var common string
		if err := rows.Scan(&common); err != nil {
			return "", err
		}
		commonDirs = append(commonDirs, common)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	rows.Close()
	rel, err := filepath.Localize(subdir)
	if err != nil {
		return "", Fail("invalid-reference", "membership directory is invalid on this platform")
	}
	if explicit != "" {
		explicit, err = canonical(explicit)
		if err != nil {
			return "", Fail("checkout-not-found", "--checkout must name a usable Git checkout root")
		}
	}
	candidates := map[string]string{}
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
				candidates[root] = memberRoot
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch len(candidates) {
	case 0:
		return "", Fail("checkout-not-found", "bind a local checkout of the Task's Repository membership or choose a usable root with --checkout")
	case 1:
		for _, root := range candidates {
			return root, nil
		}
	}
	paths := make([]string, 0, len(candidates))
	for path := range candidates {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return "", &Error{Code: "ambiguous-checkout", Message: "multiple local checkouts match the Task; choose a Git checkout root with --checkout", Details: paths}
}

// ExecuteTask runs only a prepared credential-free Task and forwards byte streams.
func ExecuteTask(ctx context.Context, plan TaskExecution, stdin io.Reader, stdout, stderr io.Writer, started func()) (int, error) {
	if len(plan.Task.CredentialRequirements) != 0 {
		return 0, Fail("credentials-required", "this Task declares credential requirements and cannot run in credential-free mode")
	}
	program, err := filepath.Localize(strings.TrimPrefix(plan.Task.Program, "./"))
	if err != nil {
		return 0, Fail("launch-failed", "Task program path is invalid on this platform")
	}
	if strings.Contains(plan.Task.Program, "/") {
		program = filepath.Join(plan.Dir, program)
	}
	cmd := exec.CommandContext(ctx, program, plan.Task.Argv...)
	if cmd.Err == nil && !filepath.IsAbs(cmd.Path) {
		return 0, Fail("launch-failed", "Task program must resolve to an absolute executable path")
	}
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = plan.Dir, stdin, stdout, stderr
	cmd.Env = taskEnvironment()
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

func taskEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "HOME", "USERPROFILE", "TMP", "TMPDIR", "TEMP",
			"LANG", "LC_ALL", "TERM", "COLORTERM":
			env = append(env, entry)
		}
	}
	return env
}
