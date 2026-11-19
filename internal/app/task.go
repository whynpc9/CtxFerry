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
	"github.com/whynpc9/CtxFerry/internal/store"
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
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		if _, _, err := taskMembership(ctx, c, project.ID, spec.MembershipID); err != nil {
			return err
		}
		env := publishedEnvelope{SchemaVersion: 1, ID: uuid.NewString(), Kind: "task", Scope: Scope{"project", project.ID},
			Policy: Policy{storage, "full"}, Revision: uuid.NewString(), OperationID: uuid.NewString(), Payload: spec}
		env.Provenance.UpdatedAt = now()
		if err := c.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&env.WorkspaceID, &env.Provenance.DeviceID); err != nil {
			return err
		}
		raw, err := json.Marshal(env)
		if err != nil {
			return err
		}
		if len(raw)+1 > MetadataLimit {
			return Fail("size-limit", "Task record envelope exceeds 256 KiB")
		}
		if _, err = c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,'task',?)", env.ID, project.ID, string(raw)); err != nil {
			return err
		}
		out = taskFromRecord(remoteRecord{publishedEnvelope: env, Task: spec})
		if shared {
			return queueChanges(ctx, c)
		}
		return nil
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
		WHERE m.id=? AND m.project_id=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.id IN (m.id,m.repository_id,m.project_id))`,
		id, projectID).Scan(&repository, &subdir)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !taskPath(subdir) {
		err = Fail("invalid-reference", "Task must reference a live Repository membership in its Project")
	}
	return
}
