package app

import (
	"context"
	"database/sql"
	"net/url"
	"slices"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

// DesktopCredentialCatalog lists selection metadata without running a task,
// consulting secure storage, contacting a provider or opening a checkout.
type DesktopCredentialCatalog struct {
	Profiles []string
	Tasks    []DesktopCredentialTask
}

type DesktopCredentialTask struct {
	ID, Name string
	Slots    []DesktopCredentialRequirement
}

type DesktopCredentialRequirement struct {
	Name, Purpose, Type string
}

type DesktopCredentialSlot struct {
	Status   SecretStatus
	Resource Resource
	Reusable []SecretBinding
	// Issue codes intentionally exclude raw backend, SQL and CLI messages.
	Issue string

	workspace, profileRevision, taskRevision string
}

func (a App) DesktopCredentials(ctx context.Context, project string) (DesktopCredentialCatalog, error) {
	var out DesktopCredentialCatalog
	db, p, err := a.openProjectReadOnly(ctx, project)
	if err != nil {
		return out, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT json_extract(payload,'$.profile') FROM configurations
		WHERE project_id=? AND json_type(payload,'$.profile')='text'
		AND `+live("configurations.id")+` ORDER BY 1`, p.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return out, err
		}
		if profileName(name) {
			out.Profiles = append(out.Profiles, name)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	tasks, err := readTasks(ctx, db, p.ID, "")
	if err != nil {
		return out, err
	}
	for _, task := range tasks {
		entry := DesktopCredentialTask{ID: task.ID, Name: task.Name}
		for _, req := range task.CredentialRequirements {
			entry.Slots = append(entry.Slots, DesktopCredentialRequirement{req.Name, req.Purpose, req.Type})
		}
		if len(entry.Slots) > 0 {
			out.Tasks = append(out.Tasks, entry)
		}
	}
	return out, nil
}

func (a App) DesktopSecretSlot(ctx context.Context, project string, target SecretTarget) (DesktopCredentialSlot, error) {
	out := DesktopCredentialSlot{Status: SecretStatus{Task: target.Task, Requirement: target.Requirement}}
	db, p, err := a.openProjectReadOnly(ctx, project)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	loc := configurationLocation{project: p}
	if err := tx.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&loc.workspace, &loc.device); err != nil {
		return out, err
	}
	out.workspace, out.Status.Project, out.Status.DeviceID = loc.workspace, p, loc.device
	err = readDesktopSecretSlot(ctx, tx, loc, target, &out)
	if err != nil {
		switch code := Classify(err).Code; code {
		case "binding-missing", "binding-conflict", "incompatible", "profile-not-found", "profile-required", "task-not-found", "ambiguous-task":
			out.Issue = code
		default:
			return out, err
		}
	}
	return out, tx.Commit()
}

func readDesktopSecretSlot(ctx context.Context, tx *sql.Tx, loc configurationLocation, target SecretTarget, out *DesktopCredentialSlot) error {
	p, cfg, err := secretProfile(ctx, tx, loc, target.Profile)
	if err != nil {
		return err
	}
	out.Status.Profile, out.profileRevision = p.Name, cfg.Revision
	task, err := loadTask(ctx, tx, loc.project.ID, target.Task)
	if err != nil {
		return err
	}
	out.Status.Task, out.Status.TaskID, out.taskRevision = task.Name, task.ID, task.Revision
	req, ok := task.requirement(target.Requirement)
	if !ok {
		return Fail("incompatible", "Task does not declare this credential requirement")
	}
	out.Status.ResourceSlot = req.ResourceSlot
	if err := requireNoBindingConflict(ctx, tx, task.ID); err != nil {
		return err
	}
	resource, err := secretResource(ctx, tx, cfg, task, req)
	if err != nil {
		return err
	}
	out.Resource = resource
	if err := readSecretReachability(ctx, tx, "", resource, &out.Status); err != nil {
		return err
	}
	out.Reusable, err = reusableDesktopBindings(ctx, tx, loc.project.ID, resource.ID, req)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(cfg.SecretMappings, func(m SecretMapping) bool { return m.TaskID == task.ID && m.Requirement == req.Name })
	return readSecretMapping(ctx, tx, cfg, req, resource, i, &out.Status)
}

func reusableDesktopBindings(ctx context.Context, tx *sql.Tx, project, resource string, req CredentialRequirement) ([]SecretBinding, error) {
	rows, err := tx.QueryContext(ctx, "SELECT metadata"+liveSecretBindings+`
		AND json_extract(metadata,'$.payload.resourceId')=?
		AND (project_id NOT IN (SELECT project_id FROM project_subscriptions WHERE subscribed=0)
			OR id IN (SELECT json_extract(m.value,'$.bindingId') FROM configurations p,
			json_each(p.payload,'$.secretMappings') m WHERE p.project_id=?))
		ORDER BY json_extract(metadata,'$.payload.alias'), id`, resource, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []SecretBinding
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		record, err := secretBindingRecord(raw)
		if err != nil {
			return nil, err
		}
		if compatibleSecret(req, resource, record.SecretBinding) {
			candidates = append(candidates, secretBindingFromRecord(record))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	out := candidates[:0]
	for _, b := range candidates {
		err := requireNoBindingConflict(ctx, tx, b.ID, b.Scope.ID)
		switch {
		case err == nil:
			out = append(out, b)
		case Classify(err).Code != "binding-conflict":
			return nil, err
		}
	}
	return out, nil
}

type DesktopSecretReceipt struct {
	Alias string
	State string
}

// BindDesktopSecret applies only the project/profile/task/slot reviewed by the user.
func (a App) BindDesktopSecret(ctx context.Context, expected DesktopCredentialSlot, change SecretBindingChange) (DesktopSecretReceipt, error) {
	status := expected.Status
	change.SecretTarget = SecretTarget{Profile: status.Profile, Task: status.TaskID, Requirement: status.Requirement}
	result, err := a.changeSecretMapping(ctx, status.Project.ID, a.DataDir, change, false, &expected)
	if err != nil {
		return DesktopSecretReceipt{}, err
	}
	return DesktopSecretReceipt{Alias: result.Binding.Alias, State: result.desktopWrite}, nil
}

func (a App) desktopProviderKey(ctx context.Context, expected DesktopCredentialSlot) (DesktopCredentialSlot, provider.SessionKey, error) {
	s := expected.Status
	current, err := a.DesktopSecretSlot(ctx, s.Project.ID, SecretTarget{Profile: s.Profile, Task: s.TaskID, Requirement: s.Requirement})
	if err != nil {
		return current, provider.SessionKey{}, err
	}
	b := current.Status.Binding
	if b == nil || s.Binding == nil || b.ID != s.Binding.ID || b.Revision != s.Binding.Revision ||
		current.workspace != expected.workspace || current.Status.DeviceID != s.DeviceID {
		return current, provider.SessionKey{}, Fail("selection-changed", "the selected binding changed; refresh and try again")
	}
	return current, provider.SessionKey{WorkspaceID: current.workspace, DeviceID: s.DeviceID, Profile: b.ProviderProfile}, nil
}

// CheckDesktopSecret verifies local authentication, optionally followed by an
// explicit entry check. Provider sessions and values never leave the core.
func (a App) CheckDesktopSecret(ctx context.Context, expected DesktopCredentialSlot, checkEntry bool) (SecretStatus, error) {
	current, key, err := a.desktopProviderKey(ctx, expected)
	if err != nil {
		return current.Status, err
	}
	if checkEntry {
		err = a.checkSecretAvailability(ctx, key, &current.Status)
	} else {
		_, session, errAuth := a.checkSecretAuthentication(ctx, key, &current.Status)
		clear(session)
		err = errAuth
	}
	return current.Status, err
}

func (a App) LoginDesktopProvider(ctx context.Context, expected DesktopCredentialSlot) (ProviderLoginResult, error) {
	_, key, err := a.desktopProviderKey(ctx, expected)
	if err != nil {
		return ProviderLoginResult{}, err
	}
	return a.loginProvider(ctx, key)
}

func (a App) DesktopSecretManagementURL(ctx context.Context, expected DesktopCredentialSlot) (string, error) {
	current, key, err := a.desktopProviderKey(ctx, expected)
	if err != nil {
		return "", err
	}
	manager, ok := a.Providers.Adapters[key.Profile.Provider].(provider.EntryManager)
	if !ok {
		return "", Fail("provider-link-unavailable", "this provider does not supply a management link")
	}
	target, err := manager.ManagementURL(ctx, key.Profile, current.Status.Binding.Locator)
	parsed, parseErr := url.Parse(target)
	if err != nil || parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", Fail("provider-link-unavailable", "the provider management link could not be verified")
	}
	return target, nil
}
