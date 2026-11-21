package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type TaskSpec struct {
	Name                   string                  `json:"name"`
	Purpose                string                  `json:"purpose"`
	Inputs                 []string                `json:"inputs"`
	ResourceSlots          []TaskResourceSlot      `json:"resourceSlots"`
	CredentialRequirements []CredentialRequirement `json:"credentialRequirements"`
	MembershipID           string                  `json:"membershipId"`
	WorkingDirectory       string                  `json:"workingDirectory"`
	Program                string                  `json:"program"`
	Argv                   []string                `json:"argv"`
	Platforms              []string                `json:"platforms"`
	// AllowCustomCommand opts the Task into exec: a caller-chosen command
	// receives the Task's credentials in its working directory.
	AllowCustomCommand bool `json:"allowCustomCommand,omitempty"`
}

type TaskResourceSlot struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Stage string `json:"stage"`
}

type CredentialRequirement struct {
	Name         string `json:"name"`
	ResourceSlot string `json:"resourceSlot"`
	Purpose      string `json:"purpose"`
	Type         string `json:"type"`
	Env          string `json:"env"`
}

type Task struct {
	TaskSpec
	ID        string `json:"id"`
	Scope     Scope  `json:"scope"`
	Policy    Policy `json:"policy"`
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
}

type TaskEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

type TaskList struct {
	Project Project `json:"project"`
	Tasks   []Task  `json:"tasks"`
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var taskPlatform = regexp.MustCompile(`^[a-z0-9]+(/[a-z0-9]+)?$`)

func taskText(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 1024 && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// Catalog paths use slash separators on all devices and have no parent traversal.
func taskPath(s string) bool {
	return len(s) <= 1024 && fs.ValidPath(s) && !strings.ContainsAny(s, "\\:\x00") && !strings.ContainsFunc(s, unicode.IsControl)
}

func validateTask(spec TaskSpec) error {
	if !profileName(spec.Name) || !taskText(spec.Purpose) || !validUUID(spec.MembershipID) {
		return Fail("invalid-task", "Task requires a slug, a purpose and a Repository membership UUID")
	}
	program := strings.TrimPrefix(spec.Program, "./")
	if !taskPath(spec.WorkingDirectory) || !taskPath(program) || program == "." || strings.TrimSpace(program) == "" {
		return Fail("invalid-task", "working directory and program must use portable relative paths or a program name")
	}
	if spec.Argv == nil {
		return Fail("invalid-task", "Task argv must be an array")
	}
	if len(spec.Inputs) > 32 || len(spec.ResourceSlots) > 32 || len(spec.CredentialRequirements) > 32 || len(spec.Platforms) > 32 || len(spec.Argv) > 128 {
		return Fail("invalid-task", "Task exceeds its input, slot, platform or argument limit")
	}
	for _, arg := range spec.Argv {
		if len(arg) > 8192 || !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return Fail("invalid-task", "Task arguments must be UTF-8, at most 8192 bytes and contain no NUL")
		}
	}
	for _, input := range spec.Inputs {
		if !taskText(input) {
			return Fail("invalid-task", "inputs must be nonempty plain text of at most 1024 bytes each")
		}
	}
	slots := map[string]bool{}
	for _, slot := range spec.ResourceSlots {
		if !resourceToken.MatchString(slot.Name) || slots[slot.Name] || !taskText(slot.Kind) || !taskText(slot.Stage) {
			return Fail("invalid-task", "Resource slots require distinct names, kinds and stages")
		}
		slots[slot.Name] = true
	}
	requirements, variables := map[string]bool{}, map[string]bool{}
	for _, req := range spec.CredentialRequirements {
		env := strings.ToUpper(req.Env)
		if !resourceToken.MatchString(req.Name) || requirements[req.Name] || !slots[req.ResourceSlot] ||
			!taskText(req.Purpose) || !taskText(req.Type) || len(req.Env) > 128 || !environmentName.MatchString(req.Env) || variables[env] {
			return Fail("invalid-task", "credential requirements need distinct names and delivery variables, an existing Resource slot, purpose and type")
		}
		requirements[req.Name], variables[env] = true, true
	}
	platforms := map[string]bool{}
	for _, platform := range spec.Platforms {
		if len(platform) > 63 || !taskPlatform.MatchString(platform) || platforms[platform] {
			return Fail("invalid-task", "platform conditions must be distinct OS or os/arch labels")
		}
		platforms[platform] = true
	}
	return nil
}

func (spec TaskSpec) slot(name string) (TaskResourceSlot, bool) {
	for _, slot := range spec.ResourceSlots {
		if slot.Name == name {
			return slot, true
		}
	}
	return TaskResourceSlot{}, false
}

func taskFromRecord(r remoteRecord) Task {
	return Task{TaskSpec: r.Task, ID: r.ID, Scope: r.Scope, Policy: r.Policy, Revision: r.Revision, UpdatedAt: r.Provenance.UpdatedAt}
}

func (a App) AddTask(ctx context.Context, selector, cwd, storage string, spec TaskSpec) (Task, error) {
	var out Task
	if storage != "shared" && storage != "local-only" {
		return out, Fail("invalid-policy", "choose storage local-only or shared")
	}
	if err := validateTask(spec); err != nil {
		return out, err
	}
	shared := storage == "shared"
	if shared {
		release, err := a.syncOwner(ctx)
		if err != nil {
			return out, err
		}
		defer release()
	}
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	if shared {
		if err := a.prepareBaseline(ctx, db); err != nil {
			return out, err
		}
	}
	err = a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		if _, _, err := taskMembership(ctx, c, project.ID, spec.MembershipID); err != nil {
			return false, err
		}
		env := publishedEnvelope{SchemaVersion: 1, ID: uuid.NewString(), Kind: "task", Scope: Scope{"project", project.ID},
			Policy: Policy{storage, "full"}, Revision: uuid.NewString(), OperationID: uuid.NewString(), Payload: spec}
		env.Provenance.UpdatedAt = now()
		if err := c.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&env.WorkspaceID, &env.Provenance.DeviceID); err != nil {
			return false, err
		}
		raw, err := json.Marshal(env)
		if err != nil {
			return false, err
		}
		if len(raw)+1 > MetadataLimit {
			return false, Fail("size-limit", "Task record envelope exceeds 256 KiB")
		}
		if _, err = c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,'task',?)", env.ID, project.ID, string(raw)); err != nil {
			return false, err
		}
		out = taskFromRecord(remoteRecord{publishedEnvelope: env, Task: spec})
		return shared, nil
	})
	return out, err
}

const liveTasks = ` FROM records WHERE kind='task' AND project_id=? AND coalesce(json_extract(metadata,'$.deleted'),0)=0`

type taskReader interface {
	rowQuerier
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readTasks(ctx context.Context, q taskReader, projectID, selector string) ([]Task, error) {
	query := "SELECT metadata" + liveTasks
	args := []any{projectID}
	if selector != "" {
		if validUUID(selector) {
			query += " AND id=?"
		} else {
			query += " AND json_extract(metadata,'$.payload.name')=?"
		}
		query += " ORDER BY id LIMIT 2"
		args = append(args, selector)
	} else {
		query += " ORDER BY id"
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		r, err := decodeRecord(raw)
		if err != nil {
			return nil, err
		}
		if err := validateTask(r.Task); err != nil {
			return nil, err
		}
		out = append(out, taskFromRecord(r))
	}
	return out, rows.Err()
}

func loadTask(ctx context.Context, q taskReader, projectID, selector string) (Task, error) {
	if selector == "" {
		return Task{}, Fail("usage", "specify a Task name or UUID")
	}
	tasks, err := readTasks(ctx, q, projectID, selector)
	if err != nil {
		return Task{}, err
	}
	switch len(tasks) {
	case 0:
		return Task{}, Fail("task-not-found", "Task not found in this Project")
	case 1:
		return tasks[0], nil
	default:
		return Task{}, Fail("ambiguous-task", "multiple Tasks have this name; use the UUID from task list")
	}
}

func (a App) ListTasks(ctx context.Context, selector, cwd string) (TaskList, error) {
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return TaskList{}, err
	}
	defer db.Close()
	tasks, err := readTasks(ctx, db, project.ID, "")
	return TaskList{Project: project, Tasks: tasks}, err
}

func (a App) InspectTask(ctx context.Context, selector, cwd, name string) (Task, error) {
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return Task{}, err
	}
	defer db.Close()
	return loadTask(ctx, db, project.ID, name)
}

func taskMembership(ctx context.Context, q rowQuerier, projectID, id string) (repository, subdir string, err error) {
	err = q.QueryRowContext(ctx, `SELECT m.repository_id,m.subdirectory FROM memberships m
		WHERE m.id=? AND m.project_id=? AND `+live("m.id", "m.repository_id", "m.project_id"),
		id, projectID).Scan(&repository, &subdir)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !taskPath(subdir) {
		err = Fail("invalid-reference", "Task must reference a live Repository membership in its Project")
	}
	return
}

// TaskChange replaces the given execution and descriptive fields of a Task.
// Nil fields keep their current values.
type TaskChange struct {
	IfRevision             string
	Purpose                *string
	Inputs                 *[]string
	ResourceSlots          *[]TaskResourceSlot
	CredentialRequirements *[]CredentialRequirement
	MembershipID           *string
	WorkingDirectory       *string
	Program                *string
	Argv                   *[]string
	Platforms              *[]string
	AllowCustomCommand     *bool
}

func (c TaskChange) apply(spec TaskSpec) TaskSpec {
	set := func(dst *[]string, src *[]string) {
		if src != nil {
			*dst = append([]string{}, (*src)...)
		}
	}
	if c.Purpose != nil {
		spec.Purpose = *c.Purpose
	}
	set(&spec.Inputs, c.Inputs)
	set(&spec.Argv, c.Argv)
	set(&spec.Platforms, c.Platforms)
	if c.ResourceSlots != nil {
		spec.ResourceSlots = append([]TaskResourceSlot{}, (*c.ResourceSlots)...)
	}
	if c.CredentialRequirements != nil {
		spec.CredentialRequirements = append([]CredentialRequirement{}, (*c.CredentialRequirements)...)
	}
	if c.MembershipID != nil {
		spec.MembershipID = *c.MembershipID
	}
	if c.WorkingDirectory != nil {
		spec.WorkingDirectory = *c.WorkingDirectory
	}
	if c.Program != nil {
		spec.Program = *c.Program
	}
	if c.AllowCustomCommand != nil {
		spec.AllowCustomCommand = *c.AllowCustomCommand
	}
	return spec
}

// UpdateTask writes a new revision of a Task. It requires the current
// revision, so a concurrent local or synced change is never overwritten.
// Changes to execution fields take effect with credentials on each device
// only after that device enables the new execution definition.
func (a App) UpdateTask(ctx context.Context, selector, cwd, name string, change TaskChange) (Task, error) {
	var out Task
	if !validUUID(change.IfRevision) {
		return out, Fail("usage", "task update requires the current --if-revision from task inspect")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	err = a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		old, err := loadTask(ctx, c, project.ID, name)
		if err != nil {
			return false, err
		}
		if err := requireNoConflict(ctx, c, old.ID); err != nil {
			return false, err
		}
		if old.Revision != change.IfRevision {
			return false, Fail("revision-conflict", "Task changed; read its current revision before updating")
		}
		spec := change.apply(old.TaskSpec)
		if err := validateTask(spec); err != nil {
			return false, err
		}
		if _, _, err := taskMembership(ctx, c, project.ID, spec.MembershipID); err != nil {
			return false, err
		}
		if err := requireMappableTask(ctx, c, old.ID, old.TaskSpec, spec); err != nil {
			return false, err
		}
		env := publishedEnvelope{SchemaVersion: 1, ID: old.ID, Kind: "task", Scope: old.Scope, Policy: old.Policy,
			Revision: uuid.NewString(), PreviousRevision: old.Revision, OperationID: uuid.NewString(), Payload: spec}
		env.Provenance.UpdatedAt = now()
		if err := c.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&env.WorkspaceID, &env.Provenance.DeviceID); err != nil {
			return false, err
		}
		raw, err := json.Marshal(env)
		if err != nil {
			return false, err
		}
		if len(raw)+1 > MetadataLimit {
			return false, Fail("size-limit", "Task record envelope exceeds 256 KiB")
		}
		if _, err = c.ExecContext(ctx, "UPDATE records SET metadata=? WHERE id=? AND kind='task'", string(raw), old.ID); err != nil {
			return false, err
		}
		out = taskFromRecord(remoteRecord{publishedEnvelope: env, Task: spec})
		return old.Policy.Storage == "shared", nil
	})
	return out, err
}

// requireMappableTask refuses a Task change that breaks a Profile mapping
// which was valid before it: a removed or incompatible slot or requirement.
// Sync would block publishing such a Task, and unmapping needs the old names,
// so the mapping is changed first. Mappings that were already broken do not
// block an edit, so a repair is never refused.
func requireMappableTask(ctx context.Context, c *sql.Conn, taskID string, before, after TaskSpec) error {
	rows, err := c.QueryContext(ctx, "SELECT id FROM configurations ORDER BY id")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	stranded := func(profile, kind, name string) error {
		return &Error{Code: "invalid-task-dependency", Message: "Profile " + profile + " maps " + kind + " " + name +
			", which this change removes or makes incompatible; change or unmap it first",
			Details: map[string]string{"profile": profile, kind: name}}
	}
	for _, id := range ids {
		cfg, exists, err := readConfiguration(c.QueryRowContext(ctx, "SELECT payload FROM configurations WHERE id=?", id), false)
		if err != nil || !exists {
			return err
		}
		resources := map[string]string{}
		for _, m := range cfg.ResourceMappings {
			if m.TaskID != taskID {
				continue
			}
			resources[m.Slot] = m.ResourceID
			resource, err := loadMappedResource(ctx, c, m.ResourceID)
			if err == nil && compatibleResource(before, m.Slot, resource.ResourceSpec) && !compatibleResource(after, m.Slot, resource.ResourceSpec) {
				return stranded(cfg.Profile, "slot", m.Slot)
			}
		}
		for _, m := range cfg.SecretMappings {
			if m.TaskID != taskID {
				continue
			}
			binding, err := loadSecretBinding(ctx, c, m.BindingID)
			if err != nil {
				continue
			}
			valid := func(spec TaskSpec) bool {
				req, ok := spec.requirement(m.Requirement)
				return ok && compatibleSecret(req, resources[req.ResourceSlot], binding.SecretBindingSpec)
			}
			if valid(before) && !valid(after) {
				return stranded(cfg.Profile, "requirement", m.Requirement)
			}
		}
	}
	return nil
}
