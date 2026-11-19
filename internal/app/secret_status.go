package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type SecretList struct {
	Project  Project         `json:"project"`
	Bindings []SecretBinding `json:"bindings"`
}

type SecretStatus struct {
	Project            Project                       `json:"project"`
	TaskID             string                        `json:"taskId"`
	Task               string                        `json:"task"`
	Requirement        string                        `json:"requirement"`
	ResourceSlot       string                        `json:"resourceSlot"`
	Profile            string                        `json:"profile"`
	DeviceID           string                        `json:"deviceId"`
	Binding            *SecretBinding                `json:"binding"`
	Bound              bool                          `json:"bound"`
	Authenticated      *bool                         `json:"authenticated"`
	EntryRetrievable   *bool                         `json:"entryRetrievable"`
	ResourceReachable  *bool                         `json:"resourceReachable"`
	ReachabilitySeenAt string                        `json:"reachabilityObservedAt,omitempty"`
	Authentication     *provider.Authentication      `json:"authentication"`
	Capabilities       []provider.MethodCapabilities `json:"capabilities"`
}

func (a App) ListSecrets(ctx context.Context, selector, cwd string) (SecretList, error) {
	out := SecretList{Bindings: []SecretBinding{}}
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	out.Project = project
	// A Project can see its own references and those explicitly mapped by its Profiles.
	rows, err := db.QueryContext(ctx, "SELECT metadata"+liveSecretBindings+` AND
		(project_id=? OR id IN (SELECT json_extract(m.value,'$.bindingId') FROM configurations p,
		json_each(p.payload,'$.secretMappings') m WHERE p.project_id=?)) ORDER BY id`, project.ID, project.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		r, err := secretBindingRecord(raw)
		if err != nil {
			return out, err
		}
		out.Bindings = append(out.Bindings, secretBindingFromRecord(r))
	}
	return out, rows.Err()
}

func (a App) StatusSecret(ctx context.Context, selector, cwd string, target SecretTarget) (SecretStatus, error) {
	out := SecretStatus{Task: target.Task, Requirement: target.Requirement, Capabilities: []provider.MethodCapabilities{}}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	loc, err := configLocation(ctx, db, selector, cwd)
	if err != nil {
		return out, err
	}
	out.Project, out.DeviceID = loc.project, loc.device
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err := readSecretStatus(ctx, tx, loc, cwd, target, &out); err != nil {
		return out, secretStatusError(err, out)
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	key := provider.SessionKey{WorkspaceID: loc.workspace, DeviceID: loc.device, Profile: out.Binding.ProviderProfile}
	err = a.checkSecretAvailability(ctx, key, &out)
	return out, secretStatusError(err, out)
}

func secretStatusError(err error, status SecretStatus) error {
	if err == nil {
		return nil
	}
	var failure *Error
	if !errors.As(err, &failure) {
		return err
	}
	out := *failure
	out.Message += " for Task " + status.Task + " requirement " + status.Requirement
	if status.ResourceSlot != "" {
		out.Message += " (Resource slot " + status.ResourceSlot + ")"
	}
	out.Details = status
	return &out
}

func readSecretStatus(ctx context.Context, tx *sql.Tx, loc configurationLocation, cwd string, target SecretTarget, out *SecretStatus) error {
	task, err := loadTask(ctx, tx, loc.project.ID, target.Task)
	if err != nil {
		return err
	}
	out.Task, out.TaskID = task.Name, task.ID
	req, ok := task.requirement(target.Requirement)
	if !ok {
		return Fail("incompatible", "Task does not declare this credential requirement")
	}
	out.ResourceSlot = req.ResourceSlot
	if err := requireNoBindingConflict(ctx, tx, task.ID); err != nil {
		return err
	}
	p, cfg, err := secretProfile(ctx, tx, loc, target.Profile)
	if p != nil {
		out.Profile = p.Name
	}
	if err != nil {
		return err
	}
	i := slices.IndexFunc(cfg.SecretMappings, func(m SecretMapping) bool { return m.TaskID == task.ID && m.Requirement == req.Name })
	resource, err := secretResource(ctx, tx, cfg, task, req)
	if err != nil {
		return err
	}
	if err := readSecretReachability(ctx, tx, cwd, resource, out); err != nil {
		return err
	}
	if i < 0 {
		return Fail("binding-missing", "Profile has no SecretBinding mapping")
	}
	id := cfg.SecretMappings[i].BindingID
	if err := requireNoBindingConflict(ctx, tx, id); err != nil {
		return err
	}
	binding, err := loadSecretBinding(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := requireNoBindingConflict(ctx, tx, binding.Scope.ID); err != nil {
		return err
	}
	if !compatibleSecret(req, resource.ID, binding.SecretBindingSpec) {
		return Fail("incompatible", "SecretBinding does not match the requirement purpose, type and selected Resource")
	}
	out.Binding, out.Bound = &binding, true
	return nil
}

func readSecretReachability(ctx context.Context, tx *sql.Tx, cwd string, resource Resource, out *SecretStatus) error {
	detail, err := resourceDetail(ctx, tx, resource.Scope.ID, cwd, resource.ID)
	if err != nil || detail.Route.Status != "resolved" || detail.Route.Proxy != "" {
		return err
	}
	scope := ObservationScope{Kind: "service-endpoint", Target: detail.Route.Endpoint, Network: detail.Route.Network,
		OS: detail.Environment.OS, Arch: detail.Environment.Arch, ResourceRevision: resource.Revision}
	rows, err := tx.QueryContext(ctx, "SELECT data FROM resource_observations WHERE resource_id=? AND device_id=? ORDER BY seq DESC", resource.ID, out.DeviceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var observation Observation
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &observation); err != nil {
			return err
		}
		if observation.Scope == scope && observation.Tool == "tcp-connect" &&
			(observation.Status == "reachable" || observation.Status == "unreachable") {
			reachable := observation.Status == "reachable"
			out.ResourceReachable, out.ReachabilitySeenAt = &reachable, observation.ObservedAt
			break
		}
	}
	return rows.Err()
}

func (a App) checkSecretAvailability(ctx context.Context, key provider.SessionKey, out *SecretStatus) error {
	adapter, capabilities, err := a.providerAdapter(key.Profile)
	if capabilities != nil {
		out.Capabilities = capabilities
	}
	if err != nil {
		return Fail("secret-unavailable", Classify(err).Message)
	}
	session, err := a.loadProviderSession(ctx, key)
	defer clear(session)
	if errors.Is(err, provider.ErrNoSession) || err == nil && len(session) == 0 {
		auth := provider.Authentication{State: provider.SignedOut}
		out.Authentication, out.Authenticated = &auth, new(bool)
		return Fail("authentication-required", "log in to the provider on this device")
	}
	if err != nil {
		return err
	}
	auth, err := providerAuthentication(ctx, adapter, key.Profile, session)
	if err != nil {
		return err
	}
	authenticated := auth.State == provider.Authenticated
	out.Authentication, out.Authenticated = &auth, &authenticated
	if !authenticated {
		return Fail("authentication-required", "provider session requires login or unlock")
	}

	value, err := adapter.Resolve(ctx, key.Profile, session, out.Binding.Locator)
	clear(value)
	if errors.Is(err, provider.ErrAuthenticationRequired) {
		out.Authentication.State, *out.Authenticated = provider.SignedOut, false
		out.Authentication.ExpiresAt = nil
		return Fail("authentication-required", "provider session is no longer usable")
	}
	retrievable := err == nil
	out.EntryRetrievable = &retrievable
	if !retrievable {
		return Fail("secret-unavailable", "provider could not retrieve the referenced entry")
	}
	return nil
}
